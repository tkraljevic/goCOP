package models

import (
	"testing"
	"time"
)

// Državna uprava: glavni rukovoditelj, voditelj Glavnog centra i zamjenici;
// istekla dužnost i uprava sektora ne
func TestDrzavnaUprava(t *testing.T) {
	jucer := time.Now().AddDate(0, 0, -1)
	for _, s := range []struct {
		opis string
		d    Duty
		zeli bool
	}{
		{"glavni rukovoditelj", Duty{Role: RoleNationalLeader, IsActive: true}, true},
		{"zamjenik voditelja Glavnog centra", Duty{Role: RoleMainCenterDeputy, IsActive: true}, true},
		{"rukovoditelj sektora", Duty{Role: RoleSectorLeader, IsActive: true}, false},
		{"istekla dužnost", Duty{Role: RoleNationalLeader, IsActive: true, ExpiresAt: &jucer}, false},
	} {
		p := &UserPermissions{User: User{Duties: []Duty{s.d}}}
		if got := p.DrzavnaUprava(); got != s.zeli {
			t.Errorf("%s: %v", s.opis, got)
		}
	}
	if (*UserPermissions)(nil).DrzavnaUprava() {
		t.Error("bez prava")
	}
}
