package web

import (
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

func TestVrijemeIzPoljaBezSekundi(t *testing.T) {
	want := time.Date(2026, 11, 24, 12, 0, 0, 0, models.Zagreb)
	for _, v := range []string{"2026-11-24T12:00", " 2026-11-24T12:00:23 ", "2026-11-24T12:00:23.456"} {
		got, err := vrijemeIzPolja(v)
		if err != nil || !got.Equal(want) {
			t.Errorf("vrijemeIzPolja(%q) = %v, %v; želim %v", v, got, err, want)
		}
	}
	for _, v := range []string{"", "24.11.2026. 12:00", "2026-11-24T12"} {
		if _, err := vrijemeIzPolja(v); err == nil {
			t.Errorf("vrijemeIzPolja(%q) bez greške", v)
		}
	}
	if got := minutaPolja("2026-11-24T12:00:59"); got != "2026-11-24T12:00" {
		t.Errorf("minutaPolja = %q", got)
	}
}

// Safari šalje skrivene sekunde; ručni vodostaj akta ih mora prihvatiti
func TestRucnoOcitanjeSaSekundama(t *testing.T) {
	var z service.ZahtjevAkta
	if err := rucnoOcitanje(&z, "36", "2026-11-24T12:00:23"); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 24, 12, 0, 0, 0, models.Zagreb)
	if z.RucniCm == nil || *z.RucniCm != 36 || !z.RucnoKad.Equal(want) {
		t.Fatalf("ručno očitanje = %v u %v", z.RucniCm, z.RucnoKad)
	}
}
