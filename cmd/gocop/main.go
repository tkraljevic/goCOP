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
	"gocop/internal/kisomjeri"
	"gocop/internal/ledger"
	"gocop/internal/mletva"
	"gocop/internal/models"
	"gocop/internal/oborine"
	"gocop/internal/peers"
	"gocop/internal/poslovi"
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
		if imeIzDatoteke == "" && (noviIme || z.node != "") {
			if err := config.UpisiIme(cfgFrom, cfg.Node.ID); err != nil {
				log.Printf("Postavke: ime čvora nije upisano u %s: %v", cfgFrom, err)
			}
		}
	}
	if noviIme {
		log.Printf("Novi čvor dobio je ime %s (upisano u postavke; ne mijenja se)", cfg.Node.ID)
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
		if z.importBP16Prijave {
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
			return 0
		}
		if z.importBP16Obilasci {
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
		if z.importBP16Journals {
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
			Arhiva: arhivaRO, Najdalje: 96, Model: prognoza.ModelLanac, Cvor: nazivCvora}
		// Izdanje putuje razmjenom: ostali čvorovi ga upišu u svoju bazu
		// prognoza, a namješteni model ide uz njega kad se promijeni.
		var oborineDB *sql.DB // baza oborina; kiša ide uz izdanje
		objaviIzdanje := func(ctx context.Context, ishod *prognoza.Ishod) {
			if err := objaviPrognozu(ctx, database, recorder, pb, oborineDB, ishod, nazivCvora); err != nil {
				log.Printf("prognoza: slanje razmjenom: %v", err)
			}
		}
		// Čvor od prije uloga zadrži što je radio: ako je zadnjih tjedan dana
		// izdavao prognozu, i dalje preuzima i izdaje. Novi čvor ne radi ni
		// jedno dok mu se uloga ne uključi u Postavkama.
		if !ulogePostavljene && ulogeErr == nil {
			var zadnje sql.NullInt64
			_ = pb.QueryRow(`SELECT max(nastalo) FROM izdanja WHERE knjiga = ''`).Scan(&zadnje)
			radio := zadnje.Valid && time.Since(time.Unix(zadnje.Int64, 0)) < 7*24*time.Hour
			if err := peersService.PostaviUloge(context.Background(), peers.Uloge{Preuzima: radio, Izdaje: radio}); err != nil {
				log.Printf("Uloge čvora nisu zapisane: %v", err)
			}
		}
		primateljPrognoze.postavi(pb, recorder, func() bool { return peersService.TrenutneUloge().Izdaje })
		pbRazmjena = pb
		peersService.NaPromjenuUloga(func(u peers.Uloge) {
			if !u.Izdaje {
				primateljPrognoze.nadoknadi()
			}
		})
		go prorjedjujIzdanja(syncCtx, recorder)
		// Pričuvni Excel: veze naših letvi s mađarskim i srpskim iz arhive, uz
		// predupis zadnjih tuđih prognoza — za dan kad naša prognoza ne radi.
		server.SetPricuvno(func(ctx context.Context) prognoza.PricuvniPodaci {
			return prognoza.PricuvniIzracun(ctx, arhivaRO, ocitanjaRO, pb, time.Now(), models.Zagreb)
		})
		// Kiša po slivovima na naslovnoj: palo i očekivano prema uobičajenom
		// za međusliv (ERA5), s letvama na kojima će porasti voda.
		server.SetKisaSlivova(func(ctx context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error) {
			ob := osvjezivac.Oborine
			if ob == nil {
				return nil, nil, fmt.Errorf("oborina po međuslivovima se na ovom čvoru ne preuzima")
			}
			pragovi, err := prognoza.PragoviSlivova(arhivaRO, ob.Tocke)
			if err != nil {
				return nil, nil, err
			}
			var oborine map[string]prognoza.DnevniNiz
			if peersService.TrenutneUloge().Izdaje {
				oborine, err = prognoza.OborineOkoSada(ob, time.Now().Unix()/3600, 3, 2)
			} else {
				// Kišu preuzima čvor koji izdaje prognozu; stiže s izdanjem.
				oborine, _, err = prognoza.ZadnjaKisa(pb)
			}
			if err != nil {
				return nil, nil, err
			}
			_, dnevne, _ := prognoza.ZadnjeDnevno(pb)
			return prognoza.StanjeSlivova(oborine, pragovi), dnevne, nil
		})
		// Oborina za dnevni model: kišomjeri iz registra slivova, živi sati s
		// Open-Meteo u zasebnoj bazi uz bazu prognoza. Bez nje dnevni model
		// radi kao i prije, bez oborine.
		var oborineUvoznik *oborine.Uvoznik
		var kisUvoznik *kisomjeri.Uvoznik
		kisUlozeno := "" // dan zadnjeg ulaganja mjerenja u arhivu
		if ob, err := oborine.Otvori(filepath.Join(filepath.Dir(*dbPath), "oborine.db")); err != nil {
			log.Printf("Oborine se neće preuzimati: %v", err)
		} else {
			oborineUvoznik = &oborine.Uvoznik{DB: ob, Tocke: oborine.TockeIzRegistra(database)}
			oborineDB = ob
			primateljPrognoze.postaviOborine(ob)
			// Stvarni kišomjeri: mjerenja se skupljaju u istu radnu bazu, a
			// jednom dnevno ulažu u arhivsko stablo. pljusak.com je javan;
			// DHMZ se čita s letva.voda.hr, koja traži prijavu računom domene
			// Hrvatskih voda — istim računom čvora kao mletva.voda.hr
			// (ime kao za e-poštu, bez "@voda.hr"), zaključanim ključem čvora.
			kisSpremiste := &kisomjeri.Spremiste{DB: ob}
			racuniHV := repository.NewRacuniSustavaRepository(database)
			kljucHV := hidroview.Kljuc(node.PrivateKey().Seed())
			letvaRacun := func() (string, string, bool) {
				r, err := racuniHV.Racun(context.Background(), mletva.Podrijetlo)
				if err != nil || r == nil {
					return "", "", false
				}
				lozinka, err := posta.Otkljucaj(kljucHV, r.Lozinka)
				if err != nil {
					log.Printf("letva.voda.hr: lozinka se ne da otključati: %v", err)
					return "", "", false
				}
				return r.Korisnik, lozinka, true
			}
			if err := kisSpremiste.Pripremi(); err != nil {
				log.Printf("Stvarni kišomjeri se neće skupljati: %v", err)
			} else {
				kisUvoznik = &kisomjeri.Uvoznik{Spremiste: kisSpremiste, Postaje: kisomjeri.PostajeIzRegistra(database),
					Citaci: map[string]kisomjeri.Citac{"pljusak": &kisomjeri.Pljusak{}, "dhmz": &kisomjeri.Letva{Racun: letvaRacun}}}
				server.SetKisomjeriMjerenja(kisSpremiste)
			}
			if tocke, err := prognoza.OborinskeTocke(database); err != nil {
				log.Printf("Oborine: registar: %v", err)
			} else if len(tocke) > 0 {
				osvjezivac.Oborine = &prognoza.OborinskiIzvor{Tocke: tocke, Satne: oborineUvoznik.Satne}
			}
		}
		javniUvoznik.NakonPreuzimanja = func(ctx context.Context) {
			if !peersService.TrenutneUloge().Izdaje {
				// Tuđe prognoze, kišu i izračun radi čvor koji izdaje
				// prognozu; njegovo izdanje stiže razmjenom.
				javniUvoznik.Redak("prognozu izdaje drugi čvor; izdanje stiže razmjenom")
				return
			}
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
			// Austrijska prognoza (Donja Austrija) daje vrhu Dunava,
			// Wildungsmaueru, 48 sati unaprijed; izlazi više puta dnevno.
			javniUvoznik.Korak("austrijska prognoza (noel.gv.at)", 92)
			if letve, err := prognoza.DohvatiNOEL(hu, nil); err != nil {
				log.Printf("%s: %v", prognoza.PodrijetloNOEL, err)
				javniUvoznik.Redak("%s: %v", prognoza.PodrijetloNOEL, err)
			} else if n, err := prognoza.SpremiTude(pb, prognoza.PodrijetloNOEL, letve, prognoza.SifraNOEL); err != nil {
				log.Printf("%s: zapis: %v", prognoza.PodrijetloNOEL, err)
				javniUvoznik.Redak("%s: zapis: %v", prognoza.PodrijetloNOEL, err)
			} else {
				if n > 0 {
					log.Printf("%s: zapisano %d novih vrijednosti", prognoza.PodrijetloNOEL, n)
				}
				javniUvoznik.Redak("%s: %d letvi, %d novih vrijednosti", prognoza.PodrijetloNOEL, len(letve), n)
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
			if oborineUvoznik != nil {
				javniUvoznik.Korak("oborine (Open-Meteo)", 95)
				ob, otkazi := context.WithTimeout(ctx, 2*time.Minute)
				if n, err := oborineUvoznik.Preuzmi(ob); err != nil {
					log.Printf("oborine: %v", err)
					javniUvoznik.Redak("oborine: %v", err)
				} else {
					javniUvoznik.Redak("oborine: %d sati za kišomjere", n)
				}
				otkazi()
				// registar se mogao promijeniti (nova ili premještena točka)
				if tocke, err := prognoza.OborinskeTocke(database); err == nil && len(tocke) > 0 {
					osvjezivac.Oborine = &prognoza.OborinskiIzvor{Tocke: tocke, Satne: oborineUvoznik.Satne}
				}
			}
			if kisUvoznik != nil {
				javniUvoznik.Korak("stvarni kišomjeri", 95)
				kc, otkazi := context.WithTimeout(ctx, 2*time.Minute)
				n, err := kisUvoznik.Preuzmi(kc)
				otkazi()
				if err != nil {
					log.Printf("stvarni kišomjeri: %v", err)
					javniUvoznik.Redak("stvarni kišomjeri: %v", err)
				}
				javniUvoznik.Redak("stvarni kišomjeri: %d mjerenja", n)
				// Jednom dnevno, nakon ponoći, završeni dani idu u arhivu:
				// zadnja četiri dana, da krug koji je ispao ne ostavi rupu.
				sada := time.Now()
				if dan := sada.In(kisomjeri.Zagreb).Format("2006-01-02"); dan != kisUlozeno && z.podaci != "" && !imaStablo(z.podaci) {
					// Gradnja letvu slaže iz stabla, pa bi čvoru koji je arhivu
					// dobio paketima povijest kišomjera svela na zadnje dane.
					javniUvoznik.Redak("stvarni kišomjeri: na ovom čvoru nema stabla izvornih datoteka (%s), pa se ne ulažu u arhivu", z.podaci)
					kisUlozeno = dan
				}
				if dan := sada.In(kisomjeri.Zagreb).Format("2006-01-02"); dan != kisUlozeno && z.podaci != "" {
					if postaje, err := kisUvoznik.Postaje(); err == nil {
						letve, err := kisomjeri.Ulozi(z.podaci, kisUvoznik.Spremiste, postaje, sada.Add(-96*time.Hour), sada)
						if err != nil {
							log.Printf("stvarni kišomjeri, ulaganje: %v", err)
						}
						izdano := 0
						for _, l := range letve {
							if _, err := server.IzgradiLetvu(l, nil); err != nil {
								log.Printf("stvarni kišomjeri, gradnja %s: %v", l, err)
								continue
							}
							// Paket izlazi odmah, pa arhiva kiše raste i na
							// ostalim čvorovima bez ručnog izdavanja.
							if iz, err := server.IzdajArhivu(l, false, nil); err != nil {
								log.Printf("stvarni kišomjeri, izdavanje %s: %v", l, err)
							} else {
								izdano += iz.Promijenjenih
							}
						}
						if izdano > 0 && razmjenaArh != nil {
							razmjenaArh.potakniKrug() // kazalo novih paketa ide u razmjenu odmah
						}
						javniUvoznik.Redak("stvarni kišomjeri: uloženo u arhivu za %d postaja, izdano %d paketa", len(letve), izdano)
						kisUlozeno = dan
					}
				}
			}
			javniUvoznik.Korak("izračun prognoze", 96)
			racun := osvjezivac
			if prognoza.TraziIznova(ctx) {
				// Generiraj rukom: novi račun i za već izdani sat.
				iznova := *osvjezivac
				iznova.Iznova = true
				racun = &iznova
			}
			ishod, err := racun.Osvjezi(ctx)
			if err != nil {
				log.Printf("prognoza: %v", err)
				javniUvoznik.Redak("prognoza: %v", err)
				return
			}
			if ishod.Preskoceno {
				if ceka := ishod.KoCeka(); ceka != "" {
					// Prognoza stoji jer vrh lanca kasni: to se mora vidjeti, a ne
					// izgledati kao da je sve u redu.
					poruka := fmt.Sprintf("prognoza stoji na %s jer kasni vrh lanca: %s",
						time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04"), ceka)
					log.Printf("prognoza: %s", poruka)
					javniUvoznik.Redak("%s", poruka)
					return
				}
				javniUvoznik.Redak("prognoza: za ovaj sat već je izdana, ništa novo")
				return
			}
			javniUvoznik.Korak("zapis prognoze", 98)
			if err := osvjezivac.Zapisi(ishod); err != nil {
				log.Printf("prognoza: zapis: %v", err)
				javniUvoznik.Redak("prognoza: zapis: %v", err)
				return
			}
			objaviIzdanje(ctx, ishod)
			log.Printf("prognoza: izdana za %s UTC (%.0f h unatrag), %d letvi, %d vrijednosti",
				time.Unix(ishod.Sada*3600, 0).UTC().Format("2006-01-02 15:04"),
				ishod.Zaostatak(time.Now()).Hours(), ishod.Letvi(), len(ishod.Izdane))
			javniUvoznik.Redak("prognoza izdana za %s, %d letvi, %d vrijednosti",
				time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04"), ishod.Letvi(), len(ishod.Izdane))
			for letva, zasto := range ishod.BezPrognoze {
				javniUvoznik.Redak("bez prognoze %s: %v", letva, zasto)
			}
		}
		// Priprema modela iz aplikacije (Prognoze → Pripremi model): namjesti
		// lanac iz arhive, provjerom unatrag zapiše promašaje i odmah izda
		// prognozu novim modelom. Ako satni krug upravo traje, novo izdanje
		// pričeka idući sat — dva izdavanja istog sata ne idu jedno preko drugog.
		server.SetPripremaModela(func(ctx context.Context, p *poslovi.Posao) error {
			if !peersService.TrenutneUloge().Izdaje {
				return fmt.Errorf("model priprema čvor koji izdaje prognozu (Postavke → Čvor); ovaj ga prima razmjenom")
			}
			n, err := prognoza.NamjestiLanac(arhivaRO, pb, prognoza.OpcijeNamjestanja{Dnevnik: p, Korak: p.Korak})
			if err != nil {
				return err
			}
			p.Korak("provjera unatrag i zapis promašaja", 0, 0)
			o := prognoza.ZadaneOpcijeProvjere()
			o.Zapisi, o.Dnevnik = true, p
			m, err := prognoza.ProvjeriUnatrag(arhivaRO, pb, o)
			if err != nil {
				return err
			}
			izdano := "novi model vrijedi od idućeg satnog kruga, jer krug upravo traje"
			if !javniUvoznik.UTijeku() {
				p.Korak("ponovno izdavanje prognoze", 0, 0)
				iznova := *osvjezivac
				iznova.Iznova = true
				ishod, err := iznova.Osvjezi(ctx)
				if err != nil {
					return fmt.Errorf("model je spremljen, ali izdavanje nije uspjelo: %w", err)
				}
				if err := iznova.Zapisi(ishod); err != nil {
					return fmt.Errorf("model je spremljen, ali zapis prognoze nije uspio: %w", err)
				}
				objaviIzdanje(ctx, ishod)
				izdano = "prognoza je izdana novim modelom za " +
					time.Unix(ishod.Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04")
			}
			p.Zavrsi(fmt.Sprintf("Namješteno %d pojasa, zapisano %d promašaja; %s.", n, m, izdano), nil)
			return nil
		})
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
