package web

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Strojar, rukovatelj i voditelj posade s područjem imaju Naslovnu
// objekata; bez područja nemaju
func TestDuznostNaObjektu(t *testing.T) {
	for _, uloga := range []models.Role{models.RoleMachinist, models.RoleFacilityOperator, models.RoleCrewLeader} {
		p := dozvoleUsluge(uloga)
		if ulogaNaslovne(p, sadNaslovne) != uObjekt || karticeUloge(p)[0] != kMojiObjekti {
			t.Errorf("%s: %s", uloga, ulogaNaslovne(p, sadNaslovne))
		}
	}
	if duznostNaObjektu(&dozvoleNaslovne(false, models.RoleMachinist).User, sadNaslovne) != nil {
		t.Error("strojar bez područja")
	}
}

// Moji objekti: očitan danas sa stanjem i vodostajem, neočitan s gumbom
// „Upiši”; letve koje nisu crpne stanice ni ustave ne ulaze
func TestKarticaMojihObjekata(t *testing.T) {
	cs, u := models.StructureKindPumpingStation, models.StructureKindSluice
	ocitan := func(g models.GaugeSummary) models.GaugeSummary {
		g.DoneToday, g.Danas, g.NewURL = true, g.Latest, "/readings/new?structure="+g.StructureID
		return g
	}
	objekti := objektiOcitanja([]models.GaugeSummary{
		ocitan(objekt("CS Draž", cs, 16, &models.Reading{StructureState: models.StructureStateStarting, LevelCm: cmNaslovne(186), MeasuredAt: sadNaslovne}, nil)),
		ocitan(objekt("Ustava Draž", u, 16, &models.Reading{Gate: models.GateClosed, MeasuredAt: sadNaslovne}, nil)),
		{Name: "CS Tikveš", StructureID: "t", Kind: cs, NewURL: "/readings/new?structure=t"},
		{Name: "Batina", StationID: "b", Kind: "POSTAJA"},
		objekt("Nasip", models.StructureKindEmbankment, 16, nil, nil),
	})
	k := karticaMojihObjekata(objekti, sadNaslovne)
	if len(k.Redovi) != 3 {
		t.Fatalf("redovi: %+v", k.Redovi)
	}
	if r := k.Redovi[0]; !strings.HasPrefix(r.Sporedno, "radi · 10:00") || r.Vrijednost != "186 cm" {
		t.Errorf("crpna stanica: %+v", r)
	}
	if r := k.Redovi[1]; !strings.HasPrefix(r.Sporedno, "zatvorena") || r.Ikona != "ustava" {
		t.Errorf("ustava: %+v", r)
	}
	if r := k.Redovi[2]; r.Gumb == nil || r.Gumb.URL != "/readings/new?structure=t&back=/" || r.Ikona != "crpna-stanica" {
		t.Errorf("neočitana: %+v", r)
	}
	if k := karticaMojihObjekata([]models.GaugeSummary{{Name: "Ustava Bilje", StructureID: "bi", Kind: u}}, sadNaslovne); k.Redovi[0].Ikona != "ustava" || k.Redovi[0].Gumb == nil {
		t.Errorf("neočitana ustava: %+v", k.Redovi)
	}
	if k := karticaMojihObjekata(nil, sadNaslovne); k.Prazno == nil {
		t.Error("bez objekata")
	}
	for _, s := range []struct {
		r     models.Reading
		vrsta string
		zeli  string
	}{
		{models.Reading{StructureState: models.StructureStateIdle}, cs, "mirovanje"},
		{models.Reading{}, cs, "očitana"},
		{models.Reading{}, u, "očitana"},
	} {
		if g := stanjeObjekta(s.r, s.vrsta); g != s.zeli {
			t.Errorf("%+v: %q", s, g)
		}
	}
}

// Naslovna strojara kroz rukovatelja: traka, objekti iz terenskog
// pregleda (uobičajeni, a dok ih nema svi) i nalozi „U izradi”
func TestNaslovnaNaObjektu(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	p := dozvoleUsluge(models.RoleMachinist)
	if w := o.zovi("/naslovna/kartica/moji-objekti", p).Body.String(); !strings.Contains(w, "Objekti trenutačno nisu dostupni.") {
		t.Errorf("bez izvora: %s", w)
	}
	if w := o.zovi("/naslovna/kartica/nalozi-objektu", p).Body.String(); !strings.Contains(w, "U izradi") {
		t.Errorf("nalozi: %s", w)
	}
	fo := &service.FieldOverview{Others: []models.GaugeSummary{{Name: "CS Bakanka", StructureID: "b", Kind: models.StructureKindPumpingStation, NewURL: "/readings/new?structure=b"}}}
	o.h.SetIzvori(IzvoriNaslovne{
		Teren: func(context.Context, *models.UserPermissions, *models.User) (*service.FieldOverview, error) {
			return fo, nil
		},
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, nil
		},
	})
	if s := o.zovi("/", p).Body.String(); !strings.Contains(s, "BP 16: preventivna obrana") || !strings.Contains(s, `data-lijeno="/naslovna/kartica/moji-objekti"`) {
		t.Error("Naslovna strojara bez trake ili kartice")
	}
	if w := o.zovi("/naslovna/kartica/moji-objekti", p).Body.String(); !strings.Contains(w, "CS Bakanka") || !strings.Contains(w, "Upiši") {
		t.Errorf("objekti područja: %s", w)
	}
	fo.Mine = []models.GaugeSummary{{Name: "CS Draž", StructureID: "d", Kind: models.StructureKindPumpingStation, NewURL: "/readings/new?structure=d"}}
	if w := o.zovi("/naslovna/kartica/moji-objekti", p).Body.String(); !strings.Contains(w, "CS Draž") || strings.Contains(w, "CS Bakanka") {
		t.Errorf("uobičajeni objekti: %s", w)
	}
	o.h.SetIzvori(IzvoriNaslovne{Teren: func(context.Context, *models.UserPermissions, *models.User) (*service.FieldOverview, error) {
		return nil, errors.New("baza zauzeta")
	}})
	if w := o.zovi("/naslovna/kartica/moji-objekti", p).Body.String(); !strings.Contains(w, "Objekti trenutačno nisu dostupni.") {
		t.Errorf("s greškom: %s", w)
	}
}
