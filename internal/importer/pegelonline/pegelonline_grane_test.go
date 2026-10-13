package pegelonline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava neispravna očitanja, isti sat zapisan dvaput (u istoj ili drugoj
// zoni) i greške datoteke PEGELONLINE izvoza.

func zapisi(t *testing.T, sadrzaj string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "niz.json")
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
	return put
}

func TestCitajNeispravneIDvostruke(t *testing.T) {
	put := zapisi(t, `[
  {"timestamp":"2026-08-21T02:00:00+02:00","value":253},
  {"timestamp":"2026-08-21T00:00:00Z","value":253},
  {"timestamp":"2026-08-21T03:00:00+02:00","value":255},
  {"timestamp":"2026-08-21T01:00:00Z","value":256},
  {"timestamp":"2026-08-21T04:00:00+02:00","value":254},
  {"timestamp":"2026-08-21T02:00:00Z","value":254},
  {"timestamp":"21.08.2026 04:00","value":257},
  {"timestamp":"2026-08-21T05:00:00+02:00","value":2500}
]`)
	r, err := Citaj(put)
	if err != nil {
		t.Fatal(err)
	}
	iz := r.Izvjestaj
	// tri sata zapisana dvaput, samo jedan s različitom vrijednošću
	if iz.Redaka != 8 || iz.Neispravnih != 2 || iz.Duplikata != 3 || iz.Sukoba != 1 || iz.IzvanPunogSata != 0 {
		t.Fatalf("izvještaj: %+v", iz)
	}
	// kod sukoba vrijedi zadnje očitanje
	if len(r.Vodostaji) != 3 || r.Vodostaji[0].Vrijednost != 253 || r.Vodostaji[1].Vrijednost != 256 || r.Vodostaji[2].Vrijednost != 254 {
		t.Errorf("vodostaji: %+v", r.Vodostaji)
	}
}

func TestCitajGreske(t *testing.T) {
	if _, err := Citaj(filepath.Join(t.TempDir(), "nema.json")); err == nil {
		t.Error("nepostojeća datoteka je prošla")
	}
	if _, err := Citaj(zapisi(t, `{"timestamp":"2026-08-21T02:00:00Z"}`)); err == nil || !strings.Contains(err.Error(), "niz.json: json: cannot unmarshal") {
		t.Errorf("nije popis: %v", err)
	}
	if _, err := Citaj(zapisi(t, `[{"timestamp":"2026-08-21T02:15:00Z","value":253}]`)); err == nil || !strings.HasSuffix(err.Error(), "nema nijednog valjanog očitanja na punom satu") {
		t.Errorf("bez punog sata: %v", err)
	}
}
