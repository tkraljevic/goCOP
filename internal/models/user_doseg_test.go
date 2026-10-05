package models

import (
	"testing"
	"time"
)

// Pravo pisanja ide po dosegu dužnosti, ne po sektoru koji dužnost nosi:
// dužnost područja piše u području, dužnost dionica na dionicama, terenska
// dužnost bez dionica u cijelom području. Sektor i područje svake dužnosti
// ostaju za prikaz (SektoriDuznosti, PodrucjaDuznosti).
func TestPravoPisanjaPoDosegu(t *testing.T) {
	b := "B"
	p16 := 16
	tests := []struct {
		naziv    string
		d        Duty
		sektor   bool // AllowedSectors[B]
		podrucje bool // AllowedAreas[16]
		dionica  bool // AllowedSections[B.16.1]
	}{
		{"rukovoditelj sektora", Duty{Role: RoleSectorLeader, ScopeType: ScopeSector, SectorID: &b}, true, false, false},
		{"operater sa sektorom i područjem", Duty{Role: RoleOperator, ScopeType: ScopeSector, SectorID: &b, AreaID: &p16}, true, true, false},
		{"rukovoditelj područja", Duty{Role: RoleAreaLeader, ScopeType: ScopeArea, SectorID: &b, AreaID: &p16}, false, true, false},
		{"rukovoditelj dionice", Duty{Role: RoleSectionLeader, ScopeType: ScopeSection, SectorID: &b, AreaID: &p16, SectionCodes: "B.16.1"}, false, false, true},
		{"vodočuvar bez dionica", Duty{Role: RoleWaterGuard, ScopeType: ScopeArea, SectorID: &b, AreaID: &p16}, false, true, false},
		{"stari zapis dionice bez dionica", Duty{Role: RoleWaterGuard, ScopeType: ScopeSection, SectorID: &b, AreaID: &p16}, false, true, false},
		{"doseg iz uloge kad ga zapis nema", Duty{Role: RoleAreaLeader, SectorID: &b, AreaID: &p16}, false, true, false},
		{"preglednik ne piše", Duty{Role: RoleViewer, ScopeType: ScopeAll, SectorID: &b, AreaID: &p16}, false, false, false},
	}
	for _, tc := range tests {
		tc.d.IsActive = true
		p := NewUserPermissions(User{Duties: []Duty{tc.d}})
		if p.AllowedSectors["B"] != tc.sektor || p.AllowedAreas[16] != tc.podrucje || p.AllowedSections["B.16.1"] != tc.dionica {
			t.Errorf("%s: sektor %v područje %v dionica %v, želi %v %v %v", tc.naziv,
				p.AllowedSectors["B"], p.AllowedAreas[16], p.AllowedSections["B.16.1"], tc.sektor, tc.podrucje, tc.dionica)
		}
		if got := p.HasWriteAccess("B", 17, ""); got != tc.sektor {
			t.Errorf("%s: HasWriteAccess(B, 17) = %v, želi %v", tc.naziv, got, tc.sektor)
		}
		pise := tc.d.Role.Writes()
		if p.RadiUSektoru("B") != pise || p.RadiUPodrucju(16) != (pise && tc.d.AreaID != nil) {
			t.Errorf("%s: za prikaz sektor %v područje %v (piše %v)", tc.naziv, p.RadiUSektoru("B"), p.RadiUPodrucju(16), pise)
		}
	}

	// istekla dužnost ne daje ništa
	jucer := time.Now().Add(-time.Hour)
	p := NewUserPermissions(User{Duties: []Duty{{Role: RoleSectorLeader, ScopeType: ScopeSector, SectorID: &b, IsActive: true, ExpiresAt: &jucer}}})
	if p.AllowedSectors["B"] || p.RadiUSektoru("B") || len(p.SektoriRada()) != 0 {
		t.Error("istekla dužnost daje pravo pisanja ili sektor rada")
	}
}

// Dnevnik otvara pojedina dužnost, ne osoba: aktivna, neistekla, s ulogom
// koja čita dnevnik. Istekla ili neaktivna dužnost i uloga koja ne čita
// (gost, preglednik, skladištar) ne otvaraju ga ni kad osoba ima drugu
// valjanu dužnost.
func TestDuznostCitaVodocuvarskiDnevnik(t *testing.T) {
	sad := time.Now()
	jucer := sad.Add(-time.Hour)
	sutra := sad.Add(time.Hour)
	slucajevi := []struct {
		ime  string
		d    Duty
		cita bool
	}{
		{"rukovoditelj područja", Duty{IsActive: true, Role: RoleAreaLeader}, true},
		{"vodočuvar s budućim istekom", Duty{IsActive: true, Role: RoleWaterGuard, ExpiresAt: &sutra}, true},
		{"istekli rukovoditelj područja", Duty{IsActive: true, Role: RoleAreaLeader, ExpiresAt: &jucer}, false},
		{"neaktivni rukovoditelj područja", Duty{Role: RoleAreaLeader}, false},
		{"gost", Duty{IsActive: true, Role: RoleGuest}, false},
		{"preglednik", Duty{IsActive: true, Role: RoleViewer}, false},
		{"skladištar", Duty{IsActive: true, Role: RoleWarehouseKeeper}, false},
	}
	for _, s := range slucajevi {
		if got := s.d.CitaVodocuvarskiDnevnik(sad); got != s.cita {
			t.Errorf("%s: CitaVodocuvarskiDnevnik = %v, očekivano %v", s.ime, got, s.cita)
		}
	}
}
