package db

import (
	"fmt"
	"path/filepath"
	"testing"
)

// Izmišljeni skup sektora P mora se učitati u praznu bazu i dati upravo ono
// na što se oslanjaju testovi djelatnika, uvoza i razmjene: pet izmišljenih
// djelatnika s dužnostima, dionice u dva sektora, letvu na dvije dionice i
// crpnu stanicu vezanu na letvu istog imena.
func TestIzmisljeniSkupSektoraP(t *testing.T) {
	KoristiIzmisljenePodatke(t)

	baza, err := OpenDB(filepath.Join(t.TempDir(), "sektor-p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if err := SeedInitialData(baza); err != nil {
		t.Fatalf("izmišljeni skup se ne učitava: %v", err)
	}

	for tablica, zeli := range map[string]int{
		"sectors":        3, // DIREKCIJA, P i E
		"areas":          3,
		"users":          6, // pet izmišljenih djelatnika i račun admin
		"sections":       6,
		"stations":       4, // Primjerovo, CS Probni, Oglednica, Rubno
		"watercourses":   6,
		"counties":       1,
		"municipalities": 3,
		"settlements":    4,
	} {
		if n := count(t, baza, "SELECT COUNT(*) FROM "+tablica); n != zeli {
			t.Errorf("%s: %d zapisa iz izmišljenog skupa, očekivano %d", tablica, n, zeli)
		}
	}

	for korisnik, zeli := range map[string]string{
		"pperic": "Pero Perić", "aanic": "Ana Anić", "iivic": "Ivo Ivić",
		"ivaivic": "Iva Ivić", "mmarkic": "Marko Markić",
	} {
		var ime string
		var admin int
		if err := baza.QueryRow(`SELECT full_name, is_global_admin FROM users WHERE username = ?`, korisnik).Scan(&ime, &admin); err != nil || ime != zeli {
			t.Errorf("%s: %q (%v), očekivano %q", korisnik, ime, err, zeli)
		}
		if (admin == 1) != (korisnik == "iivic") {
			t.Errorf("%s: globalni administrator = %d; administrator je samo Ivo Ivić", korisnik, admin)
		}
	}
	if n := count(t, baza, `SELECT COUNT(*) FROM duties d JOIN users u ON u.id = d.user_id WHERE u.username = 'mmarkic' AND d.role = 'WATER_GUARD'`); n != 1 {
		t.Errorf("Marko Markić ima %d dužnosti vodočuvara, očekivana jedna", n)
	}

	// Primjerovo je mjerodavno za dvije dionice: razmjena uklanja jednu vezu
	if n := count(t, baza, `SELECT COUNT(*) FROM section_stations ss JOIN stations st ON st.id = ss.station_id WHERE st.name = 'Primjerovo'`); n != 2 {
		t.Errorf("Primjerovo je mjerodavno za %d dionica, očekivano 2", n)
	}
	// CS Probni: crpna stanica i letva istog imena, objekt vezan na letvu
	if n := count(t, baza, `SELECT COUNT(*) FROM structures s JOIN stations st ON st.id = s.station_id WHERE s.name = 'CS Probni' AND st.name = 'CS Probni'`); n != 1 {
		t.Error("CS Probni nije vezana na letvu istog imena")
	}
	for _, sifra := range []string{"potok-granicnik-primjerica", "potok-granicnik-rubnica", "kanal-probni"} {
		if n := count(t, baza, `SELECT COUNT(*) FROM watercourses WHERE code = ?`, sifra); n != 1 {
			t.Errorf("voda %s nije u registru", sifra)
		}
	}
}

// lazniTest bilježi što bi testing.T učinio, za provjeru grane bez skupa
type lazniTest struct {
	pao      string
	ciscenja []func()
}

func (l *lazniTest) Helper() {}
func (l *lazniTest) Fatalf(format string, args ...any) {
	l.pao = fmt.Sprintf(format, args...)
}
func (l *lazniTest) Cleanup(f func()) { l.ciscenja = append(l.ciscenja, f) }

// Bez izmišljenog skupa test pada, ne preskače se, i ne dira putanje; sa
// skupom se putanje vraćaju kad test završi.
func TestKoristiIzmisljenePodatkeVracaPutanje(t *testing.T) {
	staraMapa, stariImenik := DataDir, ImenikPath

	lazni := &lazniTest{}
	KoristiIzmisljenePodatke(lazni)
	if lazni.pao != "" || len(lazni.ciscenja) != 1 {
		t.Fatalf("skup nije nađen ili se putanje ne vraćaju: %q, čišćenja %d", lazni.pao, len(lazni.ciscenja))
	}
	if filepath.Base(DataDir) != "sektor-p" || ImenikPath != filepath.Join(DataDir, "imenik.json") {
		t.Errorf("putanje nisu na izmišljenom skupu: %s, %s", DataDir, ImenikPath)
	}
	lazni.ciscenja[0]()
	if DataDir != staraMapa || ImenikPath != stariImenik {
		t.Errorf("putanje nisu vraćene: %s, %s", DataDir, ImenikPath)
	}

	// iz mape bez skupa iznad sebe
	t.Chdir(t.TempDir())
	bez := &lazniTest{}
	KoristiIzmisljenePodatke(bez)
	if bez.pao == "" || len(bez.ciscenja) != 0 || DataDir != staraMapa {
		t.Errorf("bez skupa: poruka %q, čišćenja %d, mapa %s", bez.pao, len(bez.ciscenja), DataDir)
	}
}
