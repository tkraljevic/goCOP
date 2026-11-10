package web

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Razdoblje dnevnika na stranici sektora: od–do, samo „od” dok traje, a
// bez početka crtica. Datum je zagrebački, pa ponoć po UTC-u ne pomiče dan.
func TestRazdobljeDnevnikaCOP(t *testing.T) {
	od := time.Date(2026, 3, 13, 23, 30, 0, 0, time.UTC) // 14. 3. u Zagrebu
	do := time.Date(2026, 5, 3, 10, 0, 0, 0, models.Zagreb)
	for _, tc := range []struct {
		j       models.Journal
		pocetak string
		raz     string
	}{
		{models.Journal{StartedAt: &od, EndedAt: &do}, "14. 3. 2026.", "14. 3. 2026. – 3. 5. 2026."},
		{models.Journal{StartedAt: &od}, "14. 3. 2026.", "od 14. 3. 2026."},
		{models.Journal{EndedAt: &do}, "", "—"},
	} {
		r := RedakCOP{Journal: tc.j}
		if got := r.Pocetak(); got != tc.pocetak {
			t.Errorf("početak %q, treba %q", got, tc.pocetak)
		}
		if got := r.Razdoblje(); got != tc.raz {
			t.Errorf("razdoblje %q, treba %q", got, tc.raz)
		}
	}
}
