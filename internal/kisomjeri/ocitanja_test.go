package kisomjeri

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
)

// registarSKisomjerom je gocop.db s letvom „lipa” i kišomjerom k-lipa uz nju
func registarSKisomjerom(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	sada := time.Now()
	for _, q := range []string{
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('lipa-id', 'lipa', 'Lipovac', ?, ?)`,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('brod-id', 'brod', 'Brodski', ?, ?)`,
	} {
		if _, err := baza.Exec(q, sada, sada); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := baza.Exec(`INSERT INTO kisomjeri (code, naziv, vrsta, izvor, letva, latitude, longitude, aktivan, created_at, updated_at)
		VALUES ('k-lipa', 'Lipovac kiša', 'stvarni', 'hv', 'lipa', 45.5, 18.5, 1, ?, ?)`, sada, sada); err != nil {
		t.Fatal(err)
	}
	return baza
}

func ocitanjeSKisom(t *testing.T, baza *sql.DB, id, letva string, kad time.Time, kisa any) {
	t.Helper()
	if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, rain_mm, source, created_at, updated_at)
		VALUES (?, ?, ?, 140, ?, 'RUČNO', ?, ?)`, id, letva, kad.UTC(), kisa, kad, kad); err != nil {
		t.Fatal(err)
	}
}

func spremisteOborina(t *testing.T) *Spremiste {
	t.Helper()
	ob, err := db.OpenDB(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ob.Close() })
	s := &Spremiste{DB: ob}
	if err := s.Pripremi(); err != nil {
		t.Fatal(err)
	}
	return s
}

// Kiša upisana uz očitanje 22. 11. u 7 h ide na kišomjer uz letvu kao
// dnevna kiša 21. 11.; ispravak je zamijeni, a brisanje očitanja odnese.
// Povijesni niz drugih dana ostaje.
func TestKisaSOcitanjaNaKisomjeru(t *testing.T) {
	ctx := context.Background()
	registar := registarSKisomjerom(t)
	sp := spremisteOborina(t)
	u := &Uvoznik{Spremiste: sp, Postaje: func() ([]Postaja, error) { return nil, nil }, Registar: registar}

	jutro := time.Date(2026, 11, 22, 7, 0, 0, 0, Zagreb)
	ponoc := time.Date(2026, 11, 22, 0, 0, 0, 0, Zagreb)
	povijest := Mjerenje{Kisomjer: "k-lipa", Kraj: ponoc.AddDate(0, 0, -5), Sati: 24, Oborina: 3, Izvor: "hv"}
	isti := Mjerenje{Kisomjer: "k-lipa", Kraj: ponoc, Sati: 24, Oborina: 1, Izvor: "hv"}
	if _, err := sp.Upisi(ctx, []Mjerenje{povijest, isti}); err != nil {
		t.Fatal(err)
	}
	ocitanjeSKisom(t, registar, "o1", "lipa-id", jutro, 12.4)
	ocitanjeSKisom(t, registar, "o2", "lipa-id", jutro.Add(-24*time.Hour), nil) // bez kiše
	ocitanjeSKisom(t, registar, "o3", "brod-id", jutro, 7.0)                    // letva bez kišomjera

	dani := func() map[string]float64 {
		t.Helper()
		m, err := sp.Od("k-lipa", ponoc.AddDate(0, 0, -30))
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]float64{}
		for _, x := range m {
			// dan kiše je dan prije kraja razdoblja, kao na stranici kišomjera
			out[x.Kraj.Add(-time.Minute).In(Zagreb).Format("2. 1.")+" "+x.Izvor] = x.Oborina
		}
		return out
	}
	provjeri := func(naziv string, zeli map[string]float64) {
		t.Helper()
		if n, err := u.Preuzmi(ctx); err != nil {
			t.Fatalf("%s: %d, %v", naziv, n, err)
		}
		got := dani()
		if len(got) != len(zeli) {
			t.Errorf("%s: %v, želim %v", naziv, got, zeli)
		}
		for k, v := range zeli {
			if got[k] != v {
				t.Errorf("%s: %s = %v, želim %v (sve: %v)", naziv, k, got[k], v, got)
			}
		}
	}
	provjeri("upis", map[string]float64{"16. 11. hv": 3, "21. 11. hv-ocitanje": 12.4})

	if _, err := registar.Exec(`UPDATE readings SET rain_mm = 13 WHERE id = 'o1'`); err != nil {
		t.Fatal(err)
	}
	provjeri("ispravak", map[string]float64{"16. 11. hv": 3, "21. 11. hv-ocitanje": 13})

	if _, err := registar.Exec(`DELETE FROM readings WHERE id = 'o1'`); err != nil {
		t.Fatal(err)
	}
	provjeri("brisanje", map[string]float64{"16. 11. hv": 3})
}

// Bez registra kiša s očitanja se ne prenosi; registar koji ne odgovara
// javi grešku, a ostali izvori se i dalje upišu.
func TestKisaSOcitanjaGreske(t *testing.T) {
	ctx := context.Background()
	sp := spremisteOborina(t)
	if n, err := (&Uvoznik{Spremiste: sp}).prenesiOcitanja(ctx); n != 0 || err != nil {
		t.Errorf("bez registra: %d, %v", n, err)
	}
	registar := registarSKisomjerom(t)
	if _, err := registar.Exec(`ALTER TABLE readings DROP COLUMN rain_mm`); err != nil {
		t.Fatal(err)
	}
	m := citacMjerenja{{Kisomjer: "k-a", Kraj: time.Date(2026, 11, 22, 7, 0, 0, 0, Zagreb), Sati: 1, Oborina: 1, Izvor: "kisomjer-dhmz"}}
	u := &Uvoznik{Spremiste: sp, Registar: registar, Citaci: map[string]Citac{"dhmz": m},
		Postaje: func() ([]Postaja, error) { return []Postaja{{Code: "k-a", Izvor: "dhmz"}}, nil }}
	if n, err := u.Preuzmi(ctx); err == nil || n != 1 {
		t.Errorf("registar bez stupca kiše: %d, %v", n, err)
	}
	if _, err := sp.DB.Exec(`DROP TABLE izmjerene`); err != nil {
		t.Fatal(err)
	}
	if _, err := sp.ZamijeniIzvor(ctx, IzvorOcitanja, nil); err == nil {
		t.Error("zamjena bez tablice nije javila grešku")
	}
}

// Upis koji ne prođe ostavlja stara mjerenja, a zatvorena baza javi grešku.
func TestZamijeniIzvorNeuspjeh(t *testing.T) {
	ctx := context.Background()
	ob, err := db.OpenDB(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ob.Exec(`CREATE TABLE izmjerene (kisomjer TEXT NOT NULL, kraj INTEGER NOT NULL, sati INTEGER NOT NULL,
		oborina REAL NOT NULL CHECK (oborina >= 0), izvor TEXT NOT NULL, preuzeto INTEGER NOT NULL, PRIMARY KEY (kisomjer, kraj, sati))`); err != nil {
		t.Fatal(err)
	}
	sp := &Spremiste{DB: ob}
	kraj := time.Date(2026, 11, 22, 0, 0, 0, 0, Zagreb)
	if _, err := sp.ZamijeniIzvor(ctx, IzvorOcitanja, []Mjerenje{{Kisomjer: "k-a", Kraj: kraj, Sati: 24, Oborina: 5}}); err != nil {
		t.Fatal(err)
	}
	if _, err := sp.ZamijeniIzvor(ctx, IzvorOcitanja, []Mjerenje{{Kisomjer: "k-a", Kraj: kraj, Sati: 24, Oborina: -1}}); err == nil {
		t.Error("upis koji krši provjeru je prošao")
	}
	if m, err := sp.Od("k-a", kraj.Add(-48*time.Hour)); err != nil || len(m) != 1 || m[0].Oborina != 5 {
		t.Errorf("neuspjela zamjena dirnula je stara mjerenja: %+v, %v", m, err)
	}
	ob.Close()
	if _, err := sp.ZamijeniIzvor(ctx, IzvorOcitanja, nil); err == nil {
		t.Error("zatvorena baza nije javila grešku")
	}
}
