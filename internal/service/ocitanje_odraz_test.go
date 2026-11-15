package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Vodostaj upisan na crpnoj stanici vidi se na njezinoj letvi, ali se
// ispravlja i briše samo na objektu, i administratoru (Odluke (15. 11. 2026.)).
func TestOdrazSeIspravljaNaObjektu(t *testing.T) {
	o := novaOkolinaCitanja(t)
	ctx := context.Background()
	rd := &models.Reading{StructureID: o.csProbni.ID.String(), MeasuredAt: time.Now().Add(-time.Hour), LevelCm: cmP(210),
		StructureState: models.StructureStateIdle}
	if err := o.rs.Create(ctx, o.adminOvl, rd); err != nil {
		t.Fatal(err)
	}
	naLetvi, err := o.readings.List(ctx, repository.ReadingFilter{StationID: o.primjerovo.ID.String()})
	if err != nil || len(naLetvi) != 1 || !naLetvi[0].JeOdrazObjekta() || *naLetvi[0].LevelCm != 210 {
		t.Fatalf("na letvi: %+v %v", naLetvi, err)
	}
	odraz := naLetvi[0]
	if o.rs.CanEdit(ctx, o.adminOvl, &odraz) || o.rs.CanDelete(ctx, o.adminOvl, &odraz) {
		t.Error("odraz se smije dirati na letvi")
	}
	odraz.LevelCm = cmP(1)
	if err := o.rs.Update(ctx, o.adminOvl, &odraz); !errors.Is(err, ErrOdrazObjekta) {
		t.Errorf("izmjena odraza: %v", err)
	}
	if _, err := o.rs.Delete(ctx, o.adminOvl, odraz.ID); !errors.Is(err, ErrOdrazObjekta) {
		t.Errorf("brisanje odraza: %v", err)
	}
	if _, err := o.rs.Delete(ctx, o.adminOvl, rd.ID); err != nil {
		t.Fatal(err)
	}
	if naLetvi, err = o.readings.List(ctx, repository.ReadingFilter{StationID: o.primjerovo.ID.String()}); err != nil || len(naLetvi) != 0 {
		t.Errorf("nakon brisanja na objektu letva ima %d očitanja (%v)", len(naLetvi), err)
	}
}
