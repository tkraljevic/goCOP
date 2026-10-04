package main

import (
	"runtime/debug"
	_ "time/tzdata"

	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/config"
	"gocop/internal/db"
	"gocop/internal/hidroview"
	"gocop/internal/imecvora"
	"gocop/internal/importer/bp16"
	"gocop/internal/importer/csvlevels"
	"gocop/internal/importer/ugovor"
	"gocop/internal/javnivodostaji"
	"gocop/internal/ledger"
	"gocop/internal/mletva"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/posta"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
	"gocop/internal/service"
	"gocop/internal/weather"
	"gocop/internal/web"
)

// verzijaPrograma je izdanje goCOP-a. Alfa traje dok se ne zaokruže
// funkcionalnosti koje program treba imati; mijenja se pri izdavanju.
const verzijaPrograma = "0.0.33-alfa"

// version se može zadati pri prevođenju (-ldflags "-X main.version=…");
// prazno znači verzijaPrograma, s oznakom commita iz kojega je prevedeno.
var version = ""

// punaVerzija vraća verziju s kratkom oznakom commita, kad je Go zna
// (gradnja iz git stabla), i zvjezdicom kad stablo ima nespremljenih izmjena.
func punaVerzija() string {
	if version != "" {
		return version
	}
	v := verzijaPrograma
	if bi, ok := debug.ReadBuildInfo(); ok {
		var rev string
		izmjene := false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				izmjene = s.Value == "true"
			}
		}
		if len(rev) >= 7 {
			v += " (" + rev[:7]
			if izmjene {
				v += "*"
			}
			v += ")"
		}
	}
	return v
}

// redakIzdanja je ono što ispiše -version: "goCOP 0.0.33-alfa", bez oznake
// commita, jer ga Postava uspoređuje s oznakom izdanja
func redakIzdanja() string { return "goCOP " + verzijaPrograma }

// cekajZatvaranjeUlaza čita ulaz do kraja (Postava ništa ne šalje) i tada
// traži gašenje; ako gašenje već čeka, ne blokira
func cekajZatvaranjeUlaza(ulaz io.Reader, stop chan<- os.Signal) {
	_, _ = io.Copy(io.Discard, ulaz)
	log.Printf("Standardni ulaz zatvoren — gasim čvor na zahtjev Postave")
	select {
	case stop <- os.Interrupt:
	default:
	}
}

// zastavice su zastavice naredbenog retka; zadane kaže koje su doista upisane
// (za -podaci i -pakete zadano nije prazno, pa se samo tako zna)
type zastavice struct {
	configPath         string
	addr               string
	db                 string
	arhiva             string
	skenovi            string
	podaci             string
	paketi             string
	node               string
	name               string
	syncPort           int
	pairPort           int
	discoveryPort      int
	autoSync           string
	bp16Objekti        bool
	importBP16         bool
	importBP16Journals bool
	importBP16Obilasci bool
	importBP16Prijave  bool
	bp16Dir            string
	directusEnv        string
	csvFile            string
	csvHour            int
	csvOrigin          string
	csvSkip            string
	csvLinks           string
	csvQuality         string
	csvDerived         string
	csvMethod          string
	csvWrite           bool
	contractFile       string
	contractLinks      string
	contractAllItems   bool
	ponistiLozinku     string
	aktivirajRacun     bool
	ispisiIzdanje      bool
	podPostavom        bool
	pripremi           bool
	zadane             map[string]bool
}

// procitajZastavice čita zastavice naredbenog retka (bez imena programa)
func procitajZastavice(args []string) (zastavice, error) {
	var z zastavice
	fs := flag.NewFlagSet("gocop", flag.ContinueOnError)
	// Postavke: zastavica > gocop.toml > zadano. Zastavice bez vrijednosti
	// znače "nije zadano", pa se tek nakon čitanja datoteke zna što vrijedi.
	fs.StringVar(&z.configPath, "config", "", "Putanja do gocop.toml (zadano: uz bazu ili uz program)")
	fs.StringVar(&z.addr, "addr", "", "Adresa i port web sučelja (zadano :80; ako nije dostupan, sam prelazi na :8080)")
	fs.StringVar(&z.db, "db", "", "Putanja do SQLite baze (zadano data/gocop.db)")
	fs.StringVar(&z.arhiva, "arhiva", "", "Putanja do arhive vodostaja (zadano vodostaji.db uz bazu); može na drugi disk")
	fs.StringVar(&z.skenovi, "skenovi", "", "Mapa sa skenovima prijava (zadano skenovi uz bazu)")
	fs.StringVar(&z.podaci, "podaci", "vodostaji", "Stablo s izvornim datotekama arhive; prazno na čvoru koji arhivu samo prima")
	fs.StringVar(&z.paketi, "pakete", "pakete", "Mapa u koju se izdaju .cop paketi i u kojoj stoji katalog; prazno isključuje izdavanje")
	fs.StringVar(&z.node, "node", "", "Identifikator ovog čvora za sinkronizaciju")
	fs.StringVar(&z.name, "name", "", "Naziv ovog čvora za druge čvorove (zadano: ime računala)")
	fs.IntVar(&z.syncPort, "sync-port", -1, "Port razmjene s drugim čvorovima (0 isključuje)")
	fs.IntVar(&z.pairPort, "pair-port", -1, "Port uparivanja")
	fs.IntVar(&z.discoveryPort, "discovery-port", -1, "UDP port pronalaženja na lokalnoj mreži (0 isključuje)")
	fs.StringVar(&z.autoSync, "auto-sync", "", "Razmak automatske sinkronizacije, npr. 5m (0 isključuje)")
	fs.BoolVar(&z.bp16Objekti, "bp16-objekti", false, "Uvoz BP16: stvori crpne stanice i ustave koje registar nema i veži ih na letve istog imena")
	fs.BoolVar(&z.importBP16, "import-bp16", false, "Uvezi očitanja vodostaja iz Directus evidencije VGI Baranja i završi")
	fs.BoolVar(&z.importBP16Journals, "import-bp16-dnevnici", false, "Uvezi evidencije radova A.02 i A.03 iz Directusa kao rekonstruirane dnevnike (bez -upisi samo izvješće)")
	fs.BoolVar(&z.importBP16Obilasci, "import-bp16-obilasci", false, "Uvezi obilaske terena iz Directusa kao zadatke vodočuvara i rekonstruirane dnevne listove (bez -upisi samo izvješće)")
	fs.BoolVar(&z.importBP16Prijave, "import-bp16-prijave", false, "Uvezi obavijesti s terena (izvješća, prijave, obavijesti, zahtjevi vodočuvara) iz Directusa kao rekonstruirane prijave s terena (bez -upisi samo izvješće)")
	fs.StringVar(&z.bp16Dir, "bp16-dir", "", "Uvoz iz ranije skinutih JSON datoteka umjesto iz Directusa")
	fs.StringVar(&z.directusEnv, "directus-env", "", "Datoteka s DIRECTUS_URL i DIRECTUS_TOKEN (zadano ~/.config/gocop/directus.env)")
	fs.StringVar(&z.csvFile, "tablica", "", "Tablica dnevnih vodostaja (CSV): stupci su postaje, redci datumi")
	fs.IntVar(&z.csvHour, "tablica-sat", 7, "Sat jutarnjeg očitanja u tablici")
	fs.StringVar(&z.csvOrigin, "tablica-izvor", "", "Odakle tablica potječe, npr. \"COP Osijek — dnevna tablica\"")
	fs.StringVar(&z.csvSkip, "tablica-preskoci", "", "Stupci koje ne uvozimo, odvojeni zarezom (npr. protoci)")
	fs.StringVar(&z.csvLinks, "tablica-veze", "", "Ručno vezivanje stupaca na letve: \"stupac=sifra,stupac=sifra\"")
	fs.StringVar(&z.csvQuality, "tablica-kvaliteta", "", "Podrijetlo vrijednosti: prazno = izmjereno, REKONSTRUIRANO za preračun iz druge postaje")
	fs.StringVar(&z.csvDerived, "tablica-izvedeno-iz", "", "Postaja iz koje je preračunato, npr. \"postaja Bezdan\"")
	fs.StringVar(&z.csvMethod, "tablica-nacin", "", "Kako je preračunato: formula, korekcija, razdoblje valjanosti")
	fs.BoolVar(&z.csvWrite, "upisi", false, "Bez ove zastavice uvoz samo izvještava, ništa ne upisuje")
	fs.StringVar(&z.contractFile, "ugovor", "", "Ugovor o održavanju A.02 (xlsx iz dodatka Hrvatskih voda): uvozi popis lokacija i stavke radova")
	fs.StringVar(&z.contractLinks, "ugovor-veze", "", "Ručno vezivanje lokacija na registar: \"naziv iz popisa=sifra,naziv=sifra\"")
	fs.BoolVar(&z.contractAllItems, "ugovor-sve-stavke", false, "Uz stavke koje ugovor koristi upisati i cijeli ponudbeni troškovnik (opisi i jedinice, bez cijena)")
	fs.StringVar(&z.ponistiLozinku, "ponisti-lozinku", "", "Oporavak s konzole čvora: računu s tim korisničkim imenom postavi privremenu lozinku, ispiše je i završi (uz -config i -db kao pri pokretanju)")
	fs.BoolVar(&z.aktivirajRacun, "aktiviraj", false, "Uz -ponisti-lozinku: isključen račun i uključi")
	fs.BoolVar(&z.ispisiIzdanje, "version", false, "Ispiši izdanje (\"goCOP 0.0.x-alfa\") i završi")
	fs.BoolVar(&z.podPostavom, "upravitelj", false, "Čvor pod Postavom: uredno se gasi kad mu se zatvori standardni ulaz")
	fs.BoolVar(&z.pripremi, "pripremi", false, "Zapiši gocop.toml uz bazu s imenom čvora (-node) prije prvog pokretanja i završi; postojeće ime se ne mijenja")
	if err := fs.Parse(args); err != nil {
		return z, err
	}
	z.zadane = map[string]bool{}
	fs.Visit(func(f *flag.Flag) { z.zadane[f.Name] = true })
	return z, nil
}

// odrediPostavke slaže postavke čvora: zastavica > gocop.toml > zadano.
// Vraća i odakle su pročitane te ime čvora iz datoteke (prije -node). Kad
// -podaci i -pakete nisu upisane, uzima ih iz datoteke (u z).
func odrediPostavke(z *zastavice) (cfg config.Config, cfgFrom, imeIzDatoteke string, err error) {
	// baza se mora znati prije datoteke, jer datoteka živi uz bazu
	dbForConfig := z.db
	if dbForConfig == "" {
		dbForConfig = config.Default().DB
	}
	cfg, cfgFrom, err = config.Load(config.Candidates(z.configPath, dbForConfig))
	if err != nil {
		return cfg, "", "", err
	}
	if z.addr != "" {
		cfg.Addr = z.addr
	}
	if z.db != "" {
		cfg.DB = z.db
	}
	if cfg.ZamijeniZatvoreneIzvore() {
		log.Printf("Karta: Wikimedia više ne daje pločice drugim stranicama; koristi se OpenStreetMap (u %s promijenite [karta] plocice)", cfgFrom)
	}
	imeIzDatoteke = cfg.Node.ID
	if z.node != "" {
		cfg.Node.ID = z.node
	}
	if z.name != "" {
		cfg.Node.Name = z.name
	}
	if z.syncPort >= 0 {
		cfg.Sync.ExchangePort = z.syncPort
	}
	if z.pairPort >= 0 {
		cfg.Sync.PairPort = z.pairPort
	}
	if z.discoveryPort >= 0 {
		cfg.Sync.DiscoveryPort = z.discoveryPort
	}
	if z.autoSync != "" {
		cfg.Sync.AutoSync = z.autoSync
	}
	// Putanje: zastavica ima prednost, pa datoteka postavki, pa zadano. Za
	// -podaci i -pakete zadano nije prazno, pa se gleda je li zastavica
	// doista zadana.
	if z.arhiva != "" {
		cfg.Arhiva = z.arhiva
	}
	if z.skenovi != "" {
		cfg.Skenovi = z.skenovi
	}
	if !z.zadane["podaci"] && cfg.Podaci != "" {
		z.podaci = cfg.Podaci
	}
	if !z.zadane["pakete"] && cfg.Pakete != "" {
		z.paketi = cfg.Pakete
	}
	return cfg, cfgFrom, imeIzDatoteke, nil
}

// Ime čvora: upisano u gocop.toml, zadano zastavicom -node, ili ga svjež
// čvor izabere sam (ime računala i četiri nasumična znaka). Postojeća
// baza bez upisanog imena zadržava dosadašnje zadano ime gocop-cvor:
// pod njim su njezini zapisi. Ime se nikad ne mijenja samo.
func odrediImeCvora(cfg *config.Config) (noviIme bool) {
	if cfg.Node.ID == "" {
		if _, err := os.Stat(cfg.DB); err == nil {
			cfg.Node.ID = imecvora.Stari
		} else {
			racunalo, _ := os.Hostname()
			cfg.Node.ID = imecvora.Nasumicno(racunalo)
			noviIme = true
		}
	}
	if err := imecvora.Provjeri(cfg.Node.ID); err != nil {
		log.Printf("Ime čvora %q: %v", cfg.Node.ID, err)
	}
	return noviIme
}

// Uvoz tablice vodostaja. Bez -upisi je samo izvješće: koje su postaje
// prepoznate, koliko bi zapisa bilo novo i gdje se izvori ne slažu.
func uveziTablicu(z zastavice, deps csvlevels.Deps) {
	rep, err := csvlevels.Run(context.Background(), csvlevels.Options{
		Path: z.csvFile, Hour: z.csvHour, Origin: z.csvOrigin, DryRun: !z.csvWrite, Log: log.Printf,
		Skip: splitList(z.csvSkip), Aliases: splitPairs(z.csvLinks),
		Quality: strings.ToUpper(strings.TrimSpace(z.csvQuality)), Derived: z.csvDerived, Method: z.csvMethod,
		Deps: deps,
	})
	if err != nil {
		log.Fatalf("Tablica vodostaja: %v", err)
	}
	log.Printf("Tablica vodostaja: %s", rep.Summary())
	for _, c := range rep.Matched {
		log.Printf("  stupac %-28q → %-28s %6d očitanja", c.Header, c.Name, c.Values)
	}
	for _, sk := range rep.Skipped2 {
		log.Printf("  preskočeno na zahtjev: %q", sk)
	}
	for _, u := range rep.Unmatched {
		log.Printf("  NIJE PREPOZNATO: %q — nema takve letve u registru", u)
	}
	for _, a := range rep.Ambiguous {
		log.Printf("  DVOZNAČNO: %q — više letvi nosi taj naziv", a)
	}
	for _, d := range rep.Differs {
		log.Printf("  RAZLIKA: %s %s — u bazi %d cm (%s%s), u tablici %d cm",
			d.Gauge, d.Day.Format("02.01.2006."), d.Have, d.HaveAt.Format("15:04"),
			map[bool]string{true: "", false: ", " + d.From}[d.From == ""], d.New)
	}
	if rep.DryRun {
		log.Printf("Ništa nije upisano. Kad odlučite koji je izvor mjerodavan, dodajte -upisi.")
	}
}

// Uvoz ugovora o održavanju: popis lokacija s kategorijom i stavke radova.
// Bez -upisi samo izvješće: što je prepoznato, što bi bilo novo, gdje treba ruka.
func uveziUgovor(z zastavice, users *service.UserService, deps ugovor.Deps) {
	areas, err := users.ListAreas("")
	if err != nil {
		log.Fatalf("Ugovor: %v", err)
	}
	deps.Areas = areas
	rep, err := ugovor.Run(context.Background(), ugovor.Options{
		Path: z.contractFile, DryRun: !z.csvWrite, Aliases: splitPairs(z.contractLinks), AllItems: z.contractAllItems, Log: log.Printf,
		Deps: deps,
	})
	if err != nil {
		log.Fatalf("Ugovor: %v", err)
	}
	log.Printf("Ugovor: %s", rep.Summary())
	for _, m := range rep.Locations {
		what := "voda"
		if m.Structure {
			what = "nasip"
		}
		switch m.Status {
		case "postoji":
			log.Printf("  %-10s %-6s %-45q → %s (%s)", m.Status, m.Location.Seq, m.Location.Name, m.Display, m.Code)
		case "novo":
			log.Printf("  %-10s %-6s %-45q → %s se dodaje u registar", "NOVO", m.Location.Seq, m.Location.Name, what)
		default:
			log.Printf("  %-10s %-6s %-45q → %s", strings.ToUpper(m.Status), m.Location.Seq, m.Location.Name, strings.Join(m.Options, "; "))
		}
	}
	if rep.Suggested+rep.Ambiguous > 0 {
		log.Printf("Prijedloge i dvoznačne lokacije vežite zastavicom -ugovor-veze \"naziv=sifra\"; bez toga ostaju u popisu bez veze na registar.")
	}
	if rep.DryRun {
		log.Printf("Ništa nije upisano. Kad je popis u redu, dodajte -upisi.")
	}
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

// zapisiPrimjerPostavki: pri prvom pokretanju zapiše gocop.toml s
// komentarima; inače upiše ime čvora koje datoteka nije imala
func zapisiPrimjerPostavki(cfg config.Config, cfgFrom, imeIzDatoteke string, noviIme, imeIzZastavice bool) {
	// Pri prvom pokretanju zapiši datoteku s komentarima da korisnik ima što urediti
	// Primjer nosi ZADANE vrijednosti, ne one iz zastavica ovog pokretanja —
	// zastavica je za jedan put, datoteka je za uvijek; jedino identifikator
	// čvora ide iz pokretanja jer se nakon uparivanja ne smije mijenjati.
	examplePath := filepath.Join(filepath.Dir(cfg.DB), config.FileName)
	if cfgFrom == "" {
		example := config.Default()
		example.Node.ID = cfg.Node.ID
		if written, err := config.WriteExample(examplePath, example); err != nil {
			log.Printf("Postavke: primjer datoteke nije zapisan: %v", err)
		} else if written {
			log.Printf("Postavke: zapisan primjer %s — uredite ga i ponovno pokrenite", examplePath)
		}
	} else {
		log.Printf("Postavke: čitane iz %s", cfgFrom)
		// ime koje datoteka nije imala (izabrano sada ili zadano zastavicom)
		// mora preživjeti ponovno pokretanje bez zastavice
		if imeIzDatoteke == "" && (noviIme || imeIzZastavice) {
			if err := config.UpisiIme(cfgFrom, cfg.Node.ID); err != nil {
				log.Printf("Postavke: ime čvora nije upisano u %s: %v", cfgFrom, err)
			}
		}
	}
	if noviIme {
		log.Printf("Novi čvor dobio je ime %s (upisano u postavke; ne mijenja se)", cfg.Node.ID)
	}
}

// otvoriBazu otvori bazu (WAL) i shemu, spremište sadržaja uz nju (s
// preseljenjem starog sadržaja) i upiše početne podatke. Kad vrati grešku uz
// otvorenu bazu, pozivatelj je zatvara kao i inače.
func otvoriBazu(dbPath string) (*sql.DB, *sadrzaj.Spremiste, error) {
	// 1. Otvaranje SQLite baze u WAL modu
	database, err := db.OpenDB(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("Kritična greška pri otvaranju baze: %w", err)
	}

	// 2. Inicijalizacija sheme
	if err := db.InitSchema(database); err != nil {
		_ = database.Close() // vraća se greška otvaranja, ne zatvaranja
		return nil, nil, fmt.Errorf("Kritična greška pri inicijalizaciji sheme: %w", err)
	}

	// 2a. Spremište sadržaja: PDF-ovi i slike po otisku, u vlastitoj datoteci
	// uz glavnu bazu, da glavna raste s brojem zapisa a ne s megabajtima
	spremiste, err := sadrzaj.Otvori(filepath.Join(filepath.Dir(dbPath), "sadrzaj.db"))
	if err != nil {
		_ = database.Close() // vraća se greška otvaranja, ne zatvaranja
		return nil, nil, fmt.Errorf("Kritična greška pri otvaranju spremišta sadržaja: %w", err)
	}
	repository.SetSpremiste(spremiste)
	if n, bajtova, err := repository.PreseliSadrzaj(context.Background(), database); err != nil {
		return database, spremiste, fmt.Errorf("Seljenje sadržaja u spremište: %w", err)
	} else if n > 0 {
		log.Printf("Preseljeno u spremište sadržaja: %d datoteka, %.1f MB", n, float64(bajtova)/1e6)
	}
	if st, err := spremiste.Stanje(context.Background()); err == nil {
		log.Printf("Spremište sadržaja: %d sadržaja, %.1f MB, %d za dohvat", st.Sadrzaja, float64(st.Bajtova)/1e6, st.Zeljenih)
	}

	// 3. Popunjavanje početnih podataka (Sektori A-F, Branjena područja 1-34, Globalni admin Tomislav Kraljević)
	// Registri i imenik stoje uz bazu, izvan programa; čitaju se samo pri prvom punjenju
	db.DataDir = filepath.Dir(dbPath)
	db.ImenikPath = db.DataFile("imenik.json")
	if err := db.SeedInitialData(database); err != nil {
		return database, spremiste, fmt.Errorf("Greška pri unosu početnih podataka: %w", err)
	}
	return database, spremiste, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	kod := run(ctx, os.Args[1:], os.Stdin)
	stop()
	os.Exit(kod)
}

// run je cijeli život programa: zastavice, postavke, jednokratni načini rada
// (izdanje, priprema, oporavak lozinke, uvozi) ili čvor dok ctx traje (ili
// dok Postava ne zatvori ulaz). Vraća izlazni kod.
func run(ctx context.Context, args []string, ulaz io.Reader) int {
	z, err := procitajZastavice(args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}
	if z.ispisiIzdanje {
		// Ugovor s Postavom (docs/plan-instalacija.md §3.1a): Postava ovim
		// provjerava preuzetu datoteku prije zamjene, pa redak ostaje točno
		// ovakav i ništa se prije njega ne ispisuje.
		fmt.Println(redakIzdanja())
		return 0
	}
	log.Printf("goCOP %s", punaVerzija())
	web.SetVerzijaPrograma(punaVerzija())

	cfg, cfgFrom, imeIzDatoteke, err := odrediPostavke(&z)
	if err != nil {
		log.Fatalf("Postavke: %v", err)
	}
	zadane := z.zadane
	noviIme := odrediImeCvora(&cfg)

	// -pripremi: Postava prije prvog pokretanja upiše ime čvora iz
	// instalacijskog programa. Postojeće ime se ne mijenja.
	if z.pripremi {
		return pripremiPostavke(cfg, cfgFrom, imeIzDatoteke, os.Stdout)
	}

	// Oporavak lozinke s konzole: samo baza, bez poslužitelja i razmjene;
	// postavke se ne zapisuju, jer ovo nije pokretanje čvora
	if zadane["ponisti-lozinku"] {
		return ponistiLozinkuSKonzole(cfg.DB, cfg.Node.ID, z.ponistiLozinku, z.aktivirajRacun, os.Stdout)
	}
	if z.aktivirajRacun {
		log.Fatalf("-aktiviraj vrijedi samo uz -ponisti-lozinku")
	}

	addr := &cfg.Addr
	dbPath := &cfg.DB
	nodeID := &cfg.Node.ID
	nodeName := &cfg.Node.Name
	syncPort := &cfg.Sync.ExchangePort
	pairPort := &cfg.Sync.PairPort
	discoveryPort := &cfg.Sync.DiscoveryPort
	autoSyncValue := cfg.AutoSyncDuration()
	autoSync := &autoSyncValue

	zapisiPrimjerPostavki(cfg, cfgFrom, imeIzDatoteke, noviIme, z.node != "")

	log.Printf("=== goCOP — Centar obrane od poplava (Hrvatske vode) ===")
	log.Printf("Pokretanje čvora: %s", *nodeID)
	log.Printf("Baza podataka (čisti Go SQLite): %s", *dbPath)

	database, spremiste, err := otvoriBazu(*dbPath)
	if database != nil {
		defer database.Close()
	}
	if spremiste != nil {
		defer spremiste.Zatvori()
	}
	if err != nil {
		log.Fatal(err)
	}

	// 4. Inicijalizacija repozitorija i servisa
	// Knjiga verzija: svaki upis ostavlja verziju u ime ovog čvora
	recorder := ledger.New(database, *nodeID)
	// Što ovaj program zna: verzija nepoznatog entiteta ili novije sheme se
	// sprema i prenosi dalje, ali se javlja da treba ažurirati. Potvrdama
	// članstva i paketima prognoze polja se ne prenose iz starih verzija
	// (potpis pokriva samo ono što je potpisano, paket je neproziran).
	ledger.PoznatiEntiteti(append(repository.PoznatiEntiteti(),
		peers.EntityPeers, peers.EntityMemberships,
		prognoza.EntitetIzdanja, prognoza.EntitetModela, EntitetArhive)...)
	ledger.BezPrijenosaPolja(peers.EntityMemberships, prognoza.EntitetIzdanja, prognoza.EntitetModela)
	recorder.NaNovost(func(n ledger.Novost) {
		log.Printf("razmjena: %s — ažurirajte goCOP", n)
	})
	if err := recorder.UcitajNovosti(context.Background()); err != nil {
		log.Printf("Knjiga verzija: pregled novijih zapisa nije uspio: %v", err)
	}

	// Koliko povijesti očitanja ovaj čvor prima razmjenom
	followRepo := repository.NewFollowRepository(database)
	applyReadingPolicy := func() {
		followed, err := followRepo.Keys(context.Background())
		if err != nil {
			log.Printf("Vodostaji: praćene letve nisu pročitane: %v", err)
			followed = map[string]bool{}
		}
		repository.SetReadingHistoryPolicy(repository.ReadingHistoryPolicy{
			Months: cfg.Readings.HistoryMonths, Followed: followed,
		})
		if cfg.Readings.HistoryMonths > 0 {
			log.Printf("Vodostaji: razmjenom se preuzimaju očitanja zadnjih %d mjeseci, a za %d praćenih letvi cijela povijest",
				cfg.Readings.HistoryMonths, len(followed))
		}
	}
	applyReadingPolicy()

	// Jednokratni popravci podataka koji moraju ostaviti verziju u knjizi
	if err := repository.RunFixups(context.Background(), database, recorder); err != nil {
		log.Fatalf("Popravci podataka nisu uspjeli: %v", err)
	}

	// Identitet čvora (ključ uz bazu) i sinkronizacija s drugim čvorovima
	node, err := peers.LoadNode(*dbPath, *nodeID, *nodeName, punaVerzija())
	if err != nil {
		log.Fatalf("Kritična greška: %v", err)
	}
	peersService, err := peers.NewService(database, recorder, node, peers.Ports{
		Exchange: *syncPort, Pair: *pairPort, Discovery: *discoveryPort,
	})
	if err != nil {
		log.Fatalf("Kritična greška (mreža čvora): %v", err)
	}
	// izdanje putuje u razmjeni, da drugi čvorovi vide tko radi na starijem
	peersService.PostaviProgram(verzijaPrograma, punaVerzija())
	// Paketi se potpisuju ključem čvora. Nepotpisan paket je samo tvrdnja o
	// tome tko ga je izdao — svaki koji danas izađe nepotpisan ostaje takav,
	// jer se onaj koji je već otišao ne da naknadno potpisati.
	arhiva.PostaviKljucIzdavaca(node.PrivateKey())
	peersService.Accept(repository.KeepVersion)
	if err := peersService.OsvjeziSebe(context.Background()); err != nil {
		log.Printf("Zapis ovog čvora nije osvježen: %v", err)
	}
	// Uloge ovog čvora za mrežu: preuzima li vodostaje, izdaje li prognozu
	_, ulogePostavljene, ulogeErr := peersService.UcitajUloge(context.Background())
	if ulogeErr != nil {
		log.Printf("Uloge čvora nisu pročitane: %v", ulogeErr)
	}
	peersService.SetWantsAll(cfg.Sync.All)
	peersService.SetSpremiste(spremiste)
	var razmjenaArh *razmjenaArhive // arhiva razmjenom; postavlja se kad je poznato gdje arhiva stoji
	peersService.OnApplied(func(ctx context.Context, versions []ledger.Version) error {
		primateljPrognoze.primi(versions)
		if razmjenaArh != nil {
			razmjenaArh.primljeno(versions)
		}
		return repository.ApplyVersions(ctx, database, recorder, versions)
	})
	// Površina se pri pokretanju obnovi iz knjige, da zapela primjena s
	// prethodne razmjene ne ostavi čvor sa starim stanjem
	if n, err := repository.ReplaySurface(context.Background(), database, recorder); err != nil {
		log.Printf("Obnova površine iz knjige (%d zapisa): %v", n, err)
	}

	userRepo := repository.NewUserRepository(database, recorder)
	sessionRepo := repository.NewSessionRepository(database)
	sectionRepo := repository.NewSectionRepository(database, recorder)

	authService := service.NewAuthService(userRepo, sessionRepo)
	sseBroker := service.NewSSEBroker()
	userService := service.NewUserService(userRepo, authService, sseBroker)
	sectionService := service.NewSectionService(sectionRepo, sseBroker)
	territoryRepo := repository.NewTerritoryRepository(database, recorder)
	territoryService := service.NewTerritoryService(territoryRepo, sectionService)
	orgRepo := repository.NewOrgRepository(database, recorder)
	orgService := service.NewOrgService(orgRepo, sseBroker)
	if terms, err := orgRepo.GetTerms(context.Background()); err == nil {
		models.SetTerms(terms)
	}
	stationRepo := repository.NewStationRepository(database, recorder)
	stationService := service.NewStationService(stationRepo, sectionService, sseBroker)
	watercourseRepo := repository.NewWatercourseRepository(database, recorder)
	watercourseService := service.NewWatercourseService(watercourseRepo)
	structureRepo := repository.NewStructureRepository(database, recorder)
	structureService := service.NewStructureService(structureRepo)
	readingRepo := repository.NewReadingRepository(database, recorder)
	moduleRepo := repository.NewModuleRepository(database, recorder)
	moduleService := service.NewModuleService(moduleRepo)
	readingService := service.NewReadingService(readingRepo, stationRepo, structureRepo, sectionService, userService)
	episodeRepo := repository.NewEpisodeRepository(database, recorder)
	episodeService := service.NewEpisodeService(episodeRepo, readingRepo, stationRepo)
	maintenanceRepo := repository.NewMaintenanceRepository(database, recorder)
	maintenanceService := service.NewMaintenanceService(maintenanceRepo, watercourseRepo, structureRepo)
	journalRepo := repository.NewJournalRepository(database, recorder)
	journalService := service.NewJournalService(journalRepo, stationRepo, readingRepo)
	obracunRepo := repository.NewObracunRepository(database, recorder)
	if err := obracunRepo.Osiguraj(context.Background()); errors.Is(err, ledger.ErrNovijaShema) {
		log.Printf("Postavke obračuna sati nisu dopunjene: %v", err)
	} else if err != nil {
		log.Fatalf("postavke obračuna sati: %v", err)
	}
	obracunService := service.NewObracunService(obracunRepo)
	mtsRepo := repository.NewMtsRepository(database, recorder)
	if err := mtsRepo.OsigurajKatalog(context.Background()); errors.Is(err, ledger.ErrNovijaShema) {
		log.Printf("Katalog sredstava za obranu nije dopunjen: %v", err)
	} else if err != nil {
		log.Fatalf("katalog sredstava za obranu: %v", err)
	}
	mtsService := service.NewMtsService(mtsRepo, sectionRepo, userRepo)
	mtsService.SetStructures(structureRepo)
	izvjescaService := service.NewIzvjescaService(repository.NewIzvjescaRepository(database, recorder), sectionRepo, stationRepo, readingRepo, episodeRepo, journalRepo)

	if z.csvFile != "" {
		uveziTablicu(z, csvlevels.Deps{Readings: readingRepo, Stations: stationRepo, Structures: structureRepo})
		return 0
	}
	if z.contractFile != "" {
		uveziUgovor(z, userService, ugovor.Deps{Waters: watercourseRepo, Structures: structureRepo, Maintenance: maintenanceRepo})
		return 0
	}

	// Uvoz iz Directusa je zaseban način rada: uveze i završi
	if z.importBP16 || z.importBP16Journals || z.importBP16Prijave || z.importBP16Obilasci {
		return uveziBP16(z, ovisnostiUvozaBP16{
			baza: database, knjiga: recorder, cvor: node,
			dnevnici: journalRepo, odrzavanje: maintenanceRepo, organizacija: orgRepo,
			ocitanja: readingRepo, postaje: stationRepo, objekti: structureRepo,
			korisnici: userRepo, djelatnici: userService, vode: watercourseRepo,
			karta: web.KartaPostavke{Plocice: cfg.Karta.Plocice, Zasluge: cfg.Karta.Zasluge, NajviseZ: cfg.Karta.NajviseZ},
		})
	}

	// Drugi korak prijave izvana (PIN na službenu e-poštu, zapamćena
	// računala, rezervni i privremeni kodovi): sve samo na ovom čvoru, s
	// ključem izvedenim iz ključa čvora; postavke poslužitelja e-pošte
	// dobiva kad se sastavi servis akata
	drugiKorak := service.NewDrugiKorak(repository.NewDrugiKorakRepository(database),
		repository.NewRacuniSustavaRepository(database), userRepo, repository.NewAktiRepository(database, recorder))
	drugiKorak.SetKljuc(node.PrivateKey().Seed())
	authService.SetZastitaPrijave(drugiKorak)

	// Čišćenje starih sesija (i isteklog drugog koraka prijave) periodički
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			_ = sessionRepo.CleanExpiredSessions()
			if _, err := drugiKorak.Ocisti(context.Background()); err != nil {
				log.Printf("čišćenje drugog koraka prijave: %v", err)
			}
		}
	}()

	// 5. Inicijalizacija web poslužitelja s ugrađenim embed.FS resursima
	server, err := web.NewServer(*addr, authService, userService, sectionService, territoryService, stationService, watercourseService, structureService, readingService, episodeService, moduleService, maintenanceService, journalService, orgService, supportContact(cfg),
		followRepo, applyReadingPolicy, peersService, recorder, sseBroker)
	if err != nil {
		log.Fatalf("Greška pri inicijalizaciji web poslužitelja: %v", err)
	}
	server.SetDatabase(database, *dbPath)
	server.SetObracun(obracunService)
	izvjescaService.SetSektorska(repository.NewSektorskaIzvjescaRepository(database, recorder))
	server.SetIzvjesca(izvjescaService)
	server.SetMts(mtsService)
	server.SetKisomjeri(service.NewKisomjerService(repository.NewKisomjerRepository(database, recorder)))
	aktService := service.NewAktService(repository.NewAktiRepository(database, recorder), stationRepo, sectionRepo, territoryRepo, readingRepo, userService, episodeService, node.ID)
	models.SetTema(aktService.Tema(context.Background()))
	aktService.SetKljuc(node.PrivateKey())
	// lozinke sandučića spremljene prije otiska lozinke računa dobiju ga
	// (jednom, prije prve uporabe), a one kojima se lozinka računa otada
	// promijenila, i razmjenom, brišu se
	if err := aktService.PopuniOtiskeSanducica(context.Background()); err != nil {
		log.Printf("e-pošta: otisak lozinke računa uz spremljene lozinke sandučića nije dodan: %v (takve se lozinke ne koriste dok se ne upišu ponovno)", err)
	}
	aktService.SetPosta(posta.Postavke{Nacin: cfg.Posta.Nacin, Posluzitelj: cfg.Posta.Posluzitelj, Domena: cfg.Posta.Domena, Port: cfg.Posta.Port, Sigurnost: cfg.Posta.Sigurnost})
	server.SetAkti(aktService)
	drugiKorak.SetPosta(aktService.Posta)
	server.SetDrugiKorak(drugiKorak)
	vodocuvarService := service.NewVodocuvarService(repository.NewVodocuvarRepository(database, recorder), userService, node.ID)
	vodocuvarService.SetOrg(orgRepo)
	vodocuvarService.SetRadnoVrijeme(func(ctx context.Context) (string, string) {
		rv, err := obracunRepo.RadnoVrijeme(ctx)
		if err != nil {
			return "", ""
		}
		return rv.OdTekst(), rv.DoTekst()
	})
	server.SetVodocuvar(vodocuvarService, orgRepo)
	// akti samo u aktivnoj obrani (otvoren dnevnik COP-a), a ovjereni idu u dnevnike
	aktService.SetObrana(journalService.AktivnaObrana, service.NewObjavaAkta(journalService, vodocuvarService).Objavi)
	prijavaService := service.NewPrijavaService(repository.NewPrijavaRepository(database, recorder), userService, vodocuvarService, node.ID)
	prijavaService.SetOpcije(aktService.Opcije)
	server.SetPrijave(prijavaService)
	// primljeni sadržaj kojem je po pretplati istekao rok držanja otpušta se
	// s računala; zapisi ostaju i sadržaj se može opet dohvatiti
	go func() {
		for {
			time.Sleep(6 * time.Hour)
			if n, b, err := peersService.OtpustiStare(context.Background()); err != nil {
				log.Printf("otpuštanje starog sadržaja: %v", err)
			} else if n > 0 {
				log.Printf("otpušteno %d primljenih sadržaja (%.1f MB) po roku pretplate", n, float64(b)/1e6)
			}
		}
	}()
	// izvorne fotografije s terena brišu se nakon roka iz opcija; PDF ih nosi trajno
	go func() {
		for {
			dani := aktService.Opcije(context.Background()).CuvanjeSlika()
			if n, err := prijavaService.ObrisiStareSlike(context.Background(), dani); err != nil {
				log.Printf("brisanje starih fotografija: %v", err)
			} else if n > 0 {
				log.Printf("obrisano %d fotografija s terena starijih od %d dana (PDF ih nosi)", n, dani)
			}
			time.Sleep(24 * time.Hour)
		}
	}()
	potpisService := service.NewPotpisService(repository.NewPotpisRepository(database, recorder), userService, node.ID, node.PrivateKey(), authService.CheckPassword)
	if err := potpisService.Pokreni(context.Background()); err != nil {
		log.Printf("elektronički potpis nije dostupan: %v", err)
	} else {
		server.SetPotpis(potpisService)
	}
	server.SetZid(service.NewZidService(recorder, journalRepo, sectionRepo, mtsRepo, userRepo, stationRepo, episodeRepo))
	server.SetKarta(cfg.Karta.Plocice, cfg.Karta.Zasluge, cfg.Karta.NajviseZ)
	if len(cfg.Web.PouzdaniPosrednici) > 0 || cfg.Web.ZaglavljeKlijenta != "" { // prazan popis = zadane mreže
		posrednici, err := web.NoviPosrednici(cfg.Web.PouzdaniPosrednici, cfg.Web.ZaglavljeKlijenta)
		if err != nil {
			log.Fatalf("gocop.toml [web]: %v", err)
		}
		server.SetPosrednici(posrednici)
	}
	server.SetJavnaAdresa(cfg.JavnaAdresa)
	server.SetPodPostavom(z.podPostavom)
	skenovi := cfg.Skenovi
	if skenovi == "" {
		skenovi = filepath.Join(filepath.Dir(*dbPath), "skenovi")
	}
	server.SetSkenovi(filepath.Join(skenovi, "prijave"))

	// Hidrološka arhiva stoji uz bazu, kao zasebna datoteka. Smije je ne biti:
	// čvor koji je nije preuzeo radi bez povijesnih nizova, a ne pada.
	prognozePut := filepath.Join(filepath.Dir(*dbPath), "prognoze.db")
	if c, err := web.OtvoriPrognoze(prognozePut); err != nil {
		log.Printf("Baza prognoza nije pronađena (%s) — grafovi rade bez prognoze", prognozePut)
	} else {
		server.SetPrognoze(c)
		log.Printf("Baza prognoza: %s", prognozePut)
	}

	arhivaPut := cfg.Arhiva
	if arhivaPut == "" {
		arhivaPut = filepath.Join(filepath.Dir(*dbPath), "vodostaji.db")
	}
	server.SetPodaciDir(z.podaci)
	server.SetPaketiDir(z.paketi)
	if arhiva, err := repository.OpenArhiva(arhivaPut); err != nil {
		log.Printf("Arhiva vodostaja %s: %v", arhivaPut, err)
	} else if arhiva == nil {
		server.SetArhivaPut(arhivaPut)
		log.Printf("Arhiva vodostaja nije pronađena (%s) — letve rade bez povijesti; paket se može učitati", arhivaPut)
	} else {
		defer arhiva.Close()
		server.SetArhiva(arhiva)
		server.SetArhivaPut(arhivaPut)
		log.Printf("Arhiva vodostaja: %s", arhivaPut)
	}

	// Sinkronizacija: prima razmjene, odgovara na probe s lokalne mreže,
	// povremeno sam nazove poznate čvorove
	syncCtx, stopSync := context.WithCancel(context.Background())
	defer stopSync()
	razmjenaArh = novaRazmjenaArhive(database, recorder, spremiste, server, peersService, arhivaPut, z.paketi)
	go razmjenaArh.vrti(syncCtx)
	var pbRazmjena *sql.DB // baza prognoza, za pločicu razmjene; postavlja se niže
	server.SetRazmjena(func(ctx context.Context) web.RazmjenaStanje {
		return stanjeRazmjene(ctx, peersService, recorder, spremiste, razmjenaArh, pbRazmjena)
	})
	if *syncPort > 0 {
		go func() {
			if err := peersService.Serve(syncCtx); err != nil {
				log.Printf("Razmjena s čvorovima nije dostupna: %v", err)
			}
		}()
	}
	if *discoveryPort > 0 {
		go func() {
			if err := peersService.Announce(syncCtx); err != nil {
				log.Printf("Pronalaženje na lokalnoj mreži nije dostupno: %v", err)
			}
		}()
	}
	go peersService.RunAutoSync(syncCtx, *autoSync)

	// Javni vodostaji: svaki sat preuzmi očitanja letvi koje su povezane s
	// vodostaji.voda.hr i označene za preuzimanje. Bez interneta samo javi
	// grešku na letvi i pokuša za sat.
	javniUvoznik := javnivodostaji.NoviUvoznik(repository.NewJavniSpremiste(database, readingRepo), log.Printf)
	javniUvoznik.Preuzima = func() bool { return peersService.TrenutneUloge().Preuzima }
	nazivCvora := node.Name
	if nazivCvora == "" {
		nazivCvora = node.ID
	}

	// Prognoza se obnavlja čim stignu novi vodostaji (satni krug uz javne
	// vodostaje); bez baze prognoza poslužitelj radi bez prognoze.
	pbRazmjena = pokreniPrognozu(ovisnostiPrognoze{
		baza: database, knjiga: recorder, mreza: peersService, cvor: node, posluzitelj: server,
		uvoznik: javniUvoznik, razmjenaArh: razmjenaArh, ctx: syncCtx,
		dbPath: *dbPath, prognozePut: prognozePut, arhivaPut: arhivaPut,
		podaci: z.podaci, nazivCvora: nazivCvora,
		ulogePostavljene: ulogePostavljene, ulogeErr: ulogeErr,
	})
	// Letve na Geolux HydroViewu traže prijavu. Račun stoji na ovom čvoru,
	// šifriran ključem čvora, i traži se pri svakom preuzimanju — tako
	// promjena lozinke odmah vrijedi, bez ponovnog pokretanja. Letva može
	// imati svoj račun; kad nema, vrijedi račun čvora.
	hidroviewKljuc := hidroview.Kljuc(node.PrivateKey().Seed())
	javniUvoznik.PostaviHidroViewRacun(hidroViewRacun(database, repository.NewHidroViewRepository(database), hidroviewKljuc))
	// mletva.voda.hr, zatvorena mobilna stranica Hrvatskih voda, ima jedan
	// račun čvora za sve postaje: ondje su istjecanja hidroelektrana na Dravi.
	// Lozinka se zaključava istim ključem kao HydroView.
	javniUvoznik.PostaviMLetvaRacun(racunSustava(repository.NewRacuniSustavaRepository(database), hidroviewKljuc, mletva.Podrijetlo))
	server.SetJavniUvoz(javniUvoznik)
	server.SetHidroViewKljuc(hidroviewKljuc)
	u := peersService.TrenutneUloge()
	log.Printf("Uloge čvora: vodostaje s izvora %s, prognozu %s", map[bool]string{true: "preuzima", false: "ne preuzima (stižu razmjenom)"}[u.Preuzima],
		map[bool]string{true: "izdaje", false: "ne izdaje (stiže razmjenom)"}[u.Izdaje])
	go javniUvoznik.Pokreni(syncCtx)
	log.Printf("Čvor %s (ključ %.12s…) — razmjena :%d, uparivanje :%d, pronalaženje :%d",
		node.ID, node.PublicKey(), *syncPort, *pairPort, *discoveryPort)
	log.Print(opisMreze(peersService.NetworkInfo()))

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	server.JaviPostavljanje(*addr)
	if z.podPostavom {
		// Windows nema SIGTERM, pa Postava čvor gasi zatvaranjem cijevi na
		// standardnom ulazu. Zatvori se i kad Postava padne, pa čvor ne
		// ostane siroče.
		go cekajZatvaranjeUlaza(ulaz, stop)
	}

	go func() {
		log.Printf("Poslužitelj spreman na http://localhost%s", *addr)
		log.Printf("Prijava korisničkim imenom iz imenika; početna lozinka se mijenja pri prvoj prijavi")
		err := server.Start()
		if err != nil && err != http.ErrServerClosed && *addr == ":80" {
			// Port 80 na Linuxu i macOS-u traži administratorska prava; radije
			// raditi na 8080 nego ne raditi uopće — uz jasnu poruku.
			log.Printf("Port 80 nije dostupan (%v) — prelazim na :8080. Za port 80 pokrenite s administratorskim pravima ili dodijelite pravo binaryju.", err)
			*addr = ":8080"
			server.SetAddr(*addr)
			log.Printf("Poslužitelj spreman na http://localhost%s", *addr)
			err = server.Start()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("Greška web poslužitelja: %v", err)
		}
	}()

	select {
	case <-ctx.Done():
	case <-stop:
	}
	fmt.Println("\nZaustavljanje goCOP poslužitelja...")
	time.Sleep(500 * time.Millisecond)
	fmt.Println("goCOP poslužitelj ugašen.")
	return 0
}

// opisMreze je redak dnevnika pri pokretanju: u kojoj je mreži čvor i smije
// li primati članove
func opisMreze(net *peers.Network) string {
	switch {
	case net == nil:
		return "Čvor još nije ni u jednoj mreži — osnujte je u Postavkama ili neka vas primi nositelj ključa mreže ili ovlašteni primatelj"
	case net.DrziKljuc:
		return fmt.Sprintf("Mreža %q — ovaj čvor drži ključ mreže i može primati članove", net.Name)
	case net.CanAdmit:
		return fmt.Sprintf("Mreža %q — ovlašteni primatelj: ovaj čvor može primati članove", net.Name)
	}
	return fmt.Sprintf("Mreža %q — član", net.Name)
}

// supportContact prenosi kontakt iz postavki čvora na stranicu prijave
func supportContact(cfg config.Config) web.SupportContact {
	return web.SupportContact{
		Center: cfg.Support.Center, CenterPhone: cfg.Support.CenterPhone, CenterLink: web.TelLink(cfg.Support.CenterPhone),
	}
}

// splitList čita popis odvojen zarezom iz zastavice
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitPairs čita "stupac=sifra,stupac=sifra" iz zastavice
func splitPairs(s string) map[string]string {
	out := map[string]string{}
	for _, p := range splitList(s) {
		if k, v, ok := strings.Cut(p, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
