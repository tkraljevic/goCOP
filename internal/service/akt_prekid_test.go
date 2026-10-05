package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Prekid stavlja izvan snage samo ovjeren, neponišten akt o uspostavi iste
// letve i stupnja: bez zadanog akta najnoviji koji još nije prekinut, a
// zadani se odbija kad je poništen, druge letve ili drugog stupnja
func TestPrekidVezanSamoNaNeponistenuNeprekinutuUspostavu(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sat := time.Now().Add(-6 * time.Hour).Truncate(time.Minute)
	prekid := func(stupanj models.DefensePhase, prekida string) (*models.Akt, error) {
		return o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: models.AktPrekid,
			Stupanj: stupanj, Vrijedi: time.Now().Truncate(time.Minute), PrekidaAktID: prekida})
	}

	u1, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat)
	p1, _ := o.ovjeri(t, models.AktPrekid, models.PhasePrep, sat.Add(time.Hour))
	if p1.PrekidaAktID != u1.ID {
		t.Fatalf("prekid nije vezan na uspostavu: %q", p1.PrekidaAktID)
	}
	u2, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat.Add(2*time.Hour))
	if _, _, err := o.akti.Storniraj(ctx, o.ovlasti, o.rukovod, u2.ID, "pogreška"); err != nil {
		t.Fatal(err)
	}

	// prva uspostava je prekinuta, druga poništena: nema se što staviti izvan snage
	a, err := prekid(models.PhasePrep, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.PrekidaAktID != "" || a.IzvanSnage != "" {
		t.Errorf("prekid vezan na prekinut ili poništen akt: %q %q", a.PrekidaAktID, a.IzvanSnage)
	}
	if za, err := o.akti.AktiZaPrekid(ctx, o.letva.ID.String(), ""); err != nil || len(za) != 0 {
		t.Errorf("obrazac nudi prekinut ili poništen akt: %+v (%v)", za, err)
	}

	// nova uspostava: na nju se prekid veže
	u3, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat.Add(3*time.Hour))
	if a, err := prekid(models.PhasePrep, ""); err != nil || a.PrekidaAktID != u3.ID || !strings.Contains(a.IzvanSnage, u3.Oznaka()) {
		t.Errorf("prekid nije vezan na zadnju neprekinutu uspostavu: %+v (%v)", a, err)
	}
	if za, err := o.akti.AktiZaPrekid(ctx, o.letva.ID.String(), ""); err != nil || len(za) != 1 || za[0].ID != u3.ID {
		t.Errorf("obrazac: %+v (%v)", za, err)
	}
	if a, err := prekid(models.PhasePrep, u3.ID); err != nil || a.PrekidaAktID != u3.ID {
		t.Errorf("zadana uspostava iste letve i stupnja: %+v (%v)", a, err)
	}

	// akt druge letve
	rec := ledger.New(o.baza, "test")
	druga := &models.Akt{ID: "druga-letva", Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Status: models.AktOvjeren,
		Broj: 9, Godina: 2026, StationID: "druga-letva", OvjerioID: "pperic", OvjeraKod: "KOD-druga", Vrijedi: sat, Dionice: []models.AktDionica{{Code: "P.1.2"}}}
	if err := repository.NewAktiRepository(o.baza, rec).SaveAkt(ctx, druga); err != nil {
		t.Fatal(err)
	}
	for ime, z := range map[string]struct {
		stupanj models.DefensePhase
		id      string
	}{
		"poništen":      {models.PhasePrep, u2.ID},
		"drugi stupanj": {models.PhaseRegular, u3.ID},
		"druga letva":   {models.PhasePrep, druga.ID},
		"prekid":        {models.PhasePrep, p1.ID},
		"ne postoji":    {models.PhasePrep, "nema-ga"},
	} {
		if _, err := prekid(z.stupanj, z.id); err == nil || !strings.Contains(err.Error(), "odabrani akt") {
			t.Errorf("%s: zadani akt mora biti odbijen: %v", ime, err)
		}
	}
}
