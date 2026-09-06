package main

import (
	_ "time/tzdata"

	"context"
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

	"gocop/internal/config"
	"gocop/internal/db"
	"gocop/internal/importer/bp16"
	"gocop/internal/importer/csvlevels"
	"gocop/internal/importer/ugovor"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/service"
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
	nodeFlag := flag.String("node", "", "Identifikator ovog čvora za sinkronizaciju")
	nameFlag := flag.String("name", "", "Naziv ovog čvora za druge čvorove (zadano: ime računala)")
	syncPortFlag := flag.Int("sync-port", -1, "Port razmjene s drugim čvorovima (0 isključuje)")
	pairPortFlag := flag.Int("pair-port", -1, "Port uparivanja")
	discoveryPortFlag := flag.Int("discovery-port", -1, "UDP port pronalaženja na lokalnoj mreži (0 isključuje)")
	autoSyncFlag := flag.String("auto-sync", "", "Razmak automatske sinkronizacije, npr. 5m (0 isključuje)")
	importBP16 := flag.Bool("import-bp16", false, "Uvezi očitanja vodostaja iz Directus evidencije VGI Baranja i završi")
	importBP16Journals := flag.Bool("import-bp16-dnevnici", false, "Uvezi evidencije radova A.02 i A.03 iz Directusa kao rekonstruirane dnevnike (bez -upisi samo izvješće)")
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
	peersService.Accept(repository.KeepVersion)
	peersService.SetWantsAll(cfg.Sync.All)
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
	maintenanceRepo := repository.NewMaintenanceRepository(database, recorder)
	maintenanceService := service.NewMaintenanceService(maintenanceRepo, watercourseRepo, structureRepo)
	journalRepo := repository.NewJournalRepository(database, recorder)
	journalService := service.NewJournalService(journalRepo, stationRepo, readingRepo)

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
	if *importBP16 || *importBP16Journals {
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
	server, err := web.NewServer(*addr, authService, userService, sectionService, territoryService, stationService, watercourseService, structureService, readingService, moduleService, maintenanceService, journalService, orgService, supportContact(cfg),
		followRepo, applyReadingPolicy, peersService, recorder, sseBroker)
	if err != nil {
		log.Fatalf("Greška pri inicijalizaciji web poslužitelja: %v", err)
	}
	server.SetDatabase(database, *dbPath)

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
