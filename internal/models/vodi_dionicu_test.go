package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// VodiDionicu: dnevno izvješće dionice pišu i predaju samo rukovoditelj
// dionice i zamjenik (Odluke (30. 10. 2026.), 3). Izmišljeni sektor P,
// područja 1 i 2.
func TestVodiDionicu(t *testing.T) {
	sektor := "P"
	jedan, dva := 1, 2
	prosla := time.Now().Add(-time.Hour)
	duznost := func(uloga Role, doseg ScopeType, podrucje *int, dionice string) Duty {
		return Duty{ID: uuid.New(), Role: uloga, ScopeType: doseg, SectorID: &sektor, AreaID: podrucje, SectionCodes: dionice, IsActive: true}
	}
	istekla := duznost(RoleSectionLeader, ScopeSection, &jedan, "P.1.1")
	istekla.ExpiresAt = &prosla
	for _, s := range []struct {
		ime            string
		d              Duty
		vodi11, vodiIj bool // vodi dionicu P.1.1; vodi ijednu dionicu
	}{
		{"rukovoditelj dionice P.1.1", duznost(RoleSectionLeader, ScopeSection, &jedan, "P.1.1"), true, true},
		{"zamjenik rukovoditelja dionice P.1.1", duznost(RoleSectionDeputy, ScopeSection, &jedan, "p.1.1"), true, true},
		{"rukovoditelj svih dionica područja 1", duznost(RoleSectionLeader, ScopeSection, &jedan, ""), true, true},
		{"rukovoditelj dionice P.1.2", duznost(RoleSectionLeader, ScopeSection, &jedan, "P.1.2"), false, true},
		{"rukovoditelj dionice područja 2", duznost(RoleSectionLeader, ScopeSection, &dva, ""), false, true},
		{"vodočuvar dionice P.1.1", duznost(RoleWaterGuard, ScopeSection, &jedan, "P.1.1"), false, false},
		{"rukovoditelj područja 1", duznost(RoleAreaLeader, ScopeArea, &jedan, ""), false, false},
		{"rukovoditelj sektora", duznost(RoleSectorLeader, ScopeSector, nil, ""), false, false},
		{"istekla dužnost", istekla, false, false},
		{"rukovoditelj dionice bez područja", duznost(RoleSectionLeader, ScopeSection, nil, "P.1.1"), false, false},
	} {
		p := NewUserPermissions(User{ID: uuid.New(), FullName: "Pero Perić", Duties: []Duty{s.d}})
		if got := p.VodiDionicu(1, "P.1.1"); got != s.vodi11 {
			t.Errorf("%s vodi P.1.1: %v, očekivano %v", s.ime, got, s.vodi11)
		}
		if got := p.VodiDionice(); got != s.vodiIj {
			t.Errorf("%s vodi dionice: %v, očekivano %v", s.ime, got, s.vodiIj)
		}
	}
	admin := NewUserPermissions(User{ID: uuid.New(), IsGlobalAdmin: true})
	if admin.VodiDionicu(1, "P.1.1") || admin.VodiDionice() {
		t.Error("globalni administrator bez dužnosti ne vodi dionicu")
	}
	p := NewUserPermissions(User{Duties: []Duty{duznost(RoleSectionLeader, ScopeSection, &jedan, "")}})
	if p.VodiDionicu(0, "P.1.1") || p.VodiDionicu(1, "") || (*UserPermissions)(nil).VodiDionicu(1, "P.1.1") || (*UserPermissions)(nil).VodiDionice() {
		t.Error("bez područja, dionice ili ovlasti")
	}
}
