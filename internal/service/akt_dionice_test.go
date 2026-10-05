package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Zadana dionica akta mora biti dionica letve i postojati u registru: inače
// je to greška unosa s njezinom šifrom, a ne tiho otpala dionica. Akt bez
// ijedne dionice iz registra ne priprema se, ni upravi organizacije.
func TestPripremaAktaZadaneDionice(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	rec := ledger.New(o.baza, "test")
	stanice := repository.NewStationRepository(o.baza, rec)

	// veza letve s dionicom koje u registru nema (npr. dionica obrisana
	// ili još nije stigla razmjenom)
	veza := func(st *models.Station, sifra string) {
		t.Helper()
		c, err := o.baza.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err := c.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			t.Fatal(err)
		}
		if _, err := c.ExecContext(ctx, `INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), sifra, st.ID.String(), time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := c.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
			t.Fatal(err)
		}
	}
	veza(o.letva, "P.1.8")
	zahtjev := func(st *models.Station, dionice ...string) service.ZahtjevAkta {
		return service.ZahtjevAkta{StationID: st.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
			Vrijedi: time.Now().Truncate(time.Minute), Dionice: dionice}
	}

	for ime, z := range map[string]struct {
		dionice []string
		sifra   string
	}{
		"nije dionica letve":          {[]string{"P.1.1", "X.9.9"}, "X.9.9"},
		"nema je u registru":          {[]string{"P.1.8"}, "P.1.8"},
		"uz ispravnu nema u registru": {[]string{"P.1.2", "P.1.8"}, "P.1.8"},
	} {
		if _, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, zahtjev(o.letva, z.dionice...)); err == nil || !strings.Contains(err.Error(), z.sifra) {
			t.Errorf("%s: priprema mora javiti dionicu %s: %v", ime, z.sifra, err)
		}
	}

	// bez zadanih: dionice letve koje su u registru
	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, zahtjev(o.letva))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Dionice) != 2 || a.Dionice[0].Code != "P.1.1" || a.Dionice[1].Code != "P.1.2" || a.Sektor != "P" || a.AreaID != 1 {
		t.Errorf("dionice letve: %+v, sektor %q, područje %d", a.Dionice, a.Sektor, a.AreaID)
	}
	// uži izbor
	if a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, zahtjev(o.letva, "P.1.2")); err != nil || len(a.Dionice) != 1 || a.Dionice[0].Code != "P.1.2" {
		t.Errorf("uži izbor: %+v (%v)", a, err)
	}

	// letva samo s dionicama kojih nema u registru: akt se ne priprema
	druga := &models.Station{ID: uuid.New(), Code: "druga-primjerica", Name: "Druga Primjerica", Watercourse: "Primjerica"}
	if err := stanice.CreateStation(ctx, druga); err != nil {
		t.Fatal(err)
	}
	veza(druga, "P.1.7")
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *o.rukovod}
	if a, err := o.akti.Pripremi(ctx, uprava, o.rukovod, zahtjev(druga)); err == nil || !strings.Contains(err.Error(), "Druga Primjerica") {
		t.Errorf("akt bez dionica iz registra: %+v (%v)", a, err)
	}
}
