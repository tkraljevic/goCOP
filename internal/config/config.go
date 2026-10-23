// Paket config čita postavke iz datoteke koju korisnik može urediti prije
// pokretanja — gocop.toml uz bazu. Zastavice na naredbenom retku imaju
// prednost pred datotekom, datoteka pred zadanim vrijednostima.
//
// Pri prvom pokretanju aplikacija sama zapiše datoteku sa zadanim
// vrijednostima i komentarima, pa korisnik ima što otvoriti i promijeniti,
// a ne prazan ekran i upute u dokumentaciji.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// FileName je naziv konfiguracijske datoteke; traži se uz bazu i uz binary
const FileName = "gocop.toml"

// Config su sve postavke koje se mogu zadati prije pokretanja
type Config struct {
	Addr string `toml:"addr" comment:"Adresa web sučelja. ':80' = port 80 na svim mrežnim karticama;\nkad je 80 zauzet, program sam prijeđe na 8080.\n  addr = ':8080'            # drugi port\n  addr = '127.0.0.1:8080'   # samo s ovog računala"`
	DB   string `toml:"db" comment:"Baza čvora. Uz nju stoje ključ čvora (node-key) i ova datoteka:\npremještajte ih i kopirajte uvijek zajedno.\n  db = '/data/gocop.db'           # Docker, NAS\n  db = 'C:\\goCOP\\data\\gocop.db'   # Windows"`
	// Velike i rijetko pisane datoteke mogu stajati odvojeno od baze: na
	// poslužitelju baza ide na brzi disk (SSD), a arhiva, izvorne datoteke,
	// skenovi i paketi na veliki (mehanički). Prazno znači uz bazu.
	Arhiva  string `toml:"arhiva" comment:"Datoteka arhive vodostaja i meteorologije (vodostaji.db, desetak GB).\nPrazno = uz bazu. Smije na drugi, veći disk, ali ne na mrežnu ni FUSE\nmapu (na NAS-u npr. /mnt/diskN, nikad /mnt/user).\n  arhiva = '/arhiva/vodostaji.db'   # disk montiran u Docker"`
	Podaci  string `toml:"podaci" comment:"Mapa s izvornim datotekama arhive. Prazno = 'vodostaji'; relativna\nputanja računa se od mape iz koje se goCOP pokreće (pod Postavom i kao\nusluga sustava to je mapa baze).\n  podaci = '/arhiva/vodostaji'"`
	Skenovi string `toml:"skenovi" comment:"Mapa za skenove i fotografije prijava s terena (u podmapi prijave).\nPrazno = mapa skenovi uz bazu.\n  skenovi = '/arhiva/skenovi'"`
	Pakete  string `toml:"pakete" comment:"Mapa u koju čvor izdaje .cop pakete arhive. Prazno = 'pakete'\n(od mape iz koje se goCOP pokreće; pod Postavom i kao usluga sustava to je\nmapa baze).\n  pakete = '/arhiva/pakete'"`
	// JavnaAdresa je adresa na kojoj je program dostupan izvana, npr.
	// https://gocop.voda.hr; ide u QR kod na dokumentima. Prazno: QR nosi
	// samo oznaku dokumenta.
	JavnaAdresa string `toml:"javna_adresa" comment:"Adresa na kojoj se program otvara izvana; ide u QR kod na\ndokumentima. Prazno = QR nosi samo oznaku dokumenta.\n  javna_adresa = 'https://cop.primjer.hr'"`

	Web struct {
		PouzdaniPosrednici []string `toml:"pouzdani_posrednici" comment:"Posrednici (tunel, nginx) kojima se vjeruje adresa klijenta iz\nzaglavlja; zahtjev koji dođe preko njih uvijek je vanjski.\nPrazno = ovo računalo i privatne mreže.\n  pouzdani_posrednici = ['172.17.0.1']   # cloudflared u Dockeru, preko mosta\n  pouzdani_posrednici = ['127.0.0.1']    # nginx na istom računalu"`
		ZaglavljeKlijenta  string   `toml:"zaglavlje_klijenta" comment:"Zaglavlje s adresom klijenta. Prazno = CF-Connecting-IP (Cloudflare tunel).\n  zaglavlje_klijenta = 'X-Forwarded-For'   # iza nginxa"`
		TailscaleLokalno   bool     `toml:"tailscale_lokalno" comment:"Na računalu s Tailscaleom zahtjev koji stigne na Tailscale adresu ovog računala\nvrijedi kao iz lokalne mreže (prijava bez PIN-a). Na računalu bez Tailscalea ne\nmijenja ništa. Uz false takav zahtjev vrijedi kao izvana i traži PIN.\n  tailscale_lokalno = false"`
	} `toml:"web"`

	Node struct {
		ID   string `toml:"id" comment:"Ime čvora u mreži: mala slova, brojke i crtica. Prazno = program ga\nizabere sam i upiše ovdje. Ne mijenjajte ga nakon prvog pokretanja:\npod njim čvor upisuje zapise, a drugi ga čvorovi pamte uz ključ.\n  id = 'cop-osijek-server'\n  id = 'pperic-thinkpad'"`
		Name string `toml:"name" comment:"Naziv koji drugi vide pri uparivanju. Prazno = ime računala.\n  name = 'COP Osijek (kućni server)'"`
	} `toml:"node"`

	Support struct {
		Center      string `toml:"centar" comment:"Prazno = redak o dežurnom operateru se ne prikazuje.\n  centar = 'COP Osijek'"`
		CenterPhone string `toml:"centar_telefon" comment:"Telefon na koji se javlja dežurni operater za vrijeme obrane.\n  centar_telefon = '031 123 456'"`
	} `toml:"kontakt" comment:"Centar obrane od poplava ovog čvora, za stranicu prijave. Svaki čvor\nupisuje svoj; osobu za pomoć oko prijave program uzima iz registra."`

	Readings struct {
		HistoryMonths int `toml:"povijest_mjeseci" comment:"Koliko mjeseci tuđih očitanja čvor drži iz razmjene. 0 = sve.\nVlastita i uvezena očitanja čuvaju se uvijek.\n  povijest_mjeseci = 12   # terenski laptop ili mobitel"`
		// MinutaPreuzimanja: krug preuzimanja ide po satu, a ne od
		// pokretanja, pa prognoza izlazi uvijek u isto vrijeme
		MinutaPreuzimanja int `toml:"minuta_preuzimanja" comment:"Minuta svakog sata (0–59) u kojoj čvor preuzima vodostaje s izvora;\nčvor koji izdaje prognozu izdaje je odmah potom. Hrvatske vode većinu\nvrijednosti punog sata objave u prvih deset minuta, a letve DHMZ-a tek\noko 40 minuta iza sata, pa je zadano 45: prognoza ide kad objavi i DHMZ.\n  minuta_preuzimanja = 2   # prognoza odmah, letve DHMZ-a stignu sat kasnije"`
	} `toml:"vodostaji"`

	// Karta je izvor pločica za prikaz položaja letve. Zadano je Wikimedijin
	// besplatni poslužitelj; kad se pločice jednom preuzmu za područje
	// obrane, ovdje se upiše lokalna putanja i karta radi bez interneta.
	Karta struct {
		Plocice  string `toml:"plocice" comment:"Izvor pločica karte, s {z}/{x}/{y}. Prazno isključuje kartu.\n  plocice = '/karta/{z}/{x}/{y}.png'   # preuzete pločice, rad bez interneta\n  plocice = ''                         # bez karte"`
		Zasluge  string `toml:"zasluge" comment:"Natpis o podrijetlu karte; obvezan po uvjetima izvora pločica."`
		NajviseZ int    `toml:"najvise_z" comment:"Najveće približavanje (0–19). Iznad 17 rijetko treba, a pločica\nje tada mnogo više."`
	} `toml:"karta"`

	Posta struct {
		Nacin       string `toml:"nacin" comment:"Kako goCOP šalje ovjerene akte primateljima „na znanje”:\n'ews' = kao Outlook, prijava sustava Windows; radi i izvan mreže\ntvrtke i sprema poruku u Poslano. 'smtp' = kad tvrtka otvori port.\n  nacin = 'smtp'"`
		Posluzitelj string `toml:"posluzitelj" comment:"Poslužitelj e-pošte tvrtke. Prazno isključuje slanje. Šalje se s\nadrese prijavljenog korisnika; lozinku svatko upisuje u svom profilu.\n  posluzitelj = 'owa.voda.hr'       # uz nacin = 'ews'\n  posluzitelj = 'smtp.primjer.hr'   # uz nacin = 'smtp'"`
		Domena      string `toml:"domena" comment:"Domena sustava Windows. Obično prazno: Exchange je javi sam.\n  domena = 'VODA'"`
		Port        int    `toml:"port" comment:"Samo za smtp: 587 (STARTTLS) ili 465 (TLS)."`
		Sigurnost   string `toml:"sigurnost" comment:"Samo za smtp: 'starttls' (uz 587) ili 'tls' (uz 465)."`
	} `toml:"posta"`

	Sync struct {
		ExchangePort  int      `toml:"exchange_port" comment:"Port razmjene s drugim čvorovima (TCP). 0 isključuje razmjenu."`
		PairPort      int      `toml:"pair_port" comment:"Port uparivanja; otvoren samo dok uparivanje traje."`
		DiscoveryPort int      `toml:"discovery_port" comment:"Port pronalaženja čvorova na lokalnoj mreži (UDP). 0 isključuje."`
		AutoSync      string   `toml:"auto_sync" comment:"Koliko se često čvor sam sinkronizira s poznatim čvorovima. '0' isključuje.\n  auto_sync = '1h'"`
		Bootstrap     []string `toml:"bootstrap" comment:"Stalno dostupni čvorovi preko kojih se pronalaze ostali. Svaki\ndodatni pomaže kad jedan padne.\n  bootstrap = ['cop.primjer.hr', 'cop.voda.hr']"`
		All           bool     `toml:"sve" comment:"true = čvor prati sve: očitanja i dnevnike svih područja i godina\n(uredski poslužitelj). false = samo ono što je označeno u programu\n(laptop, mobitel).\n  sve = true"`
		Tailscale     bool     `toml:"tailscale" comment:"true = na računalu s Tailscaleom razmjena koristi i Tailscale adrese\ndrugih vaših čvorova; false = razmjena radi kao da Tailscalea nema\n(za probu razmjene preko interneta). Na prijavu ne utječe."`
		// Protokol je ime protokola mreže (certifikat, pronalaženje,
		// uparivanje). Zadano je „gocop”. Drugo ime ima samo zasebna
		// instanca s novom mapom podataka (TESTNA SIMULACIJA, „gocop-sim”):
		// računala s različitim imenom protokola ne vide se i ne
		// razmjenjuju. Baza pamti ime protokola pod kojim je ušla u mrežu,
		// pa kopirana mapa s drugim imenom ne kreće
		// (peers.ProvjeriProtokolMreze). Ne skraćuje se i ne pretvara u mala
		// slova: neispravno ime odbija pokretanje.
		Protokol string `toml:"protokol" comment:"Ime protokola mreže. Računala s različitim imenom protokola ne vide se\ni ne razmjenjuju podatke. Mapa podataka pamti ime pod kojim je ušla u\nmrežu, pa s drugim imenom ne kreće.\n  protokol = 'gocop-sim'   # testna simulacija, u novoj mapi podataka"`
	} `toml:"sync"`

	// Povezivost je WEB sync (plan WEB synca, F1)
	Povezivost Povezivost `toml:"povezivost"`
}

// Povezivost je WEB sync (plan WEB synca, F1): izravna razmjena preko
// interneta, preko UDP-a i QUIC-a. Zadano je sve isključeno; uz
// web_sync_prijava = false nema ni UDP utičnice ni upita prema STUN
// poslužiteljima, osim kad administrator pritisne Provjeri vezu.
type Povezivost struct {
	WebSyncPrijava bool     `toml:"web_sync_prijava" comment:"true = računalo otvori UDP utičnicu za WEB sync i provjeri mrežu (STUN).\nfalse = nema UDP utičnice ni upita prema STUN poslužiteljima, osim kad\nadministrator pritisne Provjeri vezu.\n  web_sync_prijava = true"`
	WebSync        bool     `toml:"web_sync" comment:"true = razmjena ide i WEB syncom, kad je uključena i prijava.\nZasad se još ne koristi: razmjena ide kao prije."`
	UDPPort        int      `toml:"udp_port" comment:"UDP port WEB synca. Kad je zauzet, uzme se nasumičan. 0 = nasumičan."`
	Stun           []string `toml:"stun" comment:"STUN poslužitelji za provjeru mreže, različitih operatera. Vide javnu\nadresu i port ovog računala. Prazan popis isključuje provjeru.\n  stun = []"`
	LanAdresa      string   `toml:"lan_adresa" comment:"Adresa ovog računala u lokalnoj mreži, kad je goCOP u spremniku (Docker)\ni sam je ne vidi, samo IPv4. WEB sync je nudi drugim računalima umjesto adrese\nspremnika. Prazno = sam je nađe.\n  lan_adresa = '192.168.1.20'"`
}

// Default su zadane vrijednosti — iste kao bez ikakve datoteke
func Default() Config {
	var c Config
	c.Addr = ":80"
	c.DB = "data/gocop.db"
	// Node.ID namjerno prazan: ime čvora mora biti jedinstveno, pa ga čvor
	// bez upisanog imena izabere sam pri prvom pokretanju (OdrediIme)
	c.Web.TailscaleLokalno = true
	c.Sync.ExchangePort = 4710
	c.Sync.PairPort = 4711
	c.Sync.DiscoveryPort = 4712
	c.Sync.AutoSync = "5m"
	// Sync.Bootstrap ostaje prazan: program ga zasad ne čita, a domena
	// poslužitelja ne ide u javni kod, da je tražilice ne nađu
	c.Sync.Tailscale = true
	c.Sync.Protokol = "gocop"   // isto kao peers.Protocol; config ne uvozi peers
	c.Povezivost.UDPPort = 4713 // isto kao peers.DefaultUDPPort; config ne uvozi peers
	c.Povezivost.Stun = []string{"stun.l.google.com:19302", "stun.cloudflare.com:3478"}
	c.Readings.MinutaPreuzimanja = 45
	c.Karta.Plocice = ZadaniIzvorPlocica
	c.Karta.Zasluge = "© OpenStreetMap suradnici"
	c.Karta.NajviseZ = 17
	c.Posta.Nacin = "ews"
	c.Posta.Port = 587
	c.Posta.Sigurnost = "starttls"
	return c
}

// AutoSyncDuration čita razmak automatske sinkronizacije; neispravna
// vrijednost znači isključeno, ne pad programa
func (c Config) AutoSyncDuration() time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(c.Sync.AutoSync))
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// MinutaPreuzimanja je minuta svakog sata u kojoj počinje krug preuzimanja.
// Broj izvan 0–59 pri pokretanju ne ruši program: vrijedi zadana minuta, a
// drugi rezultat kaže što ne valja, za dnevnik.
func (c Config) MinutaPreuzimanja() (int, string) {
	m := c.Readings.MinutaPreuzimanja
	if poruka := provjeriMinutu(m); poruka != "" {
		return Default().Readings.MinutaPreuzimanja, poruka
	}
	return m, ""
}

// Candidates su mjesta gdje se datoteka traži, redom: izričito zadana,
// uz bazu, uz binary, u radnom direktoriju
func Candidates(explicit, dbPath string) []string {
	var out []string
	if explicit != "" {
		out = append(out, explicit)
	}
	if dbPath != "" {
		out = append(out, filepath.Join(filepath.Dir(dbPath), FileName))
	}
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), FileName))
	}
	out = append(out, FileName)
	return out
}

// Load čita prvu datoteku koja postoji. Vraća i putanju koja je pročitana;
// prazna putanja znači da datoteke nema i vrijede zadane vrijednosti.
func Load(candidates []string) (Config, string, error) {
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Default(), path, err
		}
		cfg, err := ProcitajTekst(data)
		if err != nil {
			return cfg, path, fmt.Errorf("%s: %w", path, err)
		}
		return cfg, path, nil
	}
	return Default(), "", nil
}

// zaglavlje je uvod datoteke postavki: kad se čita, što ima prednost, gdje
// piše što znači prazna vrijednost i najčešće izmjene.
const zaglavlje = "# goCOP — postavke čvora\n" +
	"#\n" +
	"# Program čita ovu datoteku samo pri pokretanju: nakon izmjene ponovno\n" +
	"# pokrenite goCOP. Zastavica na naredbenom retku (npr. -addr :8080) ima\n" +
	"# prednost pred datotekom.\n" +
	"#\n" +
	"# Uz svaku postavku piše što znači prazna vrijednost (''). Redak s #\n" +
	"# je komentar; primjer uključite tako da maknete # ispred njega.\n" +
	"#\n" +
	"# Najčešće izmjene:\n" +
	"#   uredski poslužitelj, stalno upaljen:  [sync] sve = true\n" +
	"#   terenski laptop ili mobitel:           [vodostaji] povijest_mjeseci = 12\n" +
	"#   port 80 je zauzet:                     addr = ':8080'\n" +
	"#   slanje akata e-poštom:                 [posta] posluzitelj = 'owa.voda.hr'\n\n"

// PrimjerFileName je primjer postavki koji program piše uz bazu pri svakom
// pokretanju, s opisima i primjerima tekuće inačice. Program ga ne čita.
// Postojeći gocop.toml nikad se ne prepisuje, pa bez ovoga čvor nadograđen
// sa starije inačice ne bi vidio nove opise.
const PrimjerFileName = "gocop.toml.primjer"

// zaglavljePrimjera stoji iznad uvoda u primjeru postavki.
const zaglavljePrimjera = "# Ovo je primjer: program ga ne čita i piše ga iznova sa svakom\n" +
	"# inačicom. Postavke čvora su u gocop.toml; odavde prepišite ono što trebate.\n" +
	"#\n"

// ZapisiPrimjer zapisuje gocop.toml.primjer u mapu dir, sa zadanim
// vrijednostima, opisima i primjerima; vraća je li ga zapisao. Isti sadržaj
// ne piše se iznova, pa se datoteka mijenja samo s novom inačicom.
func ZapisiPrimjer(dir string) (bool, error) {
	data, err := toml.Marshal(Default())
	if err != nil {
		return false, err
	}
	sadrzaj := []byte(zaglavljePrimjera + zaglavlje + string(data))
	put := filepath.Join(dir, PrimjerFileName)
	if postojeci, err := os.ReadFile(put); err == nil && bytes.Equal(postojeci, sadrzaj) {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(put, sadrzaj, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// WriteExample zapisuje datoteku sa zadanim vrijednostima i komentarima —
// samo ako ne postoji, da nikad ne pregazi korisnikove izmjene
func WriteExample(path string, cfg Config) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	data, err := Primjer(cfg)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// UpisiIme upisuje ime čvora u postojeću datoteku postavki: zamijeni redak
// id u odjeljku [node] ili ga doda. Ostatak datoteke, s komentarima, ostaje.
func UpisiIme(path, ime string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	redovi := strings.Split(string(b), "\n")
	odjeljak, umetni := "", -1
	for i, r := range redovi {
		t := strings.TrimSpace(r)
		if strings.HasPrefix(t, "[") {
			odjeljak = t
			if t == "[node]" {
				umetni = i + 1
			}
			continue
		}
		if odjeljak == "[node]" && (strings.HasPrefix(t, "id ") || strings.HasPrefix(t, "id=")) {
			redovi[i] = fmt.Sprintf("id = %q", ime)
			return os.WriteFile(path, []byte(strings.Join(redovi, "\n")), 0o644)
		}
	}
	if umetni >= 0 {
		redovi = append(redovi[:umetni], append([]string{fmt.Sprintf("id = %q", ime)}, redovi[umetni:]...)...)
		return os.WriteFile(path, []byte(strings.Join(redovi, "\n")), 0o644)
	}
	tekst := strings.TrimRight(string(b), "\n") + fmt.Sprintf("\n\n[node]\nid = %q\n", ime)
	return os.WriteFile(path, []byte(tekst), 0o644)
}

// ZadaniIzvorPlocica je OpenStreetMapov poslužitelj pločica
const ZadaniIzvorPlocica = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"

// stariIzvorPlocica je Wikimedijin poslužitelj, zadan do 0.0.29-alfa. Od
// listopada 2026. pločice daje samo Wikimedijinim stranicama (403), a
// postavke ga često imaju upisanog izričito (primjer pri prvom pokretanju).
const stariIzvorPlocica = "https://maps.wikimedia.org/osm-intl/{z}/{x}/{y}.png"

// ZamijeniZatvoreneIzvore mijenja Wikimedijine pločice, koje više ne rade,
// OpenStreetMapovima; vraća je li zamijenio. Drugi izvor ostaje kakav jest.
func (c *Config) ZamijeniZatvoreneIzvore() bool {
	if strings.TrimSpace(c.Karta.Plocice) != stariIzvorPlocica {
		return false
	}
	c.Karta.Plocice = ZadaniIzvorPlocica
	if strings.Contains(c.Karta.Zasluge, "Wikimedia") || c.Karta.Zasluge == "" {
		c.Karta.Zasluge = "© OpenStreetMap suradnici"
	}
	return true
}
