package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Staro i novo na istim aktima: ovjera danas mijenja epizodu (staro), a
// stanje se računa iz ovjerenih akata (novo). Dok su akti postupni prema
// gore, slažu se; prekid višeg stadija epizoda ne vidi, a stanje iz akata
// vrati obranu na niži stadij; akt unaprijed epizoda ne otvori, a stanje iz
// akata ga najavi i u svoje vrijeme uključi.
func TestStanjeObraneIzAkataPremaEpizodi(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sat := time.Now().Add(-time.Hour).Truncate(time.Minute)
	izAkata := func(d string, kad time.Time) models.StanjeObrane {
		t.Helper()
		s, _, err := o.akti.StanjeObrane(ctx, "P", d, kad)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat)
	o.ovjeri(t, models.AktUspostava, models.PhaseRegular, sat.Add(10*time.Minute))
	for _, d := range []string{"P.1.1", "P.1.2"} {
		if staro, novo := o.stanje(t)[d], izAkata(d, time.Now()); staro != models.PhaseRegular || novo.Najvisi() != models.PhaseRegular || len(novo.Pozadina()) != 1 {
			t.Errorf("%s nakon postupnog podizanja: epizoda %s, akti %+v", d, staro, novo)
		}
	}

	// prekid redovne: epizoda ostaje na redovnoj, akti vrate pripremno
	o.ovjeri(t, models.AktPrekid, models.PhaseRegular, sat.Add(30*time.Minute))
	if staro, novo := o.stanje(t)["P.1.1"], izAkata("P.1.1", time.Now()); staro != models.PhaseRegular || novo.Najvisi() != models.PhasePrep {
		t.Errorf("nakon prekida redovne: epizoda %s, akti %s", staro, novo.Najvisi())
	}
	o.ovjeri(t, models.AktPrekid, models.PhasePrep, sat.Add(40*time.Minute))
	if staro, novo := o.stanje(t)["P.1.1"], izAkata("P.1.1", time.Now()); staro != "" || novo.Traje() {
		t.Errorf("nakon prekida pripremnog: epizoda %q, akti %+v", staro, novo)
	}

	// akt za dva sata: epizoda ga ne otvori nikad, akti ga najave i u svoje
	// vrijeme uključe
	za2h := time.Now().Add(2 * time.Hour).Truncate(time.Minute)
	a, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, za2h)
	s, najavljeni, err := o.akti.StanjeObrane(ctx, "P", "P.1.1", time.Now())
	if err != nil || s.Traje() || len(najavljeni) != 1 || najavljeni[0].ID != a.ID {
		t.Errorf("akt za dva sata sada: %+v, najavljeni %d, %v", s, len(najavljeni), err)
	}
	if novo := izAkata("P.1.1", za2h); novo.Najvisi() != models.PhasePrep || novo.Vrh().AktID != a.ID {
		t.Errorf("akt za dva sata u svoje vrijeme: %+v", novo)
	}
	if staro := o.stanje(t)["P.1.1"]; staro != "" {
		t.Errorf("epizoda je otvorena unaprijed: %s", staro)
	}

	// kad se akti ne daju pročitati, javlja se greška
	if _, err := o.baza.Exec(`ALTER TABLE akti RENAME TO nema_akata`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.akti.StanjeObrane(ctx, "P", "P.1.1", time.Now()); err == nil || !strings.Contains(err.Error(), "akti") {
		t.Errorf("bez tablice akata: %v", err)
	}
}

// Stanja sektora: sve dionice s ovjerenim aktima, i greška kad se akti ne
// daju pročitati
func TestStanjaSektoraIzAkata(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sat := time.Now().Add(-time.Hour).Truncate(time.Minute)
	o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat)
	o.ovjeri(t, models.AktUspostava, models.PhaseRegular, sat.Add(10*time.Minute))
	stanja, err := o.akti.StanjaSektora(ctx, "P", time.Now())
	if err != nil || len(stanja) != 2 || stanja["P.1.1"].Najvisi() != models.PhaseRegular || stanja["P.1.2"].Najvisi() != models.PhaseRegular {
		t.Fatalf("stanja sektora: %+v %v", stanja, err)
	}
	if _, err := o.baza.Exec(`ALTER TABLE akti RENAME TO nema_akata`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.akti.StanjaSektora(ctx, "P", time.Now()); err == nil {
		t.Error("bez tablice akata")
	}
}

// Ovjera skenom potpisanog akta ide istim putem kao izravna: isti slijed
// stadija i ista aktivna obrana u sektoru; nemoguć akt odbija se prije nego
// što se sken spremi
func TestOvjeraSkenomIstiPreduvjeti(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sat := time.Now().Add(-time.Hour).Truncate(time.Minute)
	sken := []byte("%PDF-1.4 potpisani akt")
	nacrt := func(radnja string, stupanj models.DefensePhase, vrijedi time.Time) *models.Akt {
		t.Helper()
		a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: radnja, Stupanj: stupanj, Vrijedi: vrijedi})
		if err != nil {
			t.Fatal(err)
		}
		if err := o.akti.Spremi(ctx, o.ovlasti, a); err != nil {
			t.Fatal(err)
		}
		return a
	}

	// prekid redovne koja ne traje: odbijen, sken nije spremljen
	prekid := nacrt(models.AktPrekid, models.PhaseRegular, sat)
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, prekid.ID, sken, o.rukovod.ID.String()); err == nil || !strings.Contains(err.Error(), "Redovna obrana ne traje") {
		t.Errorf("sken nemogućeg akta: %v", err)
	}
	if pdf, _ := o.akti.Izvornik(ctx, prekid.ID); len(pdf) != 0 {
		t.Error("sken odbijenog akta je spremljen")
	}

	// bez otvorenog dnevnika COP-a (preventivna obrana) sken se ne ovjerava,
	// kao ni izravno
	uspostava := nacrt(models.AktUspostava, models.PhasePrep, sat)
	o.akti.SetObrana(func(context.Context, string) *models.Journal { return nil }, nil)
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, uspostava.ID, sken, o.rukovod.ID.String()); err == nil || !strings.Contains(err.Error(), "preventivnoj obrani") {
		t.Errorf("sken u preventivnoj obrani: %v", err)
	}
	o.akti.SetObrana(nil, nil)

	// ispravan akt: ovjeren skenom, epizoda otvorena, ponovni sken odbijen
	a, upozorenja, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, uspostava.ID, sken, o.rukovod.ID.String())
	if err != nil || !a.Ovjeren() || a.Rucno == nil || len(upozorenja) != 0 {
		t.Fatalf("ovjera skenom: %+v %v %v", a, upozorenja, err)
	}
	if got := o.stanje(t); got["P.1.1"] != models.PhasePrep {
		t.Errorf("epizoda nakon ovjere skenom: %v", got)
	}
	if _, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, uspostava.ID, sken, o.rukovod.ID.String()); err == nil || !strings.Contains(err.Error(), "već ovjeren") {
		t.Errorf("ponovni sken: %v", err)
	}
}
