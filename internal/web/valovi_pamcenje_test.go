package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Zapamćeni izračun smije se koristiti samo dok su i podaci i pragovi isti.
// Pragovi stoje na kraju otiska da se njihov dio može usporediti zasebno —
// tako se između dvije provjere arhive prepozna promjena praga, koja se
// dogodi u programu i ne čeka minutu.
func TestOtisakValovaRazlikujePodatkeIPragove(t *testing.T) {
	pragovi := []models.PragObrane{
		{Faza: models.PhasePrep, Cm: 300},
		{Faza: models.PhaseRegular, Cm: 500},
	}
	drugi := []models.PragObrane{
		{Faza: models.PhasePrep, Cm: 320},
		{Faza: models.PhaseRegular, Cm: 500},
	}
	zadnje := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)

	a := otisakValova(pragovi, zadnje, 269304)
	if got := otisakValova(pragovi, zadnje, 269304); got != a {
		t.Error("isti ulaz mora dati isti otisak")
	}
	if otisakValova(pragovi, zadnje, 269305) == a {
		t.Error("novo mjerenje mora promijeniti otisak")
	}
	if otisakValova(pragovi, zadnje.Add(time.Hour), 269304) == a {
		t.Error("novije zadnje mjerenje mora promijeniti otisak")
	}
	if otisakValova(drugi, zadnje, 269304) == a {
		t.Error("promjena praga mora promijeniti otisak")
	}

	// Sam dio s pragovima mora biti prepoznatljiv na kraju cijelog otiska.
	samoPragovi := otisakValova(pragovi, time.Time{}, 0)
	if !strings.HasSuffix(a, samoPragovi) {
		t.Errorf("pragovi nisu na kraju otiska: %q u %q", samoPragovi, a)
	}
	if strings.HasSuffix(a, otisakValova(drugi, time.Time{}, 0)) {
		t.Error("drugi pragovi ne smiju odgovarati kraju otiska")
	}
}
