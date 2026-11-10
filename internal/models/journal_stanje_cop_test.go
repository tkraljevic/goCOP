package models

import (
	"testing"
	"time"
)

// Dani dnevnika COP-a broje se kalendarski, s oba kraja: 12.–26. 10. je 15
// dana, a dnevnik koji traje broji do danas. Sat ne mijenja dan.
func TestDanaObrane(t *testing.T) {
	dan := func(m, d, h int) *time.Time { t := time.Date(2026, time.Month(m), d, h, 0, 0, 0, Zagreb); return &t }
	danas := *dan(10, 26, 0)
	for _, tc := range []struct {
		ime  string
		j    Journal
		dana int
	}{
		{"traje do danas", Journal{StartedAt: dan(10, 12, 23)}, 15},
		{"zaključen", Journal{StartedAt: dan(3, 14, 8), EndedAt: dan(5, 3, 0)}, 51},
		{"isti dan", Journal{StartedAt: dan(4, 9, 1), EndedAt: dan(4, 9, 23)}, 1},
		{"bez početka", Journal{EndedAt: dan(4, 9, 0)}, 0},
		{"kraj prije početka", Journal{StartedAt: dan(4, 9, 0), EndedAt: dan(4, 8, 0)}, 0},
	} {
		if got := tc.j.DanaObrane(danas); got != tc.dana {
			t.Errorf("%s: %d dana, treba %d", tc.ime, got, tc.dana)
		}
	}
}

// Stanje na popisu: prijepis ima prednost, zatim ovjera, pa zaključen; samo
// otvoren dnevnik drži sektor u aktivnoj obrani.
func TestStanjeCOP(t *testing.T) {
	sad := time.Now()
	for _, tc := range []struct {
		j       Journal
		stanje  string
		aktivna bool
	}{
		{Journal{}, "otvoren", true},
		{Journal{EndedAt: &sad}, "zaključen, čeka ovjeru", false},
		{Journal{EndedAt: &sad, ZakljucenoAt: &sad}, "ovjeren", false},
		{Journal{Reconstruction: true, ZakljucenoAt: &sad}, "prijepis iz uveza", false},
		{Journal{Reconstruction: true}, "prijepis iz uveza", false},
	} {
		if got := tc.j.StanjeCOP(); got != tc.stanje {
			t.Errorf("stanje %q, treba %q", got, tc.stanje)
		}
		if got := tc.j.NosiAktivnuObranu(); got != tc.aktivna {
			t.Errorf("%s: aktivna obrana %v, treba %v", tc.stanje, got, tc.aktivna)
		}
	}
}
