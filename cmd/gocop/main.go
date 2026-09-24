package main

import (
	_ "time/tzdata"

	"context"
	"database/sql"
	"flag"
	"fmt"
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

// version se postavlja pri prevođenju: -ldflags "-X main.version=1.2.3"
var version = ""

func main() {
	// Postavke: zastavica > gocop.toml > zadano. Zastavice bez vrijednosti
	// znače "nije zadano", pa se tek nakon čitanja datoteke zna što vrijedi.
	configPath := flag.String("config", "", "Putanja do gocop.toml (zadano: uz bazu ili uz program)")
	addrFlag := flag.String("addr", "", "Adresa i port web sučelja (zadano :80; ako nije dostupan, sam prelazi na :8080)")
	dbFlag := flag.String("db", "", "Putanja do SQLite baze (zadano data/gocop.db)")
	podaciFlag := flag.String("podaci", "vodostaji", "Stablo s izvornim datotekama arhive; prazno na čvoru koji arhivu samo prima")
	paketiFlag := flag.String("pakete", "pakete", "Mapa u koju se izdaju .cop paketi i u kojoj stoji katalog; prazno isključuje izdavanje")
	nodeFlag := flag.String("node", "", "Identifikator ovog čvora za sinkronizaciju")
	nameFlag := flag.String("name", "", "Naziv ovog čvora za druge čvorove (zadano: ime računala)")
	syncPortFlag := flag.Int("sync-port", -1, "Port razmjene s drugim čvorovima (0 isključuje)")
	pairPortFlag := flag.Int("pair-port", -1, "Port uparivanja")
	discoveryPortFlag := flag.Int("discovery-port", -1, "UDP port pronalaženja na lokalnoj mreži (0 isključuje)")
	autoSyncFlag := flag.String("auto-sync", "", "Razmak automatske sinkronizacije, npr. 5m (0 isključuje)")
	bp16Objekti := flag.Bool("bp16-objekti", false, "Uvoz BP16: stvori crpne stanice i ustave koje registar nema i veži ih na letve istog imena")
	importBP16 := flag.Bool("import-bp16", false, "Uvezi očitanja vodostaja iz Directus evidencije VGI Baranja i završi")
	importBP16Journals := flag.Bool("import-bp16-dnevnici", false, "Uvezi evidencije radova A.02 i A.03 iz Directusa kao rekonstruirane dnevnike (bez -upisi samo izvješće)")
	importBP16Obilasci := flag.Bool("import-bp16-obilasci", false, "Uvezi obilaske terena iz Directusa kao zadatke vodočuvara i rekonstruirane dnevne listove (bez -upisi samo izvješće)")
	importBP16Prijave := flag.Bool("import-bp16-prijave", false, "Uvezi obavijesti s terena (izvješća, prijave, obavijesti, zahtjevi vodočuvara) iz Directusa kao rekonstruirane prijave s terena (bez -upisi samo izvješće)")
	bp16Dir := flag.String("bp16-dir", "", "Uvoz iz ranije skinutih JSON datoteka umjesto iz Directusa")
	directusEnv := flag.String("directus-env", "", "Datoteka s DIRECTUS_URL i DIRECTUS_TOKEN (zadano ~/.config/gocop/directus.env)")
	csvFile := flag.String("tablica", "", "Tablica dnevnih vodostaja (CSV): stupci su postaje, redci datumi")
	csvHour := flag.Int("tablica-sat", 7, "Sat jutarnjeg očitanja u tablici")
	csvOrigin := flag.String("tablica-izvor", "", "Odakle tablica potječe, npr. \"COP Osijek — dnevna tablica\"")
	csvSkip := flag.String("tablica-preskoci", "", "Stupci koje ne uvozimo, odvojeni zarezom (npr. protoci)")
	csvLinks := flag.String("tablica-veze", "", "Ručno vezivanje stupaca na letve: \"stupac=sifra,stupac=sifra\"")
	csvQuality := flag.String("tablica-kvaliteta", "", "Podrijetlo vrijednosti: prazno = izmjereno, REKONSTRUIRANO za preračun iz druge postaje")
	csvDerived := flag.String("tablica-izvedeno-iz", "", "Postaja iz koje je preračunato, npr. \"postaja Bezdan\"")
	csvMethod := flag.String("tablica-nacin", "", "Kako je preračunato: formula, korekcija, razdoblje valjanosti")
	csvWrite := flag.Bool("upisi", false, "Bez ove zastavice uvoz samo izvještava, ništa ne upisuje")
	contractFile := flag.String("ugovor", "", "Ugovor o održavanju A.02 (xlsx iz dodatka Hrvatskih voda): uvozi popis lokacija i stavke radova")
	contractLinks := flag.String("ugovor-veze", "", "Ručno vezivanje lokacija na registar: \"naziv iz popisa=sifra,naziv=sifra\"")
	contractAllItems := flag.Bool("ugovor-sve-stavke", false, "Uz stavke koje ugovor koristi upisati i cijeli ponudbeni troškovnik (opisi i jedinice, bez cijena)")
	flag.Parse()

	// baza se mora znati prije datoteke, jer datoteka živi uz bazu
	dbForConfig := *dbFlag
	if dbForConfig == "" {
		dbForConfig = config.Default().DB
	}
	cfg, cfgFrom, err := config.Load(config.Candidates(*configPath, dbForConfig))
	if err != nil {
		log.Fatalf("Postavke: %v", err)
	}
	if *addrFlag != "" {
		cfg.Addr = *addrFlag
	}
	if *dbFlag != "" {
		cfg.DB = *dbFlag
	}
	if *nodeFlag != "" {
		cfg.Node.ID = *nodeFlag
	}
	if *nameFlag != "" {
		cfg.Node.Name = *nameFlag
	}
	if *syncPortFlag >= 0 {
		cfg.Sync.ExchangePort = *syncPortFlag
	}
	if *pairPortFlag >= 0 {
		cfg.Sync.PairPort = *pairPortFlag
	}
	if *discoveryPortFlag >= 0 {
		cfg.Sync.DiscoveryPort = *discoveryPortFlag
	}
	if *autoSyncFlag != "" {
		cfg.Sync.AutoSync = *autoSyncFlag
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
	}

	log.Printf("=== goCOP — Centar obrane od poplava (Hrvatske vode) ===")
	log.Printf("Pokretanje čvora: %s", *nodeID)
	log.Printf("Baza podataka (čisti Go SQLite): %s", *dbPath)

	// 1. Otvaranje SQLite baze u WAL modu
	database, err := db.OpenDB(*dbPath)
	if err != nil {
		log.Fatalf("Kritična greška pri otvaranju baze: %v", err)
	}
	defer database.Close()

	// 2. Inicijalizacija sheme
	if err := db.InitSchema(database); err != nil {
		log.Fatalf("Kritična greška pri inicijalizaciji sheme: %v", err)
	}

	// 2a. Spremište sadržaja: PDF-ovi i slike po otisku, u vlastitoj datoteci
	// uz glavnu bazu, da glavna raste s brojem zapisa a ne s megabajtima
	spremiste, err := sadrzaj.Otvori(filepath.Join(filepath.Dir(*dbPath), "sadrzaj.db"))
	if err != nil {
		log.Fatalf("Kritična greška pri otvaranju spremišta sadržaja: %v", err)
	}
	defer spremiste.Zatvori()
	repository.SetSpremiste(spremiste)
	if n, bajtova, err := repository.PreseliSadrzaj(context.Background(), database); err != nil {
		log.Fatalf("Seljenje sadržaja u spremište: %v", err)
	} else if n > 0 {
		log.Printf("Preseljeno u spremište sadržaja: %d datoteka, %.1f MB", n, float64(bajtova)/1e6)
	}
	if st, err := spremiste.Stanje(context.Background()); err == nil {
		log.Printf("Spremište sadržaja: %d sadržaja, %.1f MB, %d za dohvat", st.Sadrzaja, float64(st.Bajtova)/1e6, st.Zeljenih)
	}

	// 3. Popunjavanje početnih podataka (Sektori A-F, Branjena područja 1-34, Globalni admin Tomislav Kraljević)
	// Registri i imenik stoje uz bazu, izvan programa; čitaju se samo pri prvom punjenju
	db.DataDir = filepath.Dir(*dbPath)
	db.ImenikPath = db.DataFile("imenik.json")
	if err := db.SeedInitialData(database); err != nil {
		log.Fatalf("Greška pri unosu početnih podataka: %v", err)
	}

	// 4. Inicijalizacija repozitorija i servisa
	// Knjiga verzija: svaki upis ostavlja verziju u ime ovog čvora
	recorder := ledger.New(database, *nodeID)

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
	node, err := peers.LoadNode(*dbPath, *nodeID, *nodeName, version)
	if err != nil {
		log.Fatalf("Kritična greška: %v", err)
	}
	peersService, err := peers.NewService(database, recorder, node, peers.Ports{
		Exchange: *syncPort, Pair: *pairPort, Discovery: *discoveryPort,
	})
	if err != nil {
		log.Fatalf("Kritična greška (mreža čvora): %v", err)
	}
	// Paketi se potpisuju ključem čvora. Nepotpisan paket je samo tvrdnja o
	// tome tko ga je izdao — svaki koji danas izađe nepotpisan ostaje takav,
	// jer se onaj koji je već otišao ne da naknadno potpisati.
	arhiva.PostaviKljucIzdavaca(node.PrivateKey())
	peersService.Accept(repository.KeepVersion)
	peersService.SetWantsAll(cfg.Sync.All)
	peersService.SetSpremiste(spremiste)
	peersService.OnApplied(func(ctx context.Context, versions []ledger.Version) error {
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
	if err := obracunRepo.Osiguraj(context.Background()); err != nil {
		log.Fatalf("postavke obračuna sati: %v", err)
	}
	obracunService := service.NewObracunService(obracunRepo)
	mtsRepo := repository.NewMtsRepository(database, recorder)
	if err := mtsRepo.OsigurajKatalog(context.Background()); err != nil {
		log.Fatalf("katalog sredstava za obranu: %v", err)
	}
	mtsService := service.NewMtsService(mtsRepo, sectionRepo, userRepo)
	mtsService.SetStructures(structureRepo)
	izvjescaService := service.NewIzvjescaService(repository.NewIzvjescaRepository(database, recorder), sectionRepo, stationRepo, readingRepo, episodeRepo, journalRepo)

	// Uvoz tablice vodostaja. Bez -upisi je samo izvješće: koje su postaje
	// prepoznate, koliko bi zapisa bilo novo i gdje se izvori ne slažu.
	if *csvFile != "" {
		rep, err := csvlevels.Run(context.Background(), csvlevels.Options{
			Path: *csvFile, Hour: *csvHour, Origin: *csvOrigin, DryRun: !*csvWrite, Log: log.Printf,
			Skip: splitList(*csvSkip), Aliases: splitPairs(*csvLinks),
			Quality: strings.ToUpper(strings.TrimSpace(*csvQuality)), Derived: *csvDerived, Method: *csvMethod,
			Deps: csvlevels.Deps{Readings: readingRepo, Stations: stationRepo, Structures: structureRepo},
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
		return
	}

	// Uvoz ugovora o održavanju: popis lokacija s kategorijom i stavke radova.
	// Bez -upisi samo izvješće: što je prepoznato, što bi bilo novo, gdje treba ruka.
	if *contractFile != "" {
		areas, err := userService.ListAreas("")
		if err != nil {
			log.Fatalf("Ugovor: %v", err)
		}
		rep, err := ugovor.Run(context.Background(), ugovor.Options{
			Path: *contractFile, DryRun: !*csvWrite, Aliases: splitPairs(*contractLinks), AllItems: *contractAllItems, Log: log.Printf,
			Deps: ugovor.Deps{
				Waters: watercourseRepo, Structures: structureRepo,
				Maintenance: maintenanceRepo, Areas: areas,
			},
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
		return
	}

	// Uvoz iz Directusa je zaseban način rada: uveze i završi
	if *importBP16 || *importBP16Journals || *importBP16Prijave || *importBP16Obilasci {
		var src bp16.Source
		if *bp16Dir != "" {
			src = bp16.DirSource{Dir: *bp16Dir}
		} else {
			envPath := *directusEnv
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
		if *importBP16Prijave {
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
				Sektor:   "B", Cvor: node.ID, Datoteka: datoteka, DryRun: !*csvWrite, Log: log.Printf,
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
					return web.PDFPrijaveRekonstrukcija(context.Background(), p, slike, sek, area, web.KartaPostavke{Plocice: cfg.Karta.Plocice, Zasluge: cfg.Karta.Zasluge, NajviseZ: cfg.Karta.NajviseZ})
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
			return
		}
		if *importBP16Obilasci {
			httpSrc, _ := src.(bp16.HTTPSource)
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
			var datoteka func(ctx context.Context, id, upit string) ([]byte, error)
			if httpSrc.URL != "" {
				datoteka = httpSrc.Asset
			}
			// vrijeme za dane kojih stara evidencija nema: arhiva Open-Meteo,
			// po koordinatama branjenog područja
			meteo := func(ctx context.Context, dan time.Time) string {
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
			rep, err := bp16.RunObilasci(context.Background(), src, bp16.ObilasciDeps{
				Vodocuvar: repository.NewVodocuvarRepository(database, recorder),
				Korisnici: korisnici, Sektor: "B", Cvor: node.ID, Datoteka: datoteka,
				SamoArhivirane: true, Meteo: meteo, DryRun: !*csvWrite, Log: log.Printf,
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
			return
		}
		if *importBP16Journals {
			areas, err := userService.ListAreas("")
			if err != nil {
				log.Fatalf("Uvoz dnevnika: %v", err)
			}
			rep, err := bp16.RunJournals(context.Background(), src, bp16.JournalDeps{
				Journals: journalRepo, Maintenance: maintenanceRepo, Waters: watercourseRepo, Structures: structureRepo,
				Areas: areas, AreaID: 16, DryRun: !*csvWrite, Log: log.Printf,
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
			return
		}
		rep, err := bp16.Run(context.Background(), src, bp16.Deps{
			Readings: readingRepo, Stations: stationRepo, Structures: structureRepo, Log: log.Printf,
			DryRun: !*csvWrite, StvoriObjekte: *bp16Objekti,
		})
		if err != nil {
			log.Fatalf("Uvoz BP16 nije uspio: %v (do greške %s)", err, rep.Summary())
		}
		log.Printf("Uvoz BP16 gotov: %s", rep.Summary())
		return
	}

	// Čišćenje starih sesija periodički
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			_ = sessionRepo.CleanExpiredSessions()
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
	aktService := service.NewAktService(repository.NewAktiRepository(database, recorder), stationRepo, sectionRepo, territoryRepo, readingRepo, userService, episodeService, node.ID)
	aktService.SetKljuc(node.PrivateKey())
	aktService.SetPosta(posta.Postavke{Nacin: cfg.Posta.Nacin, Posluzitelj: cfg.Posta.Posluzitelj, Domena: cfg.Posta.Domena, Port: cfg.Posta.Port, Sigurnost: cfg.Posta.Sigurnost})
	server.SetAkti(aktService)
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
	server.SetJavnaAdresa(cfg.JavnaAdresa)
	server.SetSkenovi(filepath.Join(filepath.Dir(*dbPath), "skenovi", "prijave"))

	// Hidrološka arhiva stoji uz bazu, kao zasebna datoteka. Smije je ne biti:
	// čvor koji je nije preuzeo radi bez povijesnih nizova, a ne pada.
	prognozePut := filepath.Join(filepath.Dir(*dbPath), "prognoze.db")
	if c, err := web.OtvoriPrognoze(prognozePut); err != nil {
		log.Printf("Baza prognoza nije pronađena (%s) — grafovi rade bez prognoze", prognozePut)
	} else {
		server.SetPrognoze(c)
		log.Printf("Baza prognoza: %s", prognozePut)
	}

	arhivaPut := filepath.Join(filepath.Dir(*dbPath), "vodostaji.db")
	server.SetPodaciDir(*podaciFlag)
	server.SetPaketiDir(*paketiFlag)
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

	// Prognoza se obnavlja čim stignu novi vodostaji, a ne po vlastitom satu:
	// inače bi pola vremena stajala na starim brojkama a izgledala kao da je
	// današnja. Kad baze prognoza nema ili je prazna, poslužitelj radi kao i
	// dosad — samo bez prognoze.
	if pb, err := prognoza.Otvori(prognozePut); err != nil {
		log.Printf("Prognoza se neće obnavljati: %v", err)
	} else if ocitanjaRO, err := sql.Open("sqlite", *dbPath+"?mode=ro"); err != nil {
		log.Printf("Prognoza se neće obnavljati: očitanja: %v", err)
	} else if arhivaRO, err := sql.Open("sqlite", arhivaPut+"?mode=ro"); err != nil {
		log.Printf("Prognoza se neće obnavljati: arhiva: %v", err)
	} else {
		osvjezivac := &prognoza.Osvjezivac{Baza: pb, Ocitanja: ocitanjaRO,
			Arhiva: arhivaRO, Najdalje: 96, Model: prognoza.ModelLanac}
		javniUvoznik.NakonPreuzimanja = func(ctx context.Context) {
			// Mađarska prognoza izlazi jednom dnevno, ali ne uvijek u isti
			// sat; čita se svaki krug, a zapisuje samo novo. Treba je i za
			// usporedbu i kao ulaz tamo gdje nam lanac nema ništa uzvodno.
			hu, otkazi := context.WithTimeout(ctx, time.Minute)
			javniUvoznik.Korak("mađarska prognoza (hydroinfo.hu)", 91)
			if letve, err := prognoza.Dohvati(hu, nil); err != nil {
				log.Printf("%s: %v", prognoza.Podrijetlo, err)
				javniUvoznik.Redak("%s: %v", prognoza.Podrijetlo, err)
			} else if n, err := prognoza.SpremiTude(pb, prognoza.Podrijetlo, letve, prognoza.Sifra); err != nil {
				log.Printf("%s: zapis: %v", prognoza.Podrijetlo, err)
				javniUvoznik.Redak("%s: zapis: %v", prognoza.Podrijetlo, err)
			} else {
				if n > 0 {
					log.Printf("%s: zapisano %d novih vrijednosti", prognoza.Podrijetlo, n)
				}
				javniUvoznik.Redak("%s: %d letvi, %d novih vrijednosti", prognoza.Podrijetlo, len(letve), n)
			}
			// Srpska prognoza izlazi u 12 h; uz naše letve stoji drugom bojom.
			javniUvoznik.Korak("srpska prognoza (hidmet.gov.rs)", 94)
			if letve, err := prognoza.DohvatiHidmet(hu, nil); err != nil {
				log.Printf("%s: %v", prognoza.PodrijetloHidmet, err)
				javniUvoznik.Redak("%s: %v", prognoza.PodrijetloHidmet, err)
			} else if n, err := prognoza.SpremiTude(pb, prognoza.PodrijetloHidmet, letve, prognoza.SifraSrpske); err != nil {
				log.Printf("%s: zapis: %v", prognoza.PodrijetloHidmet, err)
				javniUvoznik.Redak("%s: zapis: %v", prognoza.PodrijetloHidmet, err)
			} else {
				if n > 0 {
					log.Printf("%s: zapisano %d novih vrijednosti", prognoza.PodrijetloHidmet, n)
				}
				javniUvoznik.Redak("%s: %d letvi, %d novih vrijednosti", prognoza.PodrijetloHidmet, len(letve), n)
			}
			otkazi()
			javniUvoznik.Korak("izračun prognoze", 96)
			ishod, err := osvjezivac.Osvjezi(ctx)
			if err != nil {
				log.Printf("prognoza: %v", err)
				javniUvoznik.Redak("prognoza: %v", err)
				return
			}
			if ishod.Preskoceno {
				javniUvoznik.Redak("prognoza: za ovaj sat već je izdana, ništa novo")
				return
			}
			javniUvoznik.Korak("zapis prognoze", 98)
			if err := osvjezivac.Zapisi(ishod); err != nil {
				log.Printf("prognoza: zapis: %v", err)
				javniUvoznik.Redak("prognoza: zapis: %v", err)
				return
			}
			log.Printf("prognoza: izdana za %s UTC (%.0f h unatrag), %d letvi, %d vrijednosti",
				time.Unix(ishod.Sada*3600, 0).UTC().Format("2006-01-02 15:04"),
				ishod.Zaostatak(time.Now()).Hours(), ishod.Letvi(), len(ishod.Izdane))
			javniUvoznik.Redak("prognoza izdana za %s, %d letvi, %d vrijednosti",
				time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04"), ishod.Letvi(), len(ishod.Izdane))
			for letva, zasto := range ishod.BezPrognoze {
				javniUvoznik.Redak("bez prognoze %s: %v", letva, zasto)
			}
		}
	}
	// Letve na Geolux HydroViewu traže prijavu. Račun stoji na ovom čvoru,
	// šifriran ključem čvora, i traži se pri svakom preuzimanju — tako
	// promjena lozinke odmah vrijedi, bez ponovnog pokretanja. Letva može
	// imati svoj račun; kad nema, vrijedi račun čvora.
	hidroviewRepo := repository.NewHidroViewRepository(database)
	hidroviewKljuc := hidroview.Kljuc(node.PrivateKey().Seed())
	javniUvoznik.PostaviHidroViewRacun(func(adresa string) (string, string, bool) {
		// Letva se traži i po adresi javne stranice i po šifri postaje na
		// telemetriji, jer se adresa u drugom slučaju sastavlja iz šifre.
		var letva string
		_ = database.QueryRow(`SELECT code FROM stations
			WHERE javni_url = ? OR (telemetrija_site <> '' AND instr(?, telemetrija_site) > 0)
			LIMIT 1`, adresa, adresa).Scan(&letva)
		r, err := hidroviewRepo.Racun(context.Background(), letva)
		if err != nil || r == nil {
			return "", "", false
		}
		lozinka, err := posta.Otkljucaj(hidroviewKljuc, r.Lozinka)
		if err != nil {
			log.Printf("HydroView: lozinka za %q se ne da otključati: %v", letva, err)
			return "", "", false
		}
		return r.Korisnik, lozinka, true
	})
	// mletva.voda.hr, zatvorena mobilna stranica Hrvatskih voda, ima jedan
	// račun čvora za sve postaje: ondje su istjecanja hidroelektrana na Dravi.
	// Lozinka se zaključava istim ključem kao HydroView.
	racuniSustava := repository.NewRacuniSustavaRepository(database)
	javniUvoznik.PostaviMLetvaRacun(func() (string, string, bool) {
		r, err := racuniSustava.Racun(context.Background(), mletva.Podrijetlo)
		if err != nil || r == nil {
			return "", "", false
		}
		lozinka, err := posta.Otkljucaj(hidroviewKljuc, r.Lozinka)
		if err != nil {
			log.Printf("%s: lozinka se ne da otključati: %v", mletva.Podrijetlo, err)
			return "", "", false
		}
		return r.Korisnik, lozinka, true
	})
	server.SetJavniUvoz(javniUvoznik)
	server.SetHidroViewKljuc(hidroviewKljuc)
	go javniUvoznik.Pokreni(syncCtx)
	log.Printf("Čvor %s (ključ %.12s…) — razmjena :%d, uparivanje :%d, pronalaženje :%d",
		node.ID, node.PublicKey(), *syncPort, *pairPort, *discoveryPort)
	if net := peersService.NetworkInfo(); net != nil {
		if net.CanAdmit {
			log.Printf("Mreža %q — ovaj čvor drži ključ mreže i može primati članove", net.Name)
		} else {
			log.Printf("Mreža %q — član", net.Name)
		}
	} else {
		log.Printf("Čvor još nije ni u jednoj mreži — osnujte je u Postavkama ili neka vas primi nositelj ključa mreže")
	}

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

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

	<-stop
	fmt.Println("\nZaustavljanje goCOP poslužitelja...")
	time.Sleep(500 * time.Millisecond)
	fmt.Println("goCOP poslužitelj ugašen.")
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
