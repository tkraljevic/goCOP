// Paket config čita postavke iz datoteke koju korisnik može urediti prije
// pokretanja — gocop.toml uz bazu. Zastavice na naredbenom retku imaju
// prednost pred datotekom, datoteka pred zadanim vrijednostima.
//
// Pri prvom pokretanju aplikacija sama zapiše datoteku sa zadanim
// vrijednostima i komentarima, pa korisnik ima što otvoriti i promijeniti,
// a ne prazan ekran i upute u dokumentaciji.
package config

import (
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
	Addr string `toml:"addr" comment:"Adresa i port web sučelja. :80 da nitko ne mora upisivati port;\nako je 80 zauzet ili nedostupan, aplikacija sama prelazi na :8080.\nPromijenite ovdje ako je na ovom računalu 80 trajno zauzet."`
	DB   string `toml:"db" comment:"Putanja do SQLite baze. Uz nju žive ključ čvora (node-key) i ova datoteka."`
	// Velike i rijetko pisane datoteke mogu stajati odvojeno od baze: na
	// poslužitelju baza ide na brzi disk (SSD), a arhiva, izvorne datoteke,
	// skenovi i paketi na veliki (mehanički). Prazno znači uz bazu.
	Arhiva  string `toml:"arhiva" comment:"Arhiva vodostaja i meteorologije (vodostaji.db, oko 10 GB).\nPrazno = uz bazu. Na poslužitelju može na veliki disk; SQLite ne\nsmije ležati na mrežnoj ili FUSE mapi (na Unraidu /mnt/diskN, ne /mnt/user)."`
	Podaci  string `toml:"podaci" comment:"Stablo s izvornim datotekama arhive (mapa vodostaji). Prazno = \"vodostaji\"."`
	Skenovi string `toml:"skenovi" comment:"Mapa sa skenovima (prijave s terena). Prazno = mapa skenovi uz bazu."`
	Pakete  string `toml:"pakete" comment:"Mapa u koju se izdaju .cop paketi. Prazno = \"pakete\"."`
	// JavnaAdresa je adresa na kojoj je program dostupan izvana, npr.
	// https://gocop.voda.hr; ide u QR kod na dokumentima. Prazno: QR nosi
	// samo oznaku dokumenta.
	JavnaAdresa string `toml:"javna_adresa" comment:"Javna adresa programa za QR kodove na dokumentima, npr. https://gocop.voda.hr.\nPrazno dok je nema."`

	Web struct {
		PouzdaniPosrednici []string `toml:"pouzdani_posrednici" comment:"Adrese ili mreže (CIDR) posrednika kojima se vjeruje zaglavlje s adresom\nklijenta, npr. [\"172.17.0.1\"] za cloudflared koji u Docker dolazi preko\nmosta. Prazno = ovo računalo i privatne mreže. Zahtjev sa zaglavljem\nposrednika uvijek se smatra vanjskim."`
		ZaglavljeKlijenta  string   `toml:"zaglavlje_klijenta" comment:"Zaglavlje u kojem posrednik šalje adresu klijenta. Prazno = CF-Connecting-IP\n(Cloudflare tunel); iza nginxa X-Forwarded-For."`
	} `toml:"web"`

	Node struct {
		ID   string `toml:"id" comment:"Jedinstveno ime ovog čvora u mreži — npr. cop-osijek-unraid, pperic-thinkpad:\nmala slova, brojke i crtica. Ne mijenjajte nakon prvog pokretanja: pod njim\nčvor upisuje svoje zapise, a drugi čvorovi ga pamte uz ključ."`
		Name string `toml:"name" comment:"Naziv koji vide drugi čvorovi pri uparivanju. Prazno = ime računala."`
	} `toml:"node"`

	Support struct {
		Center      string `toml:"centar" comment:"Centar obrane od poplava ovog čvora, npr. \"COP Osijek\".\nPrazno = redak o dežurnom operateru se ne prikazuje."`
		CenterPhone string `toml:"centar_telefon" comment:"Telefon tog centra; na njemu se javlja dežurni operater\nza vrijeme obrane od poplava."`
	} `toml:"kontakt" comment:"Centar koji stoji na stranici prijave. Svaki čvor upisuje svoj:\nprogram vrijedi za cijelu Hrvatsku, pa Osijek nije zadano za sve.\nOsobu za pomoć oko prijave program uzima iz registra: glavnog\nadministratora s upisanim mobitelom ili e-poštom."`

	Readings struct {
		HistoryMonths int `toml:"povijest_mjeseci" comment:"Koliko mjeseci očitanja ovaj čvor drži iz razmjene s drugima.\n0 = sve. Na terenskom uređaju stavite 12: povijest od stotinjak\ngodina ne stane na telefon, a na nasipu ne treba. Ograda ne dira\nočitanja koja čvor sam upiše ili uveze."`
	} `toml:"vodostaji"`

	// Karta je izvor pločica za prikaz položaja letve. Zadano je Wikimedijin
	// besplatni poslužitelj; kad se pločice jednom preuzmu za područje
	// obrane, ovdje se upiše lokalna putanja i karta radi bez interneta.
	Karta struct {
		Plocice  string `toml:"plocice" comment:"Predložak URL-a pločica, s {z}/{x}/{y}. Prazno isključuje kartu.\nZadano je OpenStreetMap (tile.openstreetmap.org). Za rad bez interneta\nupišite lokalnu putanju, npr. \"/karta/{z}/{x}/{y}.png\"."`
		Zasluge  string `toml:"zasluge" comment:"Natpis o podrijetlu karte. Obvezan je: pločice se koriste pod\nuvjetima onoga tko ih daje."`
		NajviseZ int    `toml:"najvise_z" comment:"Najveće približavanje. Više od 17 rijetko treba, a povlači\nmnogo više pločica kad se jednom budu preuzimale."`
	} `toml:"karta"`

	Posta struct {
		Nacin       string `toml:"nacin" comment:"ews (zadano): Exchange Web Services, isto kao Outlook, prijava sustava\nWindows (NTLM), radi i izvan mreže tvrtke i sprema poruku u Poslano.\nsmtp: slanje na port 587/465, kad ga tvrtka otvori."`
		Posluzitelj string `toml:"posluzitelj" comment:"Poslužitelj e-pošte tvrtke (Exchange) preko kojeg goCOP šalje ovjerene akte\nprimateljima \"na znanje\", npr. \"owa.voda.hr\". Prazno isključuje slanje.\nŠalje se s adrese prijavljenog korisnika; lozinku svaki korisnik upisuje\nu svom profilu i ona ostaje samo na ovom računalu, šifrirana."`
		Domena      string `toml:"domena" comment:"Obično prazno: domenu sustava Windows (npr. VODA) Exchange sam objavi pa\ngoCOP, kad prijava upisanim imenom ne prođe, pokuša i DOMENA\\korisnik.\nUpišite samo ako ta automatika ne radi."`
		Port        int    `toml:"port" comment:"Samo za smtp: 587 za STARTTLS (zadano), 465 za izravni TLS."`
		Sigurnost   string `toml:"sigurnost" comment:"Samo za smtp: starttls (zadano) ili tls."`
	} `toml:"posta"`

	Sync struct {
		ExchangePort  int      `toml:"exchange_port" comment:"Port razmjene verzija s drugim čvorovima. 0 isključuje razmjenu."`
		PairPort      int      `toml:"pair_port" comment:"Port uparivanja (samo dok uparivanje traje)."`
		DiscoveryPort int      `toml:"discovery_port" comment:"UDP port pronalaženja na lokalnoj mreži. 0 isključuje."`
		AutoSync      string   `toml:"auto_sync" comment:"Razmak automatske sinkronizacije sa svim poznatim čvorovima, npr. \"5m\", \"1h\". \"0\" isključuje."`
		Bootstrap     []string `toml:"bootstrap" comment:"Stalno izloženi čvorovi (domene) preko kojih se pronalaze ostali,\nnpr. [\"cop-osijek.com\", \"cop.voda.hr\"]. Popis raste; svaki dodatni ubrzava\ni osigurava pronalaženje ako jedan padne."`
		All           bool     `toml:"sve" comment:"true = ovaj čvor prati sve kanale: sva očitanja i dnevnike svih područja i\ngodina (uredski poslužitelj). false = prati samo što je označeno u\nprogramu (laptop, mobitel)."`
	} `toml:"sync"`
}

// Default su zadane vrijednosti — iste kao bez ikakve datoteke
func Default() Config {
	var c Config
	c.Addr = ":80"
	c.DB = "data/gocop.db"
	// Node.ID namjerno prazan: ime čvora mora biti jedinstveno, pa ga čvor
	// bez upisanog imena izabere sam pri prvom pokretanju (OdrediIme)
	c.Sync.ExchangePort = 4710
	c.Sync.PairPort = 4711
	c.Sync.DiscoveryPort = 4712
	c.Sync.AutoSync = "5m"
	c.Sync.Bootstrap = []string{"cop-osijek.com"}
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
	cfg := Default()
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return cfg, path, err
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return cfg, path, fmt.Errorf("%s: %w", path, err)
		}
		return cfg, path, nil
	}
	return cfg, "", nil
}

// WriteExample zapisuje datoteku sa zadanim vrijednostima i komentarima —
// samo ako ne postoji, da nikad ne pregazi korisnikove izmjene
func WriteExample(path string, cfg Config) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return false, err
	}
	header := "# goCOP — postavke\n" +
		"# Uredite prije pokretanja. Zastavice na naredbenom retku imaju prednost\n" +
		"# pred ovom datotekom. Nakon izmjene ponovno pokrenite aplikaciju.\n\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(header+string(data)), 0o644); err != nil {
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
