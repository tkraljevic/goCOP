package ehyd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Zaključava rubne retke i greške eHYD izvoza: prazan redak i redak bez
// točke-zareza u vrijednostima, vrijednost izvan mogućeg, mjesečni trenutak
// u UTC-u, nečitljivo vrijeme, predug redak te izvoz bez odjeljka Werte,
// bez intervala ili bez ijedne valjane vrijednosti.

func zapisi(t *testing.T, sadrzaj string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "ehyd.csv")
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
	return put
}

const mjesecni = "Exportzeitreihe:;(1,Abfluss,I,Mit,M,1,F,Z,0,,,)\nWerte:\n"

func TestCitajRubneRetke(t *testing.T) {
	put := zapisi(t, mjesecni+"01.01.2024 00:00:00; 12,5\n\nsamo tekst\n01.02.2024 00:00:00; nema\n01.03.2024 00:00:00; -40\n01.04.2024 00:00:00; 13\n")
	r, err := Citaj(put, "protok")
	if err != nil {
		t.Fatal(err)
	}
	if r.Izvjestaj.Redaka != 4 || r.Izvjestaj.Neispravnih != 3 || r.Izvjestaj.Praznina != 0 || len(r.Redci) != 2 {
		t.Fatalf("izvještaj: %+v, redci %+v", r.Izvjestaj, r.Redci)
	}
	// mjesečni niz čuva trenutak, preračunat u UTC
	if !r.Redci[0].Vrijeme.Equal(time.Date(2023, 12, 31, 23, 0, 0, 0, time.UTC)) || r.Redci[0].Vrijeme.Location() != time.UTC ||
		r.Redci[1].Vrijeme.Format(time.RFC3339) != "2024-03-31T22:00:00Z" {
		t.Errorf("vrijeme: %v, %v", r.Redci[0].Vrijeme, r.Redci[1].Vrijeme)
	}
}

func TestCitajGreske(t *testing.T) {
	if _, err := Citaj(filepath.Join(t.TempDir(), "nema.csv"), "protok"); err == nil {
		t.Error("nepostojeća datoteka je prošla")
	}
	for ime, s := range map[string]struct{ sadrzaj, greska string }{
		"vrijeme":         {mjesecni + "1.1.2024; 12\n", `redak 3: vrijeme "1.1.2024" nije čitljivo`},
		"bez Werte":       {"Exportzeitreihe:;(1,Abfluss,I,Mit,T,1,F,Z,0,,,)\n01.01.2024 00:00:00; 12\n", "nije pronađen odjeljak Werte"},
		"bez intervala":   {"Exportzeitreihe:;(1,Abfluss)\nWerte:\n01.01.2024 00:00:00; 12\n", "nije prepoznat interval eHYD niza"},
		"bez vrijednosti": {mjesecni + "01.01.2024 00:00:00; Lücke\n", "nema nijedne valjane vrijednosti"},
		"predug redak":    {mjesecni + strings.Repeat("9", 70000) + "\n", "token too long"},
	} {
		put := zapisi(t, s.sadrzaj)
		if _, err := Citaj(put, "protok"); err == nil || !strings.HasSuffix(err.Error(), s.greska) {
			t.Errorf("%s: %v", ime, err)
		}
	}
}
