package models

import (
	"reflect"
	"slices"
	"testing"
)

// Rukovoditelj obrane izvođača za branjeno područje i zamjenik (Odluke
// (22. 11. 2026.)): u katalogu iza voditelja usluga, u skupini Vanjski
// suradnici, s dosegom branjenog područja, bez uprave; strana izvođača kao
// voditelj usluga.
func TestUlogeObraneIzvodjacaUKatalogu(t *testing.T) {
	vu := RoleServiceLeaderForeman.CatalogIndex()
	for i, s := range []struct {
		uloga Role
		naziv string
	}{
		{RoleContractorAreaLeader, "Rukovoditelj obrane izvođača za branjeno područje"},
		{RoleContractorAreaDeputy, "Zamjenik rukovoditelja obrane izvođača"},
	} {
		d := RoleCatalog[s.uloga.CatalogIndex()]
		if s.uloga.CatalogIndex() != vu+1+i || d.Group != RoleGroupExternal || d.Scope != ScopeArea || d.Name != s.naziv || d.Desc == "" {
			t.Errorf("%s: mjesto %d (voditelj usluga %d), %+v", s.uloga, s.uloga.CatalogIndex(), vu, d)
		}
		if s.uloga.RazinaUprave() != 0 || !s.uloga.Writes() || s.uloga.IsField() || s.uloga.ProgramUgovora() != "" {
			t.Errorf("%s: uprava %d, piše %v", s.uloga, s.uloga.RazinaUprave(), s.uloga.Writes())
		}
		if !reflect.DeepEqual(DefaultModules(s.uloga), DefaultModules(RoleServiceLeaderForeman)) {
			t.Errorf("%s: moduli %v", s.uloga, DefaultModules(s.uloga))
		}
	}
	if got := UlogeIzvodjaca(); !reflect.DeepEqual(got, []Role{RoleServiceLeaderForeman, RoleContractorAreaLeader, RoleContractorAreaDeputy}) {
		t.Errorf("uloge izvođača: %v", got)
	}
	UlogeIzvodjaca()[0] = RoleGuest // popis je kopija
	for _, d := range RoleCatalog {
		izvodjac := slices.Contains(UlogeIzvodjaca(), d.Role)
		obrana := d.Role == RoleContractorAreaLeader || d.Role == RoleContractorAreaDeputy
		if d.Role.Izvodjac() != izvodjac || d.Role.ObranaIzvodjaca() != obrana {
			t.Errorf("%s: izvođač %v, obrana izvođača %v", d.Role, d.Role.Izvodjac(), d.Role.ObranaIzvodjaca())
		}
	}
}

// Naziv se mijenja kao i ostalima (Administracija › Sudionici obrane)
func TestNazivUlogeObraneIzvodjaca(t *testing.T) {
	postaviNazive(t, OrgTerms{RoleLabels: map[string]string{string(RoleContractorAreaLeader): "Rukovoditelj izvođača"}})
	if RoleContractorAreaLeader.Label() != "Rukovoditelj izvođača" || RoleContractorAreaDeputy.Label() != "Zamjenik rukovoditelja obrane izvođača" {
		t.Errorf("nazivi: %q, %q", RoleContractorAreaLeader.Label(), RoleContractorAreaDeputy.Label())
	}
}

// Dužnost rukovoditelja obrane izvođača piše na svom području i ne daje rad
// u cijelom sektoru (pitanja 3 i 6); voditelj usluga radi u sektoru kao i dosad
func TestObranaIzvodjacaRadiNaPodrucju(t *testing.T) {
	for _, uloga := range []Role{RoleContractorAreaLeader, RoleContractorAreaDeputy} {
		p := ovlastiZa(aktivna(Duty{Role: uloga, ScopeType: ScopeArea, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(34)}))
		if !p.HasWriteAccess("", 34, "") || p.HasWriteAccess("B", 0, "") || p.HasWriteAccess("", 16, "") {
			t.Errorf("%s: pisanje %v", uloga, p.AllowedAreas)
		}
		if p.RadiUSektoru("B") || !p.RadiUPodrucju(34) || len(p.SektoriRada()) != 0 {
			t.Errorf("%s: radi u sektoru %v, na području %v", uloga, p.RadiUSektoru("B"), p.RadiUPodrucju(34))
		}
	}
	vu := ovlastiZa(aktivna(Duty{Role: RoleServiceLeaderForeman, ScopeType: ScopeArea, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(34)}))
	if !vu.RadiUSektoru("B") {
		t.Error("voditelj usluga više ne radi u sektoru")
	}
}

// Firma s računa i registar: naziv do pravnog oblika, puni ili kratki;
// veza na područje, a ne na sektor
func TestFirmaRacunaURegistru(t *testing.T) {
	firme := []Contractor{
		{ID: "1", Name: "Meandar Proba d.o.o.", ShortName: "Meandar", Assignments: []ContractorAssignment{{SectorID: "B", AreaID: 34}}},
		{ID: "2", Name: "Nasip Primjer d.d.", Assignments: []ContractorAssignment{{SectorID: "B", AreaID: 0}}},
	}
	for _, s := range []struct {
		firma string
		id    string
	}{
		{"MEANDAR PROBA DOO, Primjerska 1", "1"},
		{"Meandar", "1"},
		{"Nasip Primjer", "2"},
		{"Meandar Proba Primjerica d.d.", ""},
		{"", ""},
	} {
		nadjene := FirmeRacuna(&User{OrgName: s.firma}, firme)
		got := ""
		if len(nadjene) == 1 {
			got = nadjene[0].ID
		}
		if got != s.id || len(nadjene) > 1 {
			t.Errorf("%q: %v", s.firma, nadjene)
		}
	}
	if FirmeRacuna(nil, firme) != nil {
		t.Error("bez računa nema firme")
	}
	if !firme[0].VezanaNa(34) || firme[0].VezanaNa(16) || firme[1].VezanaNa(34) {
		t.Error("veza na područje")
	}
}
