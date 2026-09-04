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
	"gocop/internal/ledger"
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
	bp16Dir := flag.String("bp16-dir", "", "Uvoz iz ranije skinutih JSON datoteka umjesto iz Directusa")
	directusEnv := flag.String("directus-env", "", "Datoteka s DIRECTUS_URL i DIRECTUS_TOKEN (zadano ~/.config/gocop/directus.env)")
	csvFile := flag.String("tablica", "", "Tablica dnevnih vodostaja (CSV): stupci su postaje, redci datumi")
	csvHour := flag.Int("tablica-sat", 7, "Sat jutarnjeg očitanja u tablici")
	csvOrigin := flag.String("tablica-izvor", "", "Odakle tablica potječe, npr. \"COP Osijek — dnevna tablica\"")
	csvWrite := flag.Bool("upisi", false, "Bez ove zastavice uvoz samo izvještava, ništa ne upisuje")
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
	// Imenik djelatnika stoji uz bazu, izvan programa (osobni podaci)
	db.ImenikPath = filepath.Join(filepath.Dir(*dbPath), "imenik.json")
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
	peersService.OnApplied(func(ctx context.Context, versions []ledger.Version) error {
		return repository.ApplyVersions(ctx, database, recorder, versions)
	})

	userRepo := repository.NewUserRepository(database, recorder)
	sessionRepo := repository.NewSessionRepository(database)
	sectionRepo := repository.NewSectionRepository(database, recorder)

	authService := service.NewAuthService(userRepo, sessionRepo)
	sseBroker := service.NewSSEBroker()
	userService := service.NewUserService(userRepo, authService, sseBroker)
	sectionService := service.NewSectionService(sectionRepo, sseBroker)
	territoryRepo := repository.NewTerritoryRepository(database, recorder)
	territoryService := service.NewTerritoryService(territoryRepo, sectionService)
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

	// Uvoz tablice vodostaja. Bez -upisi je samo izvješće: koje su postaje
	// prepoznate, koliko bi zapisa bilo novo i gdje se izvori ne slažu.
	if *csvFile != "" {
		rep, err := csvlevels.Run(context.Background(), csvlevels.Options{
			Path: *csvFile, Hour: *csvHour, Origin: *csvOrigin, DryRun: !*csvWrite, Log: log.Printf,
			Deps: csvlevels.Deps{Readings: readingRepo, Stations: stationRepo, Structures: structureRepo},
		})
		if err != nil {
			log.Fatalf("Tablica vodostaja: %v", err)
		}
		log.Printf("Tablica vodostaja: %s", rep.Summary())
		for _, c := range rep.Matched {
			log.Printf("  stupac %-28q → %-28s %6d očitanja", c.Header, c.Name, c.Values)
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

	// Uvoz iz Directusa je zaseban način rada: uveze i završi
	if *importBP16 {
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
	server, err := web.NewServer(*addr, authService, userService, sectionService, territoryService, stationService, watercourseService, structureService, readingService, moduleService, supportContact(cfg),
		followRepo, applyReadingPolicy, peersService, recorder, sseBroker)
	if err != nil {
		log.Fatalf("Greška pri inicijalizaciji web poslužitelja: %v", err)
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
	tel := func(s string) string {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s)
		if strings.HasPrefix(digits, "0") {
			return "+385" + digits[1:]
		}
		if digits == "" {
			return ""
		}
		return "+" + digits
	}
	return web.SupportContact{
		Name: cfg.Support.Name, Phone: cfg.Support.Phone, PhoneLink: tel(cfg.Support.Phone),
		Email: cfg.Support.Email, Center: cfg.Support.Center,
		CenterPhone: cfg.Support.CenterPhone, CenterLink: tel(cfg.Support.CenterPhone),
	}
}
