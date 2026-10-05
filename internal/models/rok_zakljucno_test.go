package models

import (
	"testing"
	"time"
)

// „Zaključno s 14. 10.” prestaje 15. 10. u 0 h po hrvatskom vremenu, i preko
// pomaka sata (25. 10. 2026. ima 25 sati); zadnji dan vraća upisani dan
func TestPrestanakNakonDana(t *testing.T) {
	for _, tc := range []struct {
		dan       time.Time
		prestanak string // UTC
	}{
		{time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), "2026-10-14T22:00:00Z"},
		{time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), "2026-10-24T22:00:00Z"}, // prestaje 25. 10. u 0 h ljetnog
		{time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), "2026-10-25T23:00:00Z"}, // prestaje 26. 10. u 0 h zimskog
		{time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), "2026-12-31T23:00:00Z"},
		{time.Date(2027, 3, 27, 0, 0, 0, 0, time.UTC), "2027-03-27T23:00:00Z"}, // prestaje 28. 3. u 0 h, prije pomaka
	} {
		p := PrestanakNakonDana(tc.dan.Year(), tc.dan.Month(), tc.dan.Day())
		if got := p.Format(time.RFC3339); got != tc.prestanak {
			t.Errorf("zaključno s %s: prestaje %s, očekivano %s", tc.dan.Format("2.1.2006."), got, tc.prestanak)
		}
		if !PrestajeUPonoc(p) {
			t.Errorf("zaključno s %s: prestanak nije ponoć u Zagrebu", tc.dan.Format("2.1.2006."))
		}
		if z := ZadnjiDanVazenja(p); z.Format("2006-01-02") != tc.dan.Format("2006-01-02") {
			t.Errorf("zadnji dan za %s: %s", tc.prestanak, z.Format("2006-01-02"))
		}
	}
}

// Kraj obrane u 10 h nije ponoć; zadnji dan važenja je taj dan
func TestPrestanakUSatu(t *testing.T) {
	kraj := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC) // 10:00 u Zagrebu
	if PrestajeUPonoc(kraj) {
		t.Errorf("10 h nije ponoć")
	}
	if z := ZadnjiDanVazenja(kraj); z.Format("2006-01-02") != "2026-10-20" {
		t.Errorf("zadnji dan: %s", z.Format("2006-01-02"))
	}
	// ponoć po UTC-u (stari zapis) je 2 h u Zagrebu
	if PrestajeUPonoc(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ponoć po UTC-u nije ponoć u Zagrebu")
	}
}
