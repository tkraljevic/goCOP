package prognoza

import (
	"context"
	"testing"
	"time"
)

// Zaključava kako prognoza bira krivulju protoka kad se razdoblja valjanosti
// preklapaju: vrijedi krivulja s najkasnijim početkom koji nije poslije
// trenutka. Arhiva Donjeg Miholjca 13. 10. 2026. imala je krivulju od
// 1. 1. 2016. bez kraja, a uz nju krivulje od 2017., 2019. i 2025.; stara bez
// kraja preklapa sve tri. Krivulje se upisuju izmiješanim redom, da izbor ne
// ovisi o redu upisa. Po odsječku Q = 100·H + oznaka (H u metrima) vidi se
// koja je krivulja izabrana.
func TestKrivuljaZaKadSeRazdobljaPreklapaju(t *testing.T) {
	arhiva := arhivaKrivulja(t)
	for _, k := range []struct {
		id     int
		od, do string
		oznaka float64
	}{
		{1, "2025-01-01", "", 25},
		{2, "2016-01-01", "", 16},
		{3, "2019-01-01", "2024-12-31", 19},
		{4, "2015-01-01", "2015-12-31", 15},
		{5, "2017-01-01", "2018-12-31", 17},
	} {
		if _, err := arhiva.Exec(`INSERT INTO hq_krivulje VALUES (?, 'donji-miholjac', ?, ?)`, k.id, k.od, k.do); err != nil {
			t.Fatal(err)
		}
		if _, err := arhiva.Exec(`INSERT INTO hq_odsjecci VALUES (?, -150, 600, 'polinom', 0, 100, ?, 0)`, k.id, k.oznaka); err != nil {
			t.Fatal(err)
		}
	}
	krivulje, err := KrivuljeIzArhive(context.Background(), arhiva, "donji-miholjac")
	if err != nil || len(krivulje) != 5 {
		t.Fatalf("krivulje: %d, %v", len(krivulje), err)
	}
	for _, s := range []struct {
		kad    time.Time
		oznaka float64
	}{
		{time.Date(2015, 6, 1, 12, 0, 0, 0, time.UTC), 15},
		{time.Date(2016, 6, 1, 12, 0, 0, 0, time.UTC), 16},
		{time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC), 17},
		{time.Date(2018, 12, 31, 23, 0, 0, 0, time.UTC), 17},
		{time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), 19},
		{time.Date(2022, 6, 1, 12, 0, 0, 0, time.UTC), 19},
		{time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), 25},
		{time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), 25},
	} {
		k := KrivuljaZa(krivulje, s.kad)
		if k == nil {
			t.Errorf("%s: nema krivulje", s.kad.Format(time.DateTime))
			continue
		}
		if q, ok := k.Protok(0); !ok || q != s.oznaka {
			t.Errorf("%s: izabrana krivulja od %s (Q(0) = %g), očekivana s oznakom %g", s.kad.Format(time.DateTime), k.VrijediOd, q, s.oznaka)
		}
	}
	if k := KrivuljaZa(krivulje, time.Date(2014, 6, 1, 12, 0, 0, 0, time.UTC)); k != nil {
		t.Errorf("prije prve krivulje izabrana je krivulja od %s", k.VrijediOd)
	}
}
