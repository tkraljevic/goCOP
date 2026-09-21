package pegelonline

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCitajUzimaSamoPuniSat(t *testing.T) {
	put := filepath.Join(t.TempDir(), "niz.json")
	sadrzaj := `[
  {"timestamp":"2026-08-21T01:15:00+02:00","value":252},
  {"timestamp":"2026-08-21T02:00:00+02:00","value":253},
  {"timestamp":"2026-08-21T02:15:00+02:00","value":254},
  {"timestamp":"2026-08-21T03:00:00+02:00","value":255}
]`
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Citaj(put)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Vodostaji) != 2 || r.Vodostaji[0].Vrijednost != 253 || r.Izvjestaj.IzvanPunogSata != 2 {
		t.Fatalf("rezultat: %+v", r)
	}
	ocekivano := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	if !r.Vodostaji[0].Vrijeme.Equal(ocekivano) {
		t.Fatalf("vremenska zona nije pretvorena u UTC: %s", r.Vodostaji[0].Vrijeme)
	}
}
