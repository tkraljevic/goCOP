package arhiva

import (
	"testing"
	"time"
)

// Hrvatski izvori daju lokalni sat, mađarski pravi UTC. Kad se to ne razdvoji,
// isti trenutak s dvije strane granice pada na različit sat — a razlika se
// mijenja s ljetnim vremenom, pa izgleda kao hidrologija.
func TestZonaIzvora(t *testing.T) {
	for _, izvor := range []string{"his2000", "his2000-cs", "letva-hv", "letva-dhmz", "cop"} {
		if z := zonaIzvora(izvor); z != Zagreb {
			t.Errorf("%s: zona %v, očekivano Europe/Zagreb", izvor, z)
		}
	}
	for _, izvor := range []string{"vituki", "danubehis", "preracun-mohacs", "preracun-hq"} {
		if z := zonaIzvora(izvor); z != time.UTC {
			t.Errorf("%s: zona %v, očekivano UTC", izvor, z)
		}
	}

	// 7.9.2026. je ljetno vrijeme: 07:00 lokalno je 05:00 UTC
	ljeti := time.Date(2026, 9, 7, 7, 0, 0, 0, Zagreb).UTC()
	if ljeti.Hour() != 5 {
		t.Errorf("ljeti 07:00 lokalno daje %d h UTC, očekivano 5", ljeti.Hour())
	}
	// 7.1.2026. je zimsko: 07:00 lokalno je 06:00 UTC
	zimi := time.Date(2026, 1, 7, 7, 0, 0, 0, Zagreb).UTC()
	if zimi.Hour() != 6 {
		t.Errorf("zimi 07:00 lokalno daje %d h UTC, očekivano 6", zimi.Hour())
	}
	// upravo ta razlika od jednog sata pokazivala se kao 6 h zimi i 7 h ljeti
	if zimi.Hour()-ljeti.Hour() != 1 {
		t.Error("razlika zima/ljeto nije jedan sat")
	}
}
