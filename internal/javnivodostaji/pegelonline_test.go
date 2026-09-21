package javnivodostaji

import (
	"testing"
	"time"
)

func TestCitajPegelOnlineUzimaPuneSate(t *testing.T) {
	b := []byte(`[
 {"timestamp":"2026-09-21T10:45:00+02:00","value":241.0},
 {"timestamp":"2026-09-21T11:00:00+02:00","value":242.0},
 {"timestamp":"2026-09-21T11:15:00+02:00","value":243.0},
 {"timestamp":"2026-09-21T12:00:00+02:00","value":244.0}
]`)
	r, err := CitajPegelOnline(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].LevelCm == nil || *r[0].LevelCm != 242 || r[1].LevelCm == nil || *r[1].LevelCm != 244 {
		t.Fatalf("redci: %+v", r)
	}
	if !r[0].Kad.Equal(time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("vrijeme: %v", r[0].Kad)
	}
}

func TestAdresaPegelOnline(t *testing.T) {
	uuid := "c389c9e2-a5d8-4104-a4cf-510ade44f143"
	if got := PostajaPegelOnlineIzAdrese(AdresaPegelOnline(uuid)); got != uuid {
		t.Fatalf("UUID %q", got)
	}
	if PostajaPegelOnlineIzAdrese("https://example.test/stations/"+uuid+"/W/measurements.json") != "" {
		t.Error("tuđa domena ne smije biti PEGELONLINE")
	}
}
