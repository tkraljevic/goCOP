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

	Node struct {
		ID   string `toml:"id" comment:"Jedinstveni identifikator ovog čvora — npr. cop-osijek, laptop-vinkovci-1.\nNe mijenjajte nakon prvog uparivanja: drugi čvorovi ga pamte."`
		Name string `toml:"name" comment:"Naziv koji vide drugi čvorovi pri uparivanju. Prazno = ime računala."`
	} `toml:"node"`

	Support struct {
		Center      string `toml:"centar" comment:"Centar obrane od poplava ovog čvora, npr. \"COP Osijek\".\nPrazno = redak o dežurnom operateru se ne prikazuje."`
		CenterPhone string `toml:"centar_telefon" comment:"Telefon tog centra; na njemu se javlja dežurni operater\nza vrijeme obrane od poplava."`
	} `toml:"kontakt" comment:"Centar koji stoji na stranici prijave. Svaki čvor upisuje svoj:\nprogram vrijedi za cijelu Hrvatsku, pa Osijek nije zadano za sve.\nOsobu za pomoć oko prijave program uzima iz registra: glavnog\nadministratora s upisanim mobitelom ili e-poštom."`

	Readings struct {
		HistoryMonths int `toml:"povijest_mjeseci" comment:"Koliko mjeseci očitanja ovaj čvor drži iz razmjene s drugima.\n0 = sve. Na terenskom uređaju stavite 12: povijest od stotinjak\ngodina ne stane na telefon, a na nasipu ne treba. Ograda ne dira\nočitanja koja čvor sam upiše ili uveze."`
	} `toml:"vodostaji"`

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
	c.Node.ID = "gocop-cvor"
	c.Sync.ExchangePort = 4710
	c.Sync.PairPort = 4711
	c.Sync.DiscoveryPort = 4712
	c.Sync.AutoSync = "5m"
	c.Sync.Bootstrap = []string{"cop-osijek.com"}
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
