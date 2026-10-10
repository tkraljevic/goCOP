package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// RadiNaDionici: tko ima dužnost na dionici odgovara za njezinu letvu
// (Odluke (8. 10. 2026.), pitanje 9). Izmišljeni sektor P, područja 1 i 2.
func TestRadiNaDionici(t *testing.T) {
	sektor := "P"
	jedan, dva := 1, 2
	prosla := time.Now().Add(-time.Hour)
	duznost := func(uloga Role, doseg ScopeType, podrucje *int, dionice string) Duty {
		return Duty{ID: uuid.New(), Role: uloga, ScopeType: doseg, SectorID: &sektor, AreaID: podrucje, SectionCodes: dionice, IsActive: true}
	}
	istekla := duznost(RoleWaterGuard, ScopeSection, &jedan, "")
	istekla.ExpiresAt = &prosla
	iskljucena := duznost(RoleWaterGuard, ScopeSection, &jedan, "")
	iskljucena.IsActive = false
	for _, s := range []struct {
		ime    string
		d      Duty
		radi11 bool // na dionici P.1.1 područja 1
	}{
		{"rukovoditelj dionice P.1.1", duznost(RoleSectionLeader, ScopeSection, &jedan, "P.1.1"), true},
		{"rukovoditelj dionice p.1.1, mala slova", duznost(RoleSectionLeader, ScopeSection, &jedan, "p.1.1"), true},
		{"vodočuvar dionice P.1.2", duznost(RoleWaterGuard, ScopeSection, &jedan, "P.1.2"), false},
		{"vodočuvar cijelog područja 1", duznost(RoleWaterGuard, ScopeSection, &jedan, ""), true},
		{"vodočuvar cijelog područja 2", duznost(RoleWaterGuard, ScopeSection, &dva, ""), false},
		// dužnost područja ili sektora nije dužnost na dionici
		{"skladištar područja 1", duznost(RoleWarehouseKeeper, ScopeArea, &jedan, ""), false},
		{"Dežurni operater u COP-u", duznost(RoleOperator, ScopeSector, nil, ""), false},
		{"gost na dionici", duznost(RoleGuest, ScopeSection, &jedan, "P.1.1"), false},
		{"istekla dužnost", istekla, false},
		{"isključena dužnost", iskljucena, false},
	} {
		p := NewUserPermissions(User{ID: uuid.New(), FullName: "Pero Perić", Duties: []Duty{s.d}})
		if got := p.RadiNaDionici(1, "P.1.1"); got != s.radi11 {
			t.Errorf("%s: %v, očekivano %v", s.ime, got, s.radi11)
		}
	}
	p := NewUserPermissions(User{Duties: []Duty{duznost(RoleWaterGuard, ScopeSection, &jedan, "")}})
	if p.RadiNaDionici(0, "P.1.1") || p.RadiNaDionici(1, "") || (*UserPermissions)(nil).RadiNaDionici(1, "P.1.1") {
		t.Error("bez područja, dionice ili ovlasti")
	}
}
