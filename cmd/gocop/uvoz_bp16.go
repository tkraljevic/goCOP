// Uvoz iz BP16 (ranije evidencije VGI Baranja u Directusu) je privremen. Još
// treba, ali se ne pojednostavljuje: drži se na jednom mjestu i briše kad BP16
// prestane (Odluke (13. 10. 2026.)).
//
// Kad BP16 prestane, briše se:
//   - ova datoteka i paket internal/importer/bp16;
//   - u main.go polje zastaviceBP16 u zastavice, poziv dodajZastaviceBP16 u
//     procitajZastavice i uvjet s uveziBP16 u run;
//   - web.PDFPrijaveRekonstrukcija (internal/web/prijava_pdf.go), jer ga zove
//     samo uvoz prijava;
//   - premještaji i iznimke za BP16 u quality/config.json.
//
// Ostaje, jer zapisi preneseni iz BP16 ostaju u bazi i u razmjeni:
//   - internal/models/reading.go: podrijetlo ReadingOriginBP16
//     ("DIRECTUS_BP16") i natpis „evidencija VGI Baranja” u OriginLabel;
//   - internal/models/structure.go: isti natpis za objekte s podrijetlom
//     DIRECTUS_BP16, a šifre bp16-… ostaju šifre objekata u registru;
//   - internal/models/vodocuvar.go: NapomenaPrenesenogLista, koja stoji na
//     PDF-u svakog prenesenog dnevnog lista;
//   - internal/web/prijava_pdf.go i internal/web/vodocuvar_pdf.go: sivi blok
//     „PRENESENO IZ RANIJE EVIDENCIJE” na PDF-u prenesene prijave i lista;
//   - internal/db/structures_seed.go: prvo punjenje registra objekata iz
//     objekti_bp16.json. To je registar, ne uvoz, i provjera registara ga traži.
//
// Ostala mjesta koja spominju BP16 (prikaz, ovlasti i razmjena prenesenih
// listova i prijava) rade sa starim zapisima i također ostaju.

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gocop/internal/importer/bp16"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/weather"
	"gocop/internal/web"
)

// zastaviceBP16 su zastavice uvoza iz BP16; zastavice ih nosi ugrađene, pa se
// čitaju kao i ostale (z.importBP16)
type zastaviceBP16 struct {
	bp16Objekti        bool
	importBP16         bool
	importBP16Journals bool
	importBP16Obilasci bool
	importBP16Prijave  bool
	bp16Dir            string
	directusEnv        string
}

// dodajZastaviceBP16 upiše zastavice uvoza iz BP16 u fs; -upisi je zajednička
// sa svim uvozima i ostaje u procitajZastavice
func (z *zastaviceBP16) dodajZastaviceBP16(fs *flag.FlagSet) {
	fs.BoolVar(&z.bp16Objekti, "bp16-objekti", false, "Uvoz BP16: stvori crpne stanice i ustave koje registar nema i veži ih na letve istog imena")
	fs.BoolVar(&z.importBP16, "import-bp16", false, "Uvezi očitanja vodostaja iz Directus evidencije VGI Baranja i završi")
	fs.BoolVar(&z.importBP16Journals, "import-bp16-dnevnici", false, "Uvezi evidencije radova A.02 i A.03 iz Directusa kao rekonstruirane dnevnike (bez -upisi samo izvješće)")
	fs.BoolVar(&z.importBP16Obilasci, "import-bp16-obilasci", false, "Uvezi obilaske terena iz Directusa kao zadatke vodočuvara i rekonstruirane dnevne listove (bez -upisi samo izvješće)")
	fs.BoolVar(&z.importBP16Prijave, "import-bp16-prijave", false, "Uvezi obavijesti s terena (izvješća, prijave, obavijesti, zahtjevi vodočuvara) iz Directusa kao rekonstruirane prijave s terena (bez -upisi samo izvješće)")
	fs.StringVar(&z.bp16Dir, "bp16-dir", "", "Uvoz iz ranije skinutih JSON datoteka umjesto iz Directusa")
	fs.StringVar(&z.directusEnv, "directus-env", "", "Datoteka s DIRECTUS_URL i DIRECTUS_TOKEN (zadano ~/.config/gocop/directus.env)")
}

// trazenUvozBP16 kaže je li zadan neki od uvoza iz BP16
func (z zastaviceBP16) trazenUvozBP16() bool {
	return z.importBP16 || z.importBP16Journals || z.importBP16Prijave || z.importBP16Obilasci
}

// ovisnostiUvozaBP16 su baza, knjiga, čvor i repozitoriji koje uvoz iz BP16
// (Directus evidencija VGI Baranja) čita i piše
type ovisnostiUvozaBP16 struct {
	baza         *sql.DB
	knjiga       *ledger.Recorder
	cvor         *peers.Node
	dnevnici     *repository.JournalRepository
	odrzavanje   *repository.MaintenanceRepository
	organizacija *repository.OrgRepository
	ocitanja     *repository.ReadingRepository
	postaje      *repository.StationRepository
	objekti      *repository.StructureRepository
	korisnici    *repository.UserRepository
	djelatnici   *service.UserService
	vode         *repository.WatercourseRepository
	karta        web.KartaPostavke // za PDF rekonstruiranih prijava
}

// uveziBP16 je zaseban način rada: uveze iz Directusa (ili ranije skinutih
// datoteka) i završi. Tijelo je premješteno iz main bez izmjena, pa ovisnosti
// nose ista imena kao ondje.
func uveziBP16(z zastavice, o ovisnostiUvozaBP16) int {
	src := izvorBP16(z)
	switch {
	case z.importBP16Prijave:
		return uveziPrijaveBP16(z, o, src)
	case z.importBP16Obilasci:
		return uveziObilaskeBP16(z, o, src)
	case z.importBP16Journals:
		return uveziDnevnikeBP16(z, o, src)
	}
	return uveziLetveBP16(z, o, src)
}

// izvorBP16 je Directus (adresa i ključ iz directus.env) ili mapa s ranije
// skinutim datotekama (-bp16-mapa)
func izvorBP16(z zastavice) bp16.Source {
	var src bp16.Source
	if z.bp16Dir != "" {
		src = bp16.DirSource{Dir: z.bp16Dir}
	} else {
		envPath := z.directusEnv
		if envPath == "" {
			home, _ := os.UserHomeDir()
			envPath = filepath.Join(home, ".config", "gocop", "directus.env")
		}
		httpSrc, err := bp16.LoadEnv(envPath)
		if err != nil {
			log.Fatalf("Uvoz BP16: %v", err)
		}
		src = httpSrc
	}
	return src
}

// uveziPrijaveBP16 rekonstruira prijave s terena iz stare evidencije
func uveziPrijaveBP16(z zastavice, o ovisnostiUvozaBP16, src bp16.Source) int {
	database, recorder, node, orgRepo, userRepo, userService := o.baza, o.knjiga, o.cvor, o.organizacija, o.korisnici, o.djelatnici
	httpSrc, _ := src.(bp16.HTTPSource)
	korisnici := map[string]bp16.KorisnikUvoza{}
	if svi, err := userRepo.ListUsers("", 0, "", "", ""); err == nil {
		for _, u := range svi {
			k := bp16.KorisnikUvoza{ID: u.ID.String(), Ime: u.FullName, Sektor: "B"}
			korisnici[u.FullName] = k
		}
	}
	var datoteka func(ctx context.Context, id, upit string) ([]byte, error)
	if httpSrc.URL != "" {
		datoteka = httpSrc.Asset
	}
	rep, err := bp16.RunPrijave(context.Background(), src, bp16.PrijaveDeps{
		Prijave: repository.NewPrijavaRepository(database, recorder), Korisnici: korisnici,
		Podrucja: map[string]int{"KARAŠICA SEKTOR": 16, "DRAVSKI SEKTOR": 34, "DUNAVSKI SEKTOR - SJEVER": 34, "DUNAVSKI SEKTOR - JUG": 34},
		Sektor:   "B", Cvor: node.ID, Datoteka: datoteka, DryRun: !z.csvWrite, Log: log.Printf,
		// uvezene prijave: PDF iz podataka nosi slike, pa se izvorne ne čuvaju;
		// skenovi potpisanih ispisa ostaju u staroj evidenciji
		SlikeOdmah: true,
		IzradiPDF: func(p *models.PrijavaSTerena, slike map[string][]byte) []byte {
			var sek *models.Sector
			if sektori, err := userService.ListSectors(); err == nil {
				for i := range sektori {
					if sektori[i].ID == p.Sektor {
						sek = &sektori[i]
					}
				}
			}
			area, _ := orgRepo.GetArea(context.Background(), p.AreaID)
			return web.PDFPrijaveRekonstrukcija(context.Background(), p, slike, sek, area, o.karta)
		},
	})
	if err != nil {
		log.Fatalf("Uvoz prijava nije uspio: %v (do greške %s)", err, rep.Summary())
	}
	log.Printf("Uvoz prijava s terena: %s", rep.Summary())
	for k, n := range rep.PoKorisniku {
		log.Printf("  %s: %d", k, n)
	}
	for k, n := range rep.PoPodrucju {
		log.Printf("  %s: %d", k, n)
	}
	for k, n := range rep.Nepoznati {
		log.Printf("  nepoznato %q: %d", k, n)
	}
	if rep.DryRun {
		log.Printf("Ništa nije upisano. Dodajte -upisi za upis rekonstruiranih prijava.")
	}
	return 0
}

// uveziObilaskeBP16 uveze zadatke i rekonstruirane listove obilazaka
func uveziObilaskeBP16(z zastavice, o ovisnostiUvozaBP16, src bp16.Source) int {
	database, recorder, node, orgRepo, userRepo := o.baza, o.knjiga, o.cvor, o.organizacija, o.korisnici
	httpSrc, _ := src.(bp16.HTTPSource)
	korisnici := korisniciObilazaka(userRepo)
	var datoteka func(ctx context.Context, id, upit string) ([]byte, error)
	if httpSrc.URL != "" {
		datoteka = httpSrc.Asset
	}
	meteo := meteoObilazaka(orgRepo)
	rep, err := bp16.RunObilasci(context.Background(), src, bp16.ObilasciDeps{
		Vodocuvar: repository.NewVodocuvarRepository(database, recorder),
		Korisnici: korisnici, Sektor: "B", Cvor: node.ID, Datoteka: datoteka,
		SamoArhivirane: true, Meteo: meteo, DryRun: !z.csvWrite, Log: log.Printf,
	})
	if err != nil {
		log.Fatalf("Uvoz obilazaka nije uspio: %v (do greške %s)", err, rep.Summary())
	}
	log.Printf("Uvoz obilazaka: %s", rep.Summary())
	for k, n := range rep.Nepoznati {
		log.Printf("  nepoznato %q: %d", k, n)
	}
	if rep.DryRun {
		log.Printf("Ništa nije upisano. Dodajte -upisi za upis zadataka i rekonstruiranih listova.")
	}
	return 0
}

// korisniciObilazaka su vodočuvari po imenu iz stare evidencije, s područjem
// na kojem su obilazili
func korisniciObilazaka(userRepo *repository.UserRepository) map[string]bp16.KorisnikUvoza {
	korisnici := map[string]bp16.KorisnikUvoza{}
	if svi, err := userRepo.ListUsers("", 0, "", "", ""); err == nil {
		for _, u := range svi {
			k := bp16.KorisnikUvoza{ID: u.ID.String(), Ime: u.FullName, Sektor: "B"}
			// područje iz glavne dužnosti, pa iz bilo koje; i neaktivna
			// vrijedi, jer umirovljeni vodočuvar ima povijest na području
			// koje mu je zaduženje nosilo
			for _, d := range u.Duties {
				if d.IsPrimary && d.AreaID != nil && *d.AreaID > 0 {
					k.AreaID = *d.AreaID
					break
				}
			}
			for _, d := range u.Duties {
				if k.AreaID == 0 && d.AreaID != nil && *d.AreaID > 0 {
					k.AreaID = *d.AreaID
				}
			}
			if k.AreaID == 0 {
				k.AreaID = userRepo.PodrucjeDuznosti(context.Background(), u.ID.String())
			}
			korisnici[u.FullName] = k
		}
	}
	return korisnici
}

// meteoObilazaka je vrijeme za dane kojih stara evidencija nema: arhiva
// Open-Meteo, po koordinatama branjenog područja
func meteoObilazaka(orgRepo *repository.OrgRepository) func(ctx context.Context, dan time.Time) string {
	return func(ctx context.Context, dan time.Time) string {
		a, err := orgRepo.GetArea(ctx, 16)
		if err != nil || a == nil || !a.ImaKoordinate() {
			return ""
		}
		dnevno, err := (&weather.Client{}).Fetch(ctx, a.Latitude, a.Longitude, dan, 12)
		if err != nil || dnevno == nil {
			return ""
		}
		return strings.ReplaceAll(fmt.Sprintf("%.0f°C, vjetar %.0f–%.0f m/s, tlak %.0f hPa, oborine %.1f mm · Open-Meteo, naknadno",
			dnevno.Temperature, dnevno.WindFrom, dnevno.WindTo, dnevno.Pressure, dnevno.Precipitation), ".", ",")
	}
}

// uveziDnevnikeBP16 rekonstruira dnevnike iz stare evidencije
func uveziDnevnikeBP16(z zastavice, o ovisnostiUvozaBP16, src bp16.Source) int {
	journalRepo, maintenanceRepo, structureRepo, userService, watercourseRepo := o.dnevnici, o.odrzavanje, o.objekti, o.djelatnici, o.vode
	areas, err := userService.ListAreas("")
	if err != nil {
		log.Fatalf("Uvoz dnevnika: %v", err)
	}
	rep, err := bp16.RunJournals(context.Background(), src, bp16.JournalDeps{
		Journals: journalRepo, Maintenance: maintenanceRepo, Waters: watercourseRepo, Structures: structureRepo,
		Areas: areas, AreaID: 16, DryRun: !z.csvWrite, Log: log.Printf,
	})
	if err != nil {
		log.Fatalf("Uvoz dnevnika nije uspio: %v (do greške %s)", err, rep.Summary())
	}
	log.Printf("Uvoz dnevnika: %s", rep.Summary())
	for _, l := range rep.NewLocations {
		log.Printf("  nova lokacija: %s", l)
	}
	for k, n := range rep.PerYear {
		log.Printf("  %s: %d upisa", k, n)
	}
	if rep.NoUser > 0 {
		log.Printf("  upisa bez poznatog upisivača: %d", rep.NoUser)
	}
	if rep.DryRun {
		log.Printf("Ništa nije upisano. Dodajte -upisi za upis rekonstruiranih dnevnika.")
	}
	return 0
}

// uveziLetveBP16 uveze očitanja letvi (i po želji objekte) iz stare evidencije
func uveziLetveBP16(z zastavice, o ovisnostiUvozaBP16, src bp16.Source) int {
	readingRepo, stationRepo, structureRepo := o.ocitanja, o.postaje, o.objekti
	rep, err := bp16.Run(context.Background(), src, bp16.Deps{
		Readings: readingRepo, Stations: stationRepo, Structures: structureRepo, Log: log.Printf,
		DryRun: !z.csvWrite, StvoriObjekte: z.bp16Objekti,
	})
	if err != nil {
		log.Fatalf("Uvoz BP16 nije uspio: %v (do greške %s)", err, rep.Summary())
	}
	log.Printf("Uvoz BP16 gotov: %s", rep.Summary())
	return 0
}
