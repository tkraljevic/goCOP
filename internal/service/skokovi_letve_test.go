package service

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
)

// Odbačeni skokovi letve za upozorenje na stranici i znak u popisu
// (Odluke (25. 10. 2026.)) čitaju se kroz servis.
func TestNedavniSkokoviKrozServis(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	cm := 830
	skok := models.OdbacenSkok(models.Reading{StationID: "d", MeasuredAt: time.Date(2026, 10, 22, 21, 0, 0, 0, models.Zagreb), LevelCm: &cm, Origin: "hdv.voda.hr"})
	if err := o.readings.ZabiljeziOdbacene(ctx, []models.OdbacenaVrijednost{skok}); err != nil {
		t.Fatal(err)
	}
	od := time.Date(2026, 10, 18, 0, 0, 0, 0, models.Zagreb)
	s, err := o.rs.NedavniSkokovi(ctx, "station:d", od)
	if err != nil || len(s) != 1 || s[0].Opis != "skok na 830 cm" {
		t.Errorf("skokovi %+v %v", s, err)
	}
	if b, err := o.rs.BrojNedavnihSkokova(ctx, od); err != nil || b["station:d"] != 1 {
		t.Errorf("broj %v %v", b, err)
	}
}
