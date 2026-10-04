package models

import (
	"testing"
	"time"
)

// Dežurstvo do 24 sata (i jedan dnevnu i noćnu smjenu) nije sumnjivo; dulje
// bez stanke jest, pa ga uprava mora pogledati prije potvrde
func TestDezurstvoBezStanke(t *testing.T) {
	od := time.Date(2026, 11, 2, 7, 0, 0, 0, Zagreb)
	for sati, bez := range map[int]bool{12: false, 24: false, 25: true, 72: true} {
		d := Dezurstvo{Od: od, Do: od.Add(time.Duration(sati) * time.Hour)}
		if d.BezStanke() != bez {
			t.Errorf("%d h: BezStanke = %v", sati, !bez)
		}
	}
}
