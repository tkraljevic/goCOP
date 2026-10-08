package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Dnevnike usluga A.02 i A.03 izvođač čita samo na branjenom području svoje
// dužnosti, a ne dnevnike drugih firmi na drugim područjima (Odluke (8. 10.
// 2026.), pitanje 1); ostali ih čitaju kao i dosad. Istekla dužnost
// poslovođe ne otvara ni njegovo područje; bez ovlasti nema čitanja.
func TestIzvodjacVidiSamoSvojDnevnikUsluge(t *testing.T) {
	s := &JournalService{}
	sektor := "P"
	p1, p2 := 1, 2
	jucer := time.Now().Add(-time.Hour)
	osoba := func(duznosti ...models.Duty) *models.UserPermissions {
		for i := range duznosti {
			duznosti[i].IsActive, duznosti[i].SectorID = true, &sektor
		}
		return models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Marko Markić", Duties: duznosti})
	}
	poslovodja := models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &p1}
	istekliPoslovodja := models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &p1, ExpiresAt: &jucer}
	podrucje1 := models.OpsegPodrucja(models.Area{ID: 1, SectorID: "P"})
	podrucje2 := models.OpsegPodrucja(models.Area{ID: 2, SectorID: "P"})
	administrator := osoba(poslovodja)
	administrator.IsGlobalAdmin = true
	slucajevi := []struct {
		naziv       string
		perms       *models.UserPermissions
		prvo, drugo bool
	}{
		{"poslovođa na području 1", osoba(poslovodja), true, false},
		{"istekli poslovođa", osoba(istekliPoslovodja), false, false},
		{"poslovođa koji je i vodočuvar područja 2", osoba(poslovodja, models.Duty{Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, AreaID: &p2}), true, true},
		{"poslovođa s administracijom", administrator, true, true},
		{"rukovoditelj područja 2", osoba(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &p2}), true, true},
		{"gost", osoba(models.Duty{Role: models.RoleGuest, ScopeType: models.ScopeAll}), true, true},
		{"bez ovlasti", nil, false, false},
	}
	for _, sl := range slucajevi {
		if got := s.VidiDnevnikUsluge(sl.perms, podrucje1); got != sl.prvo {
			t.Errorf("%s: dnevnik područja 1 %v, očekivano %v", sl.naziv, got, sl.prvo)
		}
		if got := s.VidiDnevnikUsluge(sl.perms, podrucje2); got != sl.drugo {
			t.Errorf("%s: dnevnik područja 2 %v, očekivano %v", sl.naziv, got, sl.drugo)
		}
	}
}
