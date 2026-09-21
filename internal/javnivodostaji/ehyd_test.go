package javnivodostaji

import (
	"testing"
	"time"
)

func TestCitajEHYDProtok(t *testing.T) {
	b := []byte(`{"features":[{"properties":{"wert":14.0,"einheit":"m³/s","zeitpunkt":"2026-09-21T10:45:00+02:00","parameter":"Q"}}]}`)
	r, err := CitajEHYD(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 || r[0].FlowM3s == nil || *r[0].FlowM3s != 14 || r[0].LevelCm != nil {
		t.Fatalf("redak: %+v", r)
	}
	if !r[0].Kad.Equal(time.Date(2026, 9, 21, 8, 45, 0, 0, time.UTC)) {
		t.Errorf("vrijeme: %v", r[0].Kad)
	}
}

func TestEHYDPrepoznajeSluzbeniOGC(t *testing.T) {
	a := "https://gis.lfrz.gv.at/api/geodata/i000501/ogc/features/v1/collections/i000501:pegel_aktuell/items/pegel_aktuell.213173?f=json"
	if !(EHYD{}).Prepoznaje(a) {
		t.Error("službeni OGC zapis nije prepoznat")
	}
}
