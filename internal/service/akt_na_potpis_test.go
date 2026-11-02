package service_test

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Na potpis su nacrti sektora koje osoba smije ovjeriti: rukovoditelj
// područja vidi nacrt pripremnog stanja, vodočuvar ne, a ovjeren akt više
// ne čeka
func TestAktiNaPotpis(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.dezOvl, o.dezurni, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.dezOvl, a); err != nil {
		t.Fatal(err)
	}
	if l, err := o.akti.NaPotpis(ctx, o.ovlasti, "P"); err != nil || len(l) != 1 || l[0].ID != a.ID {
		t.Errorf("rukovoditelj: %+v %v", l, err)
	}
	if l, err := o.akti.NaPotpis(ctx, o.vodOvl, "P"); err != nil || len(l) != 0 {
		t.Errorf("vodočuvar: %+v %v", l, err)
	}
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID, potvrdaPrimatelja); err != nil {
		t.Fatal(err)
	}
	if l, err := o.akti.NaPotpis(ctx, o.ovlasti, "P"); err != nil || len(l) != 0 {
		t.Errorf("ovjeren akt čeka potpis: %+v %v", l, err)
	}
	o.baza.Close()
	if _, err := o.akti.NaPotpis(ctx, o.ovlasti, "P"); err == nil {
		t.Error("bez baze bez greške")
	}
}
