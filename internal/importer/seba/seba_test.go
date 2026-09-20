package seba

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const glava = "Time,Geolux SmartObserver - Device Temperature [°C],Vodostaj Seba - Average Water Level [m],HydroTemp - Water Temperature [°C]\n"

func TestCitajSpajaIzvozeIDrziDvaNiza(t *testing.T) {
	d := t.TempDir()
	prva := filepath.Join(d, "prva.csv")
	druga := filepath.Join(d, "druga.csv")
	if err := os.WriteFile(prva, []byte(glava+
		"2025-01-01 12:00,5,1.234,8.2\n"+
		"2025-01-01 12:15,5,,8.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(druga, []byte(glava+
		"2025-01-01 12:00,5,1.235,6553.5\n"+
		"2025-01-01 12:30,5,1.236,8.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Citaj([]string{prva, druga})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Vodostaji) != 2 || math.Abs(r.Vodostaji[0].Vrijednost-123.5) > 1e-9 || math.Abs(r.Vodostaji[1].Vrijednost-123.6) > 1e-9 {
		t.Fatalf("vodostaji: %+v", r.Vodostaji)
	}
	if len(r.Temperature) != 3 || r.Temperature[0].Vrijednost != 8.2 {
		t.Fatalf("temperature: %+v", r.Temperature)
	}
	if r.Izvjestaj.Duplikata != 1 || r.Izvjestaj.NeispravnihTemperatura != 1 {
		t.Fatalf("izvještaj: %+v", r.Izvjestaj)
	}
	ocekivano, _ := time.Parse(time.RFC3339, "2025-01-01T11:00:00Z")
	if !r.Vodostaji[0].Vrijeme.Equal(ocekivano) {
		t.Errorf("lokalno vrijeme nije pretvoreno u UTC: %s", r.Vodostaji[0].Vrijeme)
	}
}
