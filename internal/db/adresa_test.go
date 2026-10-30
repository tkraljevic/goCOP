package db

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Zaključava da glavna baza u mapi s razmakom, „#”, „%” i „?” u imenu nastaje
// baš na zadanom putu i s postavkama iz adrese. Dok se „?” s postavkama
// dopisivao običnom putu, SQLite je „?” iz imena mape čitao kao početak
// postavki: baza je nastajala drugdje, a postavke se nisu primijenile.
func TestGlavnaBazaUMapiSUpitnikom(t *testing.T) {
	put := filepath.Join(t.TempDir(), "Moji podaci #1 %20?da", "gocop.db")
	baza, err := OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`CREATE TABLE proba (x)`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(put); err != nil {
		t.Fatalf("baze nema na zadanom putu: %v", err)
	}
	var mod string
	var kljucevi, synchronous int
	if err := baza.QueryRow(`PRAGMA journal_mode`).Scan(&mod); err != nil || mod != "wal" {
		t.Errorf("journal_mode %q, %v; treba wal", mod, err)
	}
	if err := baza.QueryRow(`PRAGMA foreign_keys`).Scan(&kljucevi); err != nil || kljucevi != 1 {
		t.Errorf("foreign_keys %d, %v; treba 1", kljucevi, err)
	}
	if err := baza.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil || synchronous != 2 {
		t.Errorf("synchronous %d, %v; treba 2 (FULL)", synchronous, err)
	}
}

// Baza u memoriji i adresa koja već počinje s „file:” ne šifriraju se iznova,
// a postavke se dopišu odgovarajućim znakom
func TestAdresaBezPonovnogSifriranja(t *testing.T) {
	for _, s := range []struct{ put, ocekivano string }{
		{":memory:", ":memory:?_pragma=busy_timeout(5000)"},
		{"file:proba?mode=memory", "file:proba?mode=memory&_pragma=busy_timeout(5000)"},
		{"/podaci/a?b#c%d.db", "file:/podaci/a%3Fb%23c%25d.db?_pragma=busy_timeout(5000)"},
	} {
		if a := Adresa(s.put, "_pragma=busy_timeout(5000)"); a != s.ocekivano {
			t.Errorf("Adresa(%q) = %q, treba %q", s.put, a, s.ocekivano)
		}
	}
	if a := Adresa("/podaci/gocop.db"); a != "file:/podaci/gocop.db" {
		t.Errorf("adresa bez postavki: %q", a)
	}
}

// Zaključava da program adresu baze s postavkama slaže samo pomoćnicima
// (Adresa, ZaPisanje, SamoZaCitanje), u cmd/ i u internal/. Postavke
// dopisane običnom putu kvare adresu kad u imenu mape stoji „?” ili „#”.
func TestAdresaBazeSamoPrekoPomocnika(t *testing.T) {
	sirovo := regexp.MustCompile(`"\?_|\+\s*(gocop)?db\.TrajniUpis`)
	pregledano := 0
	for _, mapa := range []string{filepath.Join("..", "..", "cmd"), filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(mapa, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			pregledano++
			for i, redak := range strings.Split(string(b), "\n") {
				if sirovo.MatchString(redak) {
					t.Errorf("%s:%d slaže adresu baze bez pomoćnika: %s", p, i+1, strings.TrimSpace(redak))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if pregledano < 100 {
		t.Errorf("pregledano je samo %d datoteka; test ne gleda kamo treba", pregledano)
	}
}
