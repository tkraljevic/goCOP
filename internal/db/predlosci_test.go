package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// koristiPredloske usmjerava prvo punjenje na mapu s predlošcima i vraća
// funkciju koja vraća zatečene putanje.
func koristiPredloske(mapa string) (vrati func()) {
	staraMapa, stariImenik := DataDir, ImenikPath
	DataDir = mapa
	ImenikPath = filepath.Join(mapa, "imenik.json")
	return func() { DataDir, ImenikPath = staraMapa, stariImenik }
}

// Predlošci prvog pokretanja u docs/predlosci/prvo-pokretanje moraju se
// učitati u praznu bazu bez greške i dati ono što u njima piše.
func TestPredlosciPrvogPokretanja(t *testing.T) {
	t.Cleanup(koristiPredloske(filepath.Join("..", "..", "docs", "predlosci", "prvo-pokretanje")))

	baza, err := OpenDB(filepath.Join(t.TempDir(), "predlosci.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if err := SeedInitialData(baza); err != nil {
		t.Fatalf("predlošci se ne učitavaju: %v", err)
	}

	for tablica, zeli := range map[string]int{
		"sectors":             2, // DIREKCIJA i sektor P
		"areas":               2,
		"users":               2, // pperic i račun admin koji se dodaje sam
		"duties":              3, // dvije pperic i jedna admin, na DIREKCIJA
		"counties":            1,
		"municipalities":      2,
		"settlements":         2,
		"sections":            1,
		"section_territories": 2,
		"watercourses":        2,
		"stations":            1,
		"structures":          3, // dva iz objekti_bp16.json i nasip iz dokumentacije dionice
	} {
		var n int
		if err := baza.QueryRow("SELECT COUNT(*) FROM " + tablica).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != zeli {
			t.Errorf("%s: %d zapisa iz predložaka, očekivano %d", tablica, n, zeli)
		}
	}

	// Vodomjer s dionice postaje postaja: naziv bez stacionaže, kota nule iz
	// zagrade i pragovi iz stupaca
	var ime string
	var kota float64
	var prip int
	if err := baza.QueryRow(`SELECT name, zero_datum, prep_cm FROM stations`).Scan(&ime, &kota, &prip); err != nil {
		t.Fatal(err)
	}
	if ime != "Primjerovo" || kota != 81.25 || prip != 300 {
		t.Errorf("postaja iz predloška: %q, kota %v, pripremno %d", ime, kota, prip)
	}

	// Šifra vode izvodi se iz službenog naziva i na nju se dionica poziva
	var voda string
	if err := baza.QueryRow(`SELECT code FROM watercourses WHERE official_name = 'rijeka Primjerica'`).Scan(&voda); err != nil || voda != "rijeka-primjerica" {
		t.Errorf("šifra vode: %q (%v)", voda, err)
	}

	// Objekt s dionice veže se na registar objekata po nazivu
	var dijelovi string
	if err := baza.QueryRow(`SELECT parts FROM sections WHERE code = 'P.1.1'`).Scan(&dijelovi); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dijelovi, `"structure_id":"`+StableID("structure", "P-CS-PROBNI").String()+`"`) {
		t.Errorf("CS Probni s dionice nije vezan na registar objekata: %s", dijelovi)
	}

	var korisnik string
	if err := baza.QueryRow(`SELECT full_name FROM users WHERE username = 'pperic'`).Scan(&korisnik); err != nil || korisnik != "Pero Perić" {
		t.Errorf("korisnik iz imenika: %q (%v)", korisnik, err)
	}
}
