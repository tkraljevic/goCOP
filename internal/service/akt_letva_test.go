package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Obrazac akta nudi letvu samo kad bi je priprema prihvatila: osoba piše po
// dosegu akta ili ga smije ovjeriti, a letva ima dionica u registru. Letva
// drugog sektora ne nudi se samo zato što osoba ima neki sektor ili područje.
func TestSmijeAktZaLetvu(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('R', 'Sektor R', 'VGO Drugovo', 'COP Drugovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (2, 'R', 'Mali sliv Drugovo', 'VGI Drugovo')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('R.2.1', 2, 'R', 'potok Drugi', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := o.baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	stanice := repository.NewStationRepository(o.baza, ledger.New(o.baza, "test"))
	letva := func(naziv string, dionice ...string) *models.Station {
		st := &models.Station{ID: uuid.New(), Code: naziv, Name: naziv, Watercourse: "Primjerica"}
		if err := stanice.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		for _, c := range dionice {
			if _, err := o.baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
				uuid.NewString(), c, st.ID.String(), time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		st, err := stanice.GetStationByID(ctx, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	drugovo, bez := letva("Drugovo", "R.2.1"), letva("Bezdionicovo")
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *o.rukovod}

	for ime, z := range map[string]struct {
		perms *models.UserPermissions
		st    *models.Station
		zeli  bool
	}{
		"rukovoditelj područja, svoja letva": {o.ovlasti, o.letva, true},
		"vodočuvar dionice letve":            {o.vodOvl, o.letva, true},
		"rukovoditelj, letva drugog sektora": {o.ovlasti, drugovo, false},
		"vodočuvar, letva drugog sektora":    {o.vodOvl, drugovo, false},
		"uprava, letva drugog sektora":       {uprava, drugovo, true},
		"uprava, letva bez dionica":          {uprava, bez, false},
		"bez ovlasti":                        {nil, o.letva, false},
		"bez letve":                          {o.ovlasti, nil, false},
	} {
		if got := o.akti.SmijeAktZaLetvu(ctx, z.perms, z.st, models.PhasePrep); got != z.zeli {
			t.Errorf("%s: %v, očekivano %v", ime, got, z.zeli)
		}
	}
}
