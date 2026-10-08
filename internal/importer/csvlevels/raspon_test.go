package csvlevels

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Odluke (8. 10. 2026.), pitanje 21: tablica centra prima isti raspon kao
// ručni upis, po letvi stupca (kota akumulacije ima svoj), i vrijeme; ćelije
// izvan raspona ne uvozi nego broji i bilježi za stranicu letve.
func TestUvozTabliceRasponPoLetvi(t *testing.T) {
	o := novaOkolinaTablice(t)
	ctx := context.Background()
	akumulacija := &models.Station{ID: uuid.New(), Code: "akumulacija-probna", Name: "Akumulacija Probna", KotaNM: true}
	if err := o.deps.Stations.CreateStation(ctx, akumulacija); err != nil {
		t.Fatal(err)
	}
	sutra := time.Now().In(models.Zagreb).AddDate(0, 0, 2).Format("02.01.2006.")
	put := napisi(t, []string{
		"Datum;Primjerovo;Akumulacija Probna",
		"01.09.2026.;120;19089",
		"02.09.2026.;8650cm;300",
		sutra + ";121;19090",
	})
	probni, err := Run(ctx, Options{Path: put, DryRun: true, Origin: "tablica centra", Deps: o.deps})
	if err != nil {
		t.Fatal(err)
	}
	// dan u budućnosti: obje ćelije; 8650 na letvi vodostaja; 300 na akumulaciji
	if probni.IzvanRaspona != 4 || probni.Inserted != 2 {
		t.Fatalf("probni prolaz: izvan raspona %d, novih %d", probni.IzvanRaspona, probni.Inserted)
	}
	if s, _ := o.deps.Readings.SazetakOdbacenih(ctx, "station:"+o.primjerovo.ID.String()); s != nil {
		t.Errorf("probni prolaz ne bilježi ništa: %+v", s)
	}

	rep, err := Run(ctx, Options{Path: put, Origin: "tablica centra", Deps: o.deps})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Inserted != 2 || rep.IzvanRaspona != 4 {
		t.Fatalf("uvoz: novih %d, izvan raspona %d", rep.Inserted, rep.IzvanRaspona)
	}
	if got := o.ocitanja(t, akumulacija.ID.String()); len(got) != 1 || *got[0].LevelCm != 19089 {
		t.Errorf("akumulacija: %+v", got)
	}
	s, err := o.deps.Readings.SazetakOdbacenih(ctx, "station:"+o.primjerovo.ID.String())
	if err != nil || s == nil || s.Broj != 2 || s.Zadnja.Izvor != "tablica centra" || s.Zadnja.Polje != models.PoljeVrijeme {
		t.Errorf("sažetak Primjerova: %+v, %v", s, err)
	}
	s, err = o.deps.Readings.SazetakOdbacenih(ctx, "station:"+akumulacija.ID.String())
	if err != nil || s == nil || s.Broj != 2 {
		t.Errorf("sažetak akumulacije: %+v, %v", s, err)
	}
	if !strings.Contains(rep.Summary(), "izvan raspona: 4 (vodostaj −500..3000 cm, kota akumulacije 5000..40000 cm n. m., vrijeme do 1 h unaprijed)") {
		t.Errorf("sažetak: %s", rep.Summary())
	}

	// ponovni uvoz iste tablice ne udvostručuje odbačene
	if _, err := Run(ctx, Options{Path: put, Origin: "tablica centra", Deps: o.deps}); err != nil {
		t.Fatal(err)
	}
	if s, _ := o.deps.Readings.SazetakOdbacenih(ctx, "station:"+akumulacija.ID.String()); s == nil || s.Broj != 2 {
		t.Errorf("ponovni uvoz: %+v", s)
	}
}

// Kad se odbačene ćelije ne mogu zabilježiti, uvoz staje prije upisa
func TestUvozTabliceStajeKadSeOdbaceneNeMoguZabiljeziti(t *testing.T) {
	o := novaOkolinaTablice(t)
	if _, err := o.baza.Exec(`CREATE TRIGGER odbij_biljezenje BEFORE INSERT ON odbacena_ocitanja
		BEGIN SELECT RAISE(ABORT, 'bilježenje odbijeno'); END`); err != nil {
		t.Fatal(err)
	}
	put := napisi(t, []string{"Datum;Primjerovo", "01.09.2026.;120", "02.09.2026.;8650"})
	rep, err := Run(context.Background(), Options{Path: put, Deps: o.deps})
	if err == nil || rep.Inserted != 0 {
		t.Fatalf("uvoz s neuspjelim bilježenjem: %+v, %v", rep, err)
	}
	if got := o.ocitanja(t, o.primjerovo.ID.String()); len(got) != 0 {
		t.Errorf("upisano %d očitanja", len(got))
	}
}

func TestProcitajCelijuStupca(t *testing.T) {
	sada := time.Date(2026, 10, 8, 12, 0, 0, 0, models.Zagreb)
	jutro := time.Date(2026, 10, 8, 7, 0, 0, 0, models.Zagreb)
	vodostaj := &Column{Key: "station:a"}
	kota := &Column{Key: "station:b", Kota: true}
	for _, s := range []struct {
		col    *Column
		celija string
		at     time.Time
		cm     int
		status levelStatus
		opis   string
	}{
		{vodostaj, "120", jutro, 120, levelOK, ""},
		{vodostaj, "", jutro, 0, levelBlank, ""},
		{vodostaj, "nije broj", jutro, 0, levelBad, ""},
		{vodostaj, "3001", jutro, 0, levelIzvanRaspona, "3001 cm"},
		{vodostaj, "8650 cm", jutro, 0, levelIzvanRaspona, "8650 cm"},
		{vodostaj, "99999999999999999999", jutro, 0, levelIzvanRaspona, "99999999999999999999 cm"},
		{kota, "19089", jutro, 19089, levelOK, ""},
		{kota, "300", jutro, 0, levelIzvanRaspona, "300 cm n. m."},
		{vodostaj, "120", sada.Add(2 * time.Hour), 0, levelIzvanRaspona, "vrijeme 8. 10. 2026. 14:00"},
	} {
		cm, status, odb := s.col.procitaj(s.celija, s.at, sada)
		if cm != s.cm || status != s.status {
			t.Errorf("%q: %d, %v; očekivano %d, %v", s.celija, cm, status, s.cm, s.status)
		}
		if (s.opis == "") != (odb == nil) || (odb != nil && (odb.Opis != s.opis || odb.Letva != s.col.Key || !odb.Kad.Equal(s.at))) {
			t.Errorf("%q: zapis %+v, očekivano %q", s.celija, odb, s.opis)
		}
	}
}
