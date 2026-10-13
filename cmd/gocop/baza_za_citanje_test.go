package main

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava da je veza otvorena za čitanje stvarno samo za čitanje, na
// običnom putu i na putu s razmakom, „#”, „%” i „?”. S „?mode=ro” iza običnog
// puta upis je prolazio, a baza koje nema nastajala je prazna.
func TestSamoZaCitanjeNePise(t *testing.T) {
	for _, ime := range []string{"podaci", "Moji podaci #1 %20?da"} {
		mapa := filepath.Join(t.TempDir(), ime)
		put := bazaUMapi(t, mapa)
		ro, err := sql.Open("sqlite", samoZaCitanje(put))
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		var letva string
		if err := ro.QueryRow(`SELECT letva FROM nizovi`).Scan(&letva); err != nil || letva != "vodograd" {
			t.Fatalf("%s: čitanje: %q, %v", ime, letva, err)
		}
		if _, err := ro.Exec(`INSERT INTO nizovi VALUES ('drugovo')`); err == nil || !strings.Contains(err.Error(), "readonly") {
			t.Errorf("%s: upis kroz vezu za čitanje: %v", ime, err)
		}
		if ima, _ := os.ReadDir(filepath.Dir(mapa)); len(ima) != 1 {
			t.Errorf("%s: uz mapu je nastalo još nešto: %v", ime, ima)
		}

		nema := filepath.Join(mapa, "nema.db")
		prazna, err := sql.Open("sqlite", samoZaCitanje(nema))
		if err != nil {
			t.Fatal(err)
		}
		defer prazna.Close()
		if err := prazna.Ping(); err == nil {
			t.Errorf("%s: baza koje nema otvorila se za čitanje", ime)
		}
		if _, err := os.Stat(nema); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: veza za čitanje napravila je bazu koje nije bilo: %v", ime, err)
		}
	}
}

// bazaUMapi slaže malu bazu s jednim nizom u zadanoj mapi. Baza nastaje na
// običnom putu pa se premjesti, jer bi običan put s „?” otvorio drugu
// datoteku.
func bazaUMapi(t *testing.T, mapa string) string {
	t.Helper()
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	privremena := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", privremena)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE nizovi (letva TEXT); INSERT INTO nizovi VALUES ('vodograd')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	put := filepath.Join(mapa, "arhiva.db")
	if err := os.Rename(privremena, put); err != nil {
		t.Fatal(err)
	}
	return put
}

// Zaključava da program bazu za čitanje otvara samo preko samoZaCitanje:
// „?mode=ro” dopisan običnom putu ne brani upis.
func TestBazaZaCitanjeSamoPrekoPomocnika(t *testing.T) {
	puts, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range puts {
		if strings.HasSuffix(p, "_test.go") || p == "baza_za_citanje.go" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"?mode=ro`) {
			t.Errorf("%s otvara bazu s „?mode=ro” iza običnog puta; treba samoZaCitanje", p)
		}
	}
}
