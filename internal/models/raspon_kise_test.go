package models

import (
	"math"
	"testing"
	"time"
)

// Kiša s kišomjera uz letvu: od 0 do 300 mm; izvan toga ručni upis dobije
// poruku, a uvoz makne samo kišu i zadrži vodostaj.
func TestRasponKise(t *testing.T) {
	for _, s := range []struct {
		v  float64
		ok bool
	}{{0, true}, {12.4, true}, {300, true}, {-0.1, false}, {300.1, false}, {math.NaN(), false}} {
		izvan := VrijednostiIzvanRaspona(Reading{RainMm: broj(s.v)}, false)
		if (len(izvan) == 0) != s.ok {
			t.Errorf("kiša %v: izvan %v", s.v, izvan)
		}
		if !s.ok && (izvan[0].Polje != PoljeKisa || izvan[0].Error() != "oborina mora biti između 0 i 300 mm") {
			t.Errorf("kiša %v: %+v, %q", s.v, izvan[0], izvan[0].Error())
		}
	}

	rd := Reading{LevelCm: cmR(140), RainMm: broj(450), MeasuredAt: time.Now().Add(-time.Hour)}
	izvan, upisati := OcistiOcitanje(&rd, false, time.Now())
	if len(izvan) != 1 || !upisati || rd.RainMm != nil || rd.LevelCm == nil {
		t.Errorf("uvoz s krivom kišom: izvan %v, upisati %v, očitanje %+v", izvan, upisati, rd)
	}
	if !(Reading{RainMm: broj(0)}).HasAnyValue() {
		t.Error("očitanje samo s kišom od 0 mm nema vrijednosti")
	}
}
