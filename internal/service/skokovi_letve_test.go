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

// Stanje ispravka telemetrije čita se kroz servis; kad baza ne odgovara,
// stranica dobije prazno stanje umjesto greške.
func TestIspravakTelemetrijeKrozServis(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	sada := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if _, err := o.baza.Exec(`INSERT INTO stations (id, code, name, javni_url, javni_uvoz, created_at, updated_at)
		VALUES ('jezero-id', 'jezero', 'Jezero', 'https://vodostaji.voda.hr/Home/PregledVodostajaPostaje?postajaID=1', 1, ?, ?)`, sada, sada); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, sirovo_cm, source, created_at, updated_at)
		VALUES ('o1', 'jezero-id', ?, 312, 355, ?, ?, ?)`, sada.Add(-time.Hour), models.ReadingSourceImport, sada, sada); err != nil {
		t.Fatal(err)
	}
	if i := o.rs.IspravciTelemetrije(ctx); len(i) != 1 || i["jezero-id"] != -43 {
		t.Errorf("ispravci: %v", i)
	}
	if s := o.rs.StanjeIspravka(ctx, "jezero-id", sada); s.PomakCm != -43 || !s.TrebaTeren() {
		t.Errorf("stanje: %+v", s)
	}
	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	if i := o.rs.IspravciTelemetrije(ctx); len(i) != 0 {
		t.Errorf("zatvorena baza: ispravci %v", i)
	}
	if s := o.rs.StanjeIspravka(ctx, "jezero-id", sada); s != (models.StanjeIspravka{}) {
		t.Errorf("zatvorena baza: stanje %+v", s)
	}
}
