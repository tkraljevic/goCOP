package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Predano izvješće dionice mijenja samo onaj tko ga je izradio, ili uprava
// koja ga smije pisati; nacrt koji nikad nije predan mijenja svatko tko
// vodi dionicu (Odluke (8. 10. 2026.), pitanje 12, i (30. 10. 2026.), 3).
// Osobe su izmišljene.
func TestSmijeMijenjatiIzvjesceDionice(t *testing.T) {
	s := &IzvjescaService{}
	sektor, podrucje := "P", 1
	sec := &models.Section{Code: "P.1.1", SectorID: sektor, AreaID: podrucje}
	osoba := func(duznosti ...models.Duty) (*models.User, *models.UserPermissions) {
		u := &models.User{ID: uuid.New(), FullName: "Ivo Ivić", Duties: duznosti}
		return u, models.NewUserPermissions(*u)
	}
	dionica := models.Duty{Role: models.RoleSectionDeputy, ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: "P.1.1", IsActive: true}
	uprava := models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsActive: true}
	zamjenik, zOvl := osoba(dionica)
	_, uprOvl := osoba(uprava)
	upravaIDionica, uidOvl := osoba(dionica, uprava)
	sad := time.Now()
	nacrt := &models.DnevnoIzvjesce{SectionCode: "P.1.1", IzradioID: uuid.NewString()}
	predano := &models.DnevnoIzvjesce{SectionCode: "P.1.1", IzradioID: uuid.NewString(), PredanoAt: &sad}
	svoje := &models.DnevnoIzvjesce{SectionCode: "P.1.1", IzradioID: zamjenik.ID.String(), PredanoAt: &sad}
	for _, k := range []struct {
		naziv string
		u     *models.User
		p     *models.UserPermissions
		iz    *models.DnevnoIzvjesce
		smije bool
	}{
		{"zamjenik, nacrt", zamjenik, zOvl, nacrt, true},
		{"zamjenik, tuđe predano", zamjenik, zOvl, predano, false},
		{"zamjenik, svoje predano", zamjenik, zOvl, svoje, true},
		{"uprava koja vodi dionicu, tuđe predano", upravaIDionica, uidOvl, predano, true},
		{"uprava koja ne vodi dionicu, nacrt", upravaIDionica, uprOvl, nacrt, false},
		{"bez osobe", nil, zOvl, nacrt, false},
		{"bez izvješća", zamjenik, zOvl, nil, false},
	} {
		if got := s.SmijeMijenjati(k.u, k.p, sec, k.iz); got != k.smije {
			t.Errorf("%s: mijenja %v, očekivano %v", k.naziv, got, k.smije)
		}
	}
}
