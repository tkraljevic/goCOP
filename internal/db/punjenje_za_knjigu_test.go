package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// oznakePunjenja broji oznake za knjigu po entitetu
func oznakePunjenja(t *testing.T, baza *sql.DB) map[string]int {
	t.Helper()
	rows, err := baza.Query(`SELECT entity, COUNT(*) FROM punjenje_za_knjigu GROUP BY entity`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	m := map[string]int{}
	for rows.Next() {
		var e string
		var n int
		if err := rows.Scan(&e, &n); err != nil {
			t.Fatal(err)
		}
		m[e] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return m
}

// Prvo punjenje označi svaki redak koji je upisalo: račune iz imenika bez
// računa admin, njihove dužnosti, sektore i područja. Drugo punjenje ne
// dodaje ništa.
func TestPunjenjeOznaciUpisaneRetke(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	ocekivano := map[string]int{entitetSektora: 3, entitetPodrucja: 3, entitetRacuna: 5, entitetDuznosti: 7}
	provjeriOznake(t, baza, ocekivano)
	var admin int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM punjenje_za_knjigu
		WHERE entity_id = ? OR entity_id IN (SELECT id FROM duties WHERE user_id = ?)`,
		StableID("user", "admin").String(), StableID("user", "admin").String()).Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if admin != 0 {
		t.Errorf("račun admin ili njegova dužnost ima %d oznaka", admin)
	}
	var bezRetka int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM punjenje_za_knjigu p WHERE NOT EXISTS (
		SELECT 1 FROM users WHERE p.entity = 'users' AND id = p.entity_id UNION ALL
		SELECT 1 FROM duties WHERE p.entity = 'duties' AND id = p.entity_id UNION ALL
		SELECT 1 FROM sectors WHERE p.entity = 'sectors' AND id = p.entity_id UNION ALL
		SELECT 1 FROM areas WHERE p.entity = 'areas' AND CAST(id AS TEXT) = p.entity_id)`).Scan(&bezRetka); err != nil {
		t.Fatal(err)
	}
	if bezRetka != 0 {
		t.Errorf("%d oznaka nema svoj redak", bezRetka)
	}

	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	provjeriOznake(t, baza, ocekivano)
}

func provjeriOznake(t *testing.T, baza *sql.DB, ocekivano map[string]int) {
	t.Helper()
	oznake := oznakePunjenja(t, baza)
	for e, n := range ocekivano {
		if oznake[e] != n {
			t.Errorf("%s: %d oznaka, očekivano %d", e, oznake[e], n)
		}
	}
	if len(oznake) != len(ocekivano) {
		t.Errorf("oznake %v, očekivano %v", oznake, ocekivano)
	}
}

// Računalo koje je već u mreži ništa ne označava: djelatnike i
// organizaciju daje mreža
func TestPunjenjeUMreziNeOznacava(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	if _, err := baza.Exec(`INSERT INTO network (id, name, public_key, joined_at) VALUES (1, 'Primjerska mreža', 'javni-kljuc', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 6 {
		t.Fatalf("u bazi je %d računa (%v), očekivano 6", n, err)
	}
	provjeriOznake(t, baza, map[string]int{})
}

// odbaciPunjenje izvodi OdbaciPunjenje u svojoj transakciji
func odbaciPunjenje(baza *sql.DB) (int64, error) {
	tx, err := baza.Begin()
	if err != nil {
		return 0, err
	}
	n, err := OdbaciPunjenje(context.Background(), tx)
	if err != nil {
		return 0, errors.Join(err, tx.Rollback())
	}
	return n, tx.Commit()
}

// Odbačene oznake nestanu sve odjednom, a bez tablice oznaka odbacivanje
// javlja grešku
func TestOdbaciPunjenje(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	if n, err := odbaciPunjenje(baza); err != nil || n != 18 {
		t.Fatalf("odbačeno %d oznaka (%v), očekivano 18", n, err)
	}
	provjeriOznake(t, baza, map[string]int{})
	if _, err := baza.Exec(`DROP TABLE punjenje_za_knjigu`); err != nil {
		t.Fatal(err)
	}
	if _, err := odbaciPunjenje(baza); err == nil {
		t.Error("odbacivanje bez tablice oznaka nije javilo grešku")
	}
}

// Sektor koji se u datoteci ponavlja upiše se i označi jednom
func TestPunjenjeDvojnikIzDatotekeOznaciJednom(t *testing.T) {
	mapa := t.TempDir()
	org := `{"sectors": [{"id": "P", "name": "Sektor P", "level": 2}, {"id": "P", "name": "Opet P", "level": 2}],
		"areas": [{"id": 1, "sector_id": "P", "name": "Područje 1"}, {"id": 1, "sector_id": "P", "name": "Opet 1"}]}`
	if err := os.WriteFile(filepath.Join(mapa, "organizacija.json"), []byte(org), 0o600); err != nil {
		t.Fatal(err)
	}
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	DataDir, ImenikPath = mapa, filepath.Join(mapa, "imenik.json")
	baza := bazaZaPunjenje(t)
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	provjeriOznake(t, baza, map[string]int{entitetSektora: 1, entitetPodrucja: 1})
}

// Područje koje je u bazi bilo prije punjenja ne dobiva oznaku: punjenje ga
// nije upisalo
func TestPunjenjeZateceniRedakBezOznake(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	// zatečeno područje bez sektora ostaje samo uz isključene strane
	// ključeve, na jednoj vezi
	baza.SetMaxOpenConns(1)
	for _, upit := range []string{
		`PRAGMA foreign_keys = OFF`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Zatečeno', 'VGI')`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := baza.Exec(upit); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	provjeriOznake(t, baza, map[string]int{entitetSektora: 3, entitetPodrucja: 2, entitetRacuna: 5, entitetDuznosti: 7})
	var n int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM punjenje_za_knjigu WHERE entity = 'areas' AND entity_id = '1'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("zatečeno područje ima %d oznaka (%v)", n, err)
	}
}

// Kad se oznaka ne da upisati, punjenje javlja grešku i ne ostavlja
// djelatnike bez oznake
func TestPunjenjeBezTabliceOznakaPada(t *testing.T) {
	t.Run("organizacija", func(t *testing.T) {
		KoristiIzmisljenePodatke(t)
		baza := bazaZaPunjenje(t)
		if _, err := baza.Exec(`DROP TABLE punjenje_za_knjigu`); err != nil {
			t.Fatal(err)
		}
		if _, err := SeedInitialData(baza); err == nil {
			t.Fatal("punjenje bez tablice oznaka nije javilo grešku")
		}
	})
	// dužnost bez sektora, da upis ne padne prije oznake
	raw := []byte(`[{"username": "pperic", "full_name": "Pero Perić", "org_type": "HRVATSKE_VODE",
		"duties": [{"title": "Dežurni", "role": "OPERATOR", "scope_type": "ALL"}]}]`)
	for ime, kvar := range map[string]string{
		"račun":   `DROP TABLE punjenje_za_knjigu`,
		"dužnost": `CREATE TRIGGER bez_duznosti BEFORE INSERT ON punjenje_za_knjigu WHEN NEW.entity = 'duties' BEGIN SELECT RAISE(ABORT, 'ne'); END`,
	} {
		t.Run(ime, func(t *testing.T) {
			samoImenik(t, raw)
			baza := bazaZaPunjenje(t)
			if _, err := baza.Exec(kvar); err != nil {
				t.Fatal(err)
			}
			if _, err := SeedInitialData(baza); err == nil {
				t.Fatal("punjenje bez oznake nije javilo grešku")
			}
			var n int
			if err := baza.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("nakon greške u bazi je %d računa (%v)", n, err)
			}
		})
	}
}

// Račun admin iz imenika se ne označava, kao ni onaj koji program doda
func TestPunjenjeAdminIzImenikaBezOznake(t *testing.T) {
	samoImenik(t, []byte(`[
		{"username": "admin", "full_name": "Ana Anić", "is_global_admin": true, "org_type": "HRVATSKE_VODE",
		 "duties": [{"title": "Administratorica", "role": "GLOBAL_ADMIN", "scope_type": "ALL"}]},
		{"username": "pperic", "full_name": "Pero Perić", "org_type": "HRVATSKE_VODE",
		 "duties": [{"title": "Dežurni", "role": "OPERATOR", "scope_type": "ALL"}]}]`))
	baza := bazaZaPunjenje(t)
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	provjeriOznake(t, baza, map[string]int{entitetRacuna: 1, entitetDuznosti: 1})
	var id string
	if err := baza.QueryRow(`SELECT entity_id FROM punjenje_za_knjigu WHERE entity = 'users'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != StableID("user", "pperic").String() {
		t.Errorf("označen je račun %s, a ne pperic", id)
	}
}

// samoImenik usmjerava punjenje na mapu u kojoj je samo imenik sa sadržajem
// imenik
func samoImenik(t *testing.T, imenik []byte) {
	t.Helper()
	mapa := t.TempDir()
	if err := os.WriteFile(filepath.Join(mapa, "imenik.json"), imenik, 0o600); err != nil {
		t.Fatal(err)
	}
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	DataDir, ImenikPath = mapa, filepath.Join(mapa, "imenik.json")
}
