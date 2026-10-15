package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func napraviMapu(t *testing.T, put string) string {
	t.Helper()
	if err := os.MkdirAll(put, 0o755); err != nil {
		t.Fatal(err)
	}
	return put
}

// uProjektu napravi mapu projekta s mapom baze data i pokrene test iz mape
// data, kao Postava; vraća mapu projekta
func uProjektu(t *testing.T) string {
	t.Helper()
	projekt := t.TempDir()
	t.Chdir(napraviMapu(t, filepath.Join(projekt, "data")))
	return projekt
}

// Prazna postavka znači isključeno: nema što tražiti ni predlagati.
func TestPraznaMapaJeIskljucena(t *testing.T) {
	uProjektu(t)
	m := ProvjeriMapu("podaci", "")
	if m.Puna != "" || m.Postoji || m.Relativna() || m.Nedostaje() || m.Drugdje() || m.Pregledna() || m.Prijedlog != "" {
		t.Errorf("prazna postavka: %+v", m)
	}
}

// Apsolutna putanja je izričita: kad mape nema, ne nedostaje, nego se stvara
// kao i prije, i ništa se ne predlaže, ni kad mapa istog imena stoji iznad
// radne mape.
func TestApsolutnaMapa(t *testing.T) {
	projekt := uProjektu(t)
	postoji := napraviMapu(t, filepath.Join(projekt, "vodostaji"))
	m := ProvjeriMapu("podaci", postoji+string(filepath.Separator))
	if m.Puna != postoji || !m.Postoji || m.Relativna() || m.Nedostaje() || !m.Pregledna() {
		t.Errorf("apsolutna koja postoji: %+v", m)
	}

	nema := filepath.Join(projekt, "data", "vodostaji")
	m = ProvjeriMapu("podaci", nema)
	if m.Puna != nema || m.Postoji || m.Nedostaje() || m.Drugdje() || !m.Pregledna() || m.Prijedlog != "" {
		t.Errorf("apsolutna koje nema: %+v", m)
	}
}

// Putanja od korijena diska ne ovisi o radnoj mapi, pa je izričita i ondje
// gdje filepath.IsAbs kaže da nije (Windows).
func TestPutanjaOdKorijenaJeIzricita(t *testing.T) {
	for _, put := range []string{string(filepath.Separator) + "arhiva", "/arhiva/vodostaji"} {
		if !Izricita(put) {
			t.Errorf("%q nije izričita", put)
		}
	}
	for _, put := range []string{"", "vodostaji", filepath.Join("..", "vodostaji")} {
		if Izricita(put) {
			t.Errorf("%q je izričita", put)
		}
	}
}

// Relativna mapa koja postoji računa se od radne mape i ne nedostaje.
func TestRelativnaKojaPostoji(t *testing.T) {
	projekt := uProjektu(t)
	mapa := napraviMapu(t, filepath.Join(projekt, "data", "pakete"))
	m := ProvjeriMapu("pakete", "pakete")
	if m.Puna != mapa || !m.Postoji || !m.Relativna() || m.Nedostaje() || m.Drugdje() || !m.Pregledna() {
		t.Errorf("relativna koja postoji: %+v", m)
	}
}

// Novi čvor (usluga na Linuxu, prvi čvor pod Postavom): relativne mape još
// nema, a ni iznad nje. Mapa nedostaje, ali nije drugdje, pa nastaje pri
// prvom upisu kao i dosad.
func TestRelativnaKojeNemaNaNovomCvoru(t *testing.T) {
	projekt := uProjektu(t)
	m := ProvjeriMapu("podaci", "vodostaji")
	if m.Puna != filepath.Join(projekt, "data", "vodostaji") {
		t.Errorf("puna putanja %q", m.Puna)
	}
	if m.Postoji || !m.Nedostaje() || m.Drugdje() || m.Pregledna() || m.Prijedlog != "" {
		t.Errorf("relativna koje nema: %+v", m)
	}
}

// Laptop pod Postavom: čvor radi iz mape baze (data), relativno stablo traži
// u data/vodostaji, a stablo stoji uz mapu data. Mapa je drugdje i to se
// predlaže, ne prebacuje.
func TestRelativnaKojeNemaAStojiIznad(t *testing.T) {
	projekt := uProjektu(t)
	stablo := napraviMapu(t, filepath.Join(projekt, "vodostaji"))
	m := ProvjeriMapu("podaci", "vodostaji")
	if m.Puna != filepath.Join(projekt, "data", "vodostaji") || m.Postoji || !m.Drugdje() || m.Pregledna() {
		t.Errorf("relativna koje nema: %+v", m)
	}
	if m.Prijedlog != stablo {
		t.Errorf("prijedlog %q, treba %q", m.Prijedlog, stablo)
	}
}

// Datoteka istog imena iznad radne mape nije trag stabla.
func TestDatotekaIznadNijePrijedlog(t *testing.T) {
	projekt := uProjektu(t)
	if err := os.WriteFile(filepath.Join(projekt, "vodostaji"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if m := ProvjeriMapu("podaci", "vodostaji"); m.Drugdje() || m.Prijedlog != "" {
		t.Errorf("datoteka iznad: %+v", m)
	}
}

// Kad radne mape više nema, putanja ostaje kako piše i ništa se ne predlaže.
func TestBezRadneMape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ne da obrisati radnu mapu")
	}
	projekt := uProjektu(t)
	napraviMapu(t, filepath.Join(projekt, "vodostaji"))
	if err := os.Remove(filepath.Join(projekt, "data")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("sustav i dalje zna radnu mapu")
	}
	m := ProvjeriMapu("podaci", "vodostaji")
	if m.Puna != "vodostaji" || !m.Nedostaje() || m.Drugdje() {
		t.Errorf("bez radne mape: %+v", m)
	}
}

// Kad sustav radne mape ne zna, putanja ostaje kako piše i ništa se ne
// predlaže. Radna mapa se ovdje ne briše nego zamijeni, pa se to vidi i na
// Windowsu i macOS-u.
func TestKadSustavNeZnaRadnuMapu(t *testing.T) {
	projekt := uProjektu(t)
	napraviMapu(t, filepath.Join(projekt, "vodostaji"))
	stara := odRadneMape
	t.Cleanup(func() { odRadneMape = stara })
	odRadneMape = func(string) (string, error) { return "", os.ErrNotExist }
	if g := PunaPutanja("vodostaji"); g != "vodostaji" {
		t.Errorf("puna putanja %q", g)
	}
	m := ProvjeriMapu("podaci", "vodostaji")
	if m.Puna != "vodostaji" || !m.Nedostaje() || m.Drugdje() || m.Prijedlog != "" {
		t.Errorf("bez radne mape: %+v", m)
	}
}

// Izvor mapa daje uputi datoteku postavki, punom putanjom, i zastavicu za tu
// postavku; zapamćeni izvor vrijedi za ProvjeriMapu, a bez njega se ne zna.
func TestIzvorMapa(t *testing.T) {
	projekt := uProjektu(t)
	t.Cleanup(func() { ZapamtiIzvorMapa(IzvorMapa{}) })
	iz := IzvorMapa{Postavke: "gocop.toml", Zastavice: map[string]string{"pakete": "-pakete"}}
	postavke := filepath.Join(projekt, "data", "gocop.toml")
	if m := iz.Provjeri("podaci", "vodostaji"); m.Kljuc != "podaci" || m.Postavke != postavke || m.Zastavica != "" {
		t.Errorf("podaci: %+v", m)
	}
	if m := iz.Provjeri("pakete", ""); m.Kljuc != "pakete" || m.Postavke != postavke || m.Zastavica != "-pakete" {
		t.Errorf("pakete: %+v", m)
	}
	if m := ProvjeriMapu("pakete", "pakete"); m.Postavke != "" || m.Zastavica != "" {
		t.Errorf("bez zapamćenog izvora: %+v", m)
	}
	ZapamtiIzvorMapa(iz)
	if m := ProvjeriMapu("pakete", "pakete"); m.Postavke != postavke || m.Zastavica != "-pakete" {
		t.Errorf("sa zapamćenim izvorom: %+v", m)
	}
}

// Uputa kaže zašto čvor mapu traži ondje i gdje se putanja mijenja: uz
// prijedlog upis te mape u datoteku postavki ili nova mapa, bez njega puna
// putanja, a kad je putanja zadana zastavicom, zastavica, jer je jača od
// datoteke.
func TestUputaZaMapuKojeNema(t *testing.T) {
	stablo := Mapa{Kljuc: "podaci", Zadano: "vodostaji", Puna: "/projekt/data/vodostaji",
		Prijedlog: "/projekt/vodostaji", Postavke: "/projekt/data/gocop.toml"}
	if g, treba := stablo.Uputa(), "Postavka podaci = 'vodostaji' ne počinje od korijena diska, pa se računa "+
		"od mape iz koje je goCOP pokrenut. Mapa /projekt/vodostaji postoji. Ako je to stablo ovog čvora, "+
		"upišite podaci = '/projekt/vodostaji' u /projekt/data/gocop.toml i ponovno pokrenite čvor. "+
		"Ako ovdje treba početi novo stablo, napravite mapu /projekt/data/vodostaji."; g != treba {
		t.Errorf("uputa s prijedlogom:\n%s\ntreba:\n%s", g, treba)
	}

	paketi := Mapa{Kljuc: "pakete", Zadano: "pakete", Puna: "/projekt/data/pakete",
		Postavke: "/projekt/data/gocop.toml", Zastavica: "-pakete"}
	if g, treba := paketi.Uputa(), "Zastavica -pakete 'pakete' ne počinje od korijena diska, pa se računa "+
		"od mape iz koje je goCOP pokrenut. Ako mapa paketa stoji drugdje, zadajte punu putanju zastavicom "+
		"-pakete '…' pri pokretanju. Zastavica je jača od datoteke /projekt/data/gocop.toml, pa upis u "+
		"datoteku ne pomaže."; g != treba {
		t.Errorf("uputa sa zastavicom:\n%s\ntreba:\n%s", g, treba)
	}

	bezDatoteke := Mapa{Kljuc: "podaci", Zadano: "vodostaji", Puna: "/projekt/data/vodostaji"}
	if g := bezDatoteke.Uputa(); !strings.HasSuffix(g, " Ako stablo stoji drugdje, upišite podaci = '…' u "+
		"gocop.toml uz bazu i ponovno pokrenite čvor.") {
		t.Errorf("uputa bez datoteke postavki: %s", g)
	}
}
