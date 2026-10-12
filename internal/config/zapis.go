package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Provjeri javlja što u postavkama ne valja, po punom ključu, riječima za
// čovjeka. Pri pokretanju program neispravan razmak sinkronizacije tiho
// shvati kao isključeno; ovdje se takav upis odbija, jer ga je netko upravo
// upisao i vjerojatno je mislio nešto drugo.
func Provjeri(c Config) map[string]string {
	greske := map[string]string{}
	for kljuc, pravilo := range pravila {
		if poruka := pravilo(c); poruka != "" {
			greske[kljuc] = poruka
		}
	}
	return greske
}

var pravila = map[string]func(Config) string{
	"javna_adresa":               func(c Config) string { return provjeriJavnuAdresu(c.JavnaAdresa) },
	"sync.auto_sync":             func(c Config) string { return provjeriRazmak(c.Sync.AutoSync) },
	"sync.bootstrap":             func(c Config) string { return provjeriPopisCvorova(c.Sync.Bootstrap) },
	"sync.exchange_port":         func(c Config) string { return provjeriPort(c.Sync.ExchangePort) },
	"sync.pair_port":             func(c Config) string { return provjeriPort(c.Sync.PairPort) },
	"sync.discovery_port":        func(c Config) string { return provjeriPort(c.Sync.DiscoveryPort) },
	"vodostaji.povijest_mjeseci": func(c Config) string { return provjeriMjesece(c.Readings.HistoryMonths) },
	"karta.plocice":              func(c Config) string { return provjeriPlocice(c.Karta.Plocice) },
	"karta.najvise_z":            func(c Config) string { return provjeriPriblizavanje(c.Karta.NajviseZ) },
}

func provjeriJavnuAdresu(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(s, " \t") {
		return "Upišite punu adresu, npr. https://cop-osijek.com, ili ostavite prazno."
	}
	return ""
}

func provjeriRazmak(s string) string {
	if d, err := time.ParseDuration(strings.TrimSpace(s)); err == nil && d >= 0 {
		return ""
	}
	return "Upišite razmak kao 5m ili 1h; 0 isključuje automatsku sinkronizaciju."
}

func provjeriPopisCvorova(popis []string) string {
	for _, s := range popis {
		if s == "" || strings.ContainsAny(s, " \t,") {
			return "Svaki čvor u svom retku: domena ili adresa, bez razmaka."
		}
	}
	return ""
}

func provjeriPort(p int) string {
	if p < 0 || p > 65535 {
		return "Port je broj od 0 do 65535."
	}
	return ""
}

func provjeriMjesece(n int) string {
	if n < 0 {
		return "Upišite 0 (sve) ili broj mjeseci."
	}
	return ""
}

func provjeriPlocice(s string) string {
	if s == "" || strings.Contains(s, "{z}") && strings.Contains(s, "{x}") && strings.Contains(s, "{y}") {
		return ""
	}
	return "Adresa pločica mora imati {z}, {x} i {y}, ili ostati prazna (bez karte)."
}

func provjeriPriblizavanje(z int) string {
	if z < 0 || z > 19 {
		return "Upišite broj od 0 do 19."
	}
	return ""
}

// ZadrziKopija je koliko se starih inačica gocop.toml čuva uz nju
const ZadrziKopija = 5

// oblikKopije je vrijeme u imenu kopije: gocop.toml.20261012-153000
const oblikKopije = "20060102-150405"

// KopijaPostavki je stara inačica gocop.toml spremljena uz nju
type KopijaPostavki struct {
	Ime      string
	Vrijeme  time.Time
	Velicina int64
}

// Zapisi zapisuje novi sadržaj datoteke postavki. Postojeća prvo ide u
// kopiju (gocop.toml.20261012-153000; čuva se zadnjih pet), pa se nova
// zapiše uz nju i zamijeni je odjednom, s istim dopuštenjima. Vraća ime
// kopije; prazno kad datoteke prije nije bilo.
func Zapisi(put string, sadrzaj []byte, sad time.Time) (string, error) {
	kopija, dopustenja, err := sacuvajStaru(put, sad)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		return kopija, err
	}
	if err := zapisiAtomski(put, sadrzaj, dopustenja); err != nil {
		return kopija, err
	}
	pocistiKopije(put, ZadrziKopija)
	return kopija, nil
}

// sacuvajStaru sprema postojeću datoteku u kopiju i vraća njezina
// dopuštenja; kad datoteke nema, nema ni kopije
func sacuvajStaru(put string, sad time.Time) (string, os.FileMode, error) {
	stari, err := os.ReadFile(put)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0o644, nil
	}
	if err != nil {
		return "", 0, err
	}
	dopustenja := os.FileMode(0o644)
	if fi, err := os.Stat(put); err == nil {
		dopustenja = fi.Mode().Perm()
	}
	kopija, err := zapisiKopiju(put, stari, sad, dopustenja)
	if err != nil {
		return "", 0, fmt.Errorf("kopija stare datoteke nije zapisana: %w", err)
	}
	return kopija, dopustenja, nil
}

// zapisiKopiju sprema stari sadržaj pod imenom s vremenom; dva spremanja u
// istoj sekundi dobiju nastavak -2, -3…
func zapisiKopiju(put string, stari []byte, sad time.Time, dopustenja os.FileMode) (string, error) {
	ime := put + "." + sad.Format(oblikKopije)
	for i := 2; ; i++ {
		if _, err := os.Stat(ime); errors.Is(err, os.ErrNotExist) {
			break
		}
		ime = fmt.Sprintf("%s.%s-%d", put, sad.Format(oblikKopije), i)
	}
	return ime, zapisiAtomski(ime, stari, dopustenja)
}

// zapisiAtomski piše u privremenu datoteku u istoj mapi i preimenuje je
// preko odredišta. Na Windowsu odredište zna nakratko držati drugi program
// (npr. Postava koja čita port), pa se preimenovanje pokuša nekoliko puta.
func zapisiAtomski(put string, sadrzaj []byte, dopustenja os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(put), "."+filepath.Base(put)+"-*")
	if err != nil {
		return err
	}
	privremena := f.Name()
	_, err = f.Write(sadrzaj)
	if err == nil {
		err = f.Sync()
	}
	if errZ := f.Close(); err == nil {
		err = errZ
	}
	if err == nil {
		err = os.Chmod(privremena, dopustenja)
	}
	if err == nil {
		err = preimenuj(privremena, put)
	}
	if err != nil {
		_ = os.Remove(privremena)
	}
	return err
}

// razmakPreimenovanja je razmak između pokušaja preimenovanja
var razmakPreimenovanja = 50 * time.Millisecond

func preimenuj(iz, u string) error {
	var err error
	for range 5 {
		if err = os.Rename(iz, u); err == nil {
			return nil
		}
		time.Sleep(razmakPreimenovanja)
	}
	return err
}

// imeKopije prepoznaje kopije koje piše Zapisi, a ne gocop.toml.primjer
var imeKopije = regexp.MustCompile(`^\d{8}-\d{6}(-\d+)?$`)

// Kopije su stare inačice datoteke put, od najnovije
func Kopije(put string) []KopijaPostavki {
	unosi, err := os.ReadDir(filepath.Dir(put))
	if err != nil {
		return nil
	}
	prefiks := filepath.Base(put) + "."
	var out []KopijaPostavki
	for _, u := range unosi {
		vrijeme, ok := strings.CutPrefix(u.Name(), prefiks)
		if !ok || u.IsDir() || !imeKopije.MatchString(vrijeme) {
			continue
		}
		k := KopijaPostavki{Ime: u.Name()}
		k.Vrijeme, _ = time.ParseInLocation(oblikKopije, vrijeme[:len(oblikKopije)], time.Local)
		if fi, err := u.Info(); err == nil {
			k.Velicina = fi.Size()
		}
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ime > out[j].Ime })
	return out
}

// pocistiKopije briše kopije starije od zadnjih n
func pocistiKopije(put string, n int) {
	kopije := Kopije(put)
	for i := n; i < len(kopije); i++ {
		_ = os.Remove(filepath.Join(filepath.Dir(put), kopije[i].Ime))
	}
}
