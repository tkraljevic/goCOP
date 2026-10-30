package models

import (
	"testing"
	"time"
)

// Odluke (30. 10. 2026.), 7: prognozu rukom pokreću dežurni i globalni
// administrator. Dežurni je onaj kome dužnost dežurnog operatera sada
// vrijedi.
func TestDezurniISmijeGeneriratiPrognozu(t *testing.T) {
	prije, poslije := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
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
	} {
		if got := x.p.Dezurni(); got != x.dezurni {
			t.Errorf("%s: dežurni %v", x.opis, got)
		}
		if got := x.p.SmijeGeneriratiPrognozu(); got != x.generira {
			t.Errorf("%s: smije generirati %v", x.opis, got)
		}
	}
}
