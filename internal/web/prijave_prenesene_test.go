package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Prijave prenesene iz ranije evidencije ne idu u Za riješiti: nitko ih
// neće riješiti, pa bi redak „starijih prijava” stajao zauvijek. Prave
// prijave, nove i starije, ostaju kao i prije.
func TestZaRijesitiBezPrenesenihPrijava(t *testing.T) {
	sad := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	objava := func(prije time.Duration) *time.Time { t := sad.Add(-prije); return &t }
	prijava := func(naslov string, prije time.Duration, prenesena bool) models.PrijavaSTerena {
		return models.PrijavaSTerena{Naslov: naslov, Datum: sad.Add(-prije), ObjavljenoAt: objava(prije), Status: models.PrijavaObjavljena, Rekonstrukcija: prenesena}
	}
	prenesene := []models.PrijavaSTerena{
		prijava("Procjeđivanje 2023.", 3*365*24*time.Hour, true),
		prijava("Odron 2024.", 2*365*24*time.Hour, true),
	}
	if redovi := redoviPrijava(prenesene, "B", sad); len(redovi) != 0 {
		t.Errorf("samo prenesene prijave: %+v", redovi)
	}

	sve := append(prenesene, prijava("Pukotina", time.Hour, false), prijava("Erozija", 30*24*time.Hour, false))
	redovi := redoviPrijava(sve, "B", sad)
	if len(redovi) != 2 || !strings.HasPrefix(redovi[1].Glavno, "1 starija prijava s terena nije riješena") {
		t.Errorf("nova i jedna starija: %+v", redovi)
	}
}
