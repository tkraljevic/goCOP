package service_test

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
)

// „Prekini” se nudi samo uz ovjerenu uspostavu koja još nije prekinuta i
// samo onome tko u sektoru sastavlja nacrte; nacrt, prekid, prekinuta i
// poništena uspostava ga nemaju
func TestPrekidljiviAkti(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sat := time.Now().Add(-6 * time.Hour).Truncate(time.Minute)

	u1, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat)
	p1, _ := o.ovjeri(t, models.AktPrekid, models.PhasePrep, sat.Add(time.Hour))
	u2, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat.Add(2*time.Hour))
	u3, _ := o.ovjeri(t, models.AktUspostava, models.PhaseRegular, sat.Add(3*time.Hour))
	if _, _, err := o.akti.Storniraj(ctx, o.ovlasti, o.rukovod, u3.ID, "pogreška"); err != nil {
		t.Fatal(err)
	}
	u3, err := o.akti.Get(ctx, u3.ID)
	if err != nil {
		t.Fatal(err)
	}
	nacrt := *u2
	nacrt.ID, nacrt.Status = "nacrt", models.AktNacrt
	svi := []models.Akt{*u1, *p1, *u2, *u3, nacrt}

	got := o.akti.Prekidljivi(ctx, o.dezOvl, svi)
	if len(got) != 1 || !got[u2.ID] {
		t.Errorf("dežurni COP-a: %v, želim samo %s", got, u2.ID)
	}
	if got := o.akti.Prekidljivi(ctx, o.vodOvl, svi); len(got) != 0 {
		t.Errorf("vodočuvar ne sastavlja nacrte, a nudi mu se prekid: %v", got)
	}
	if got := o.akti.Prekidljivi(ctx, nil, svi); len(got) != 0 {
		t.Errorf("bez prava: %v", got)
	}
}
