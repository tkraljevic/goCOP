package models

import (
	"testing"
	"time"
)

// Odluke (30. 10. 2026.), 7 i 17: prognozu rukom pokreću dežurni, uprava
// sektora (i voditelj centra) i globalni administrator. Dežurni je onaj kome
// dužnost dežurnog operatera sada vrijedi.
func TestDezurniISmijeGeneriratiPrognozu(t *testing.T) {
	prije, poslije := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	sektor, podrucje := "B", 16
	s := func(d ...Duty) *UserPermissions { return &UserPermissions{User: User{Duties: d}} }
	for _, x := range []struct {
		opis              string
		p                 *UserPermissions
		dezurni, generira bool
	}{
		{"nitko", nil, false, false},
		{"bez dužnosti", s(), false, false},
		{"vodočuvar", s(Duty{Role: RoleWaterGuard, IsActive: true}), false, false},
		{"dežurni", s(Duty{Role: RoleWaterGuard, IsActive: true}, Duty{Role: RoleOperator, IsActive: true, ExpiresAt: &poslije}), true, true},
		{"dežurstvo isteklo", s(Duty{Role: RoleOperator, IsActive: true, ExpiresAt: &prije}), false, false},
		{"dežurstvo opozvano", s(Duty{Role: RoleOperator}), false, false},
		{"globalni administrator", &UserPermissions{IsGlobalAdmin: true}, false, true},
		{"voditelj centra", s(Duty{Role: RoleCopLeader, SectorID: &sektor, IsActive: true}), false, true},
		{"rukovoditelj sektora", s(Duty{Role: RoleSectorLeader, SectorID: &sektor, IsActive: true}), false, true},
		{"uprava sektora istekla", s(Duty{Role: RoleSectorDeputy, SectorID: &sektor, IsActive: true, ExpiresAt: &prije}), false, false},
		{"uprava sektora bez sektora", s(Duty{Role: RoleSectorLeader, IsActive: true}), false, false},
		{"rukovoditelj branjenog područja", s(Duty{Role: RoleAreaLeader, AreaID: &podrucje, IsActive: true}), false, false},
	} {
		if got := x.p.Dezurni(); got != x.dezurni {
			t.Errorf("%s: dežurni %v", x.opis, got)
		}
		if got := x.p.SmijeGeneriratiPrognozu(); got != x.generira {
			t.Errorf("%s: smije generirati %v", x.opis, got)
		}
	}
}
