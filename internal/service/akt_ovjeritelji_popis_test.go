package service_test

import (
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Pravilo kartice je isto kao pravilo ovjere: razina sektora sve, zamjenik
// za područje sve za svoje područje, rukovoditelj područja bez izvanredne
// obrane; neaktivni račun, istekla dužnost i drugi sektor ne
func TestOvjeriteljiSektora(t *testing.T) {
	b, c, p34, p40 := "B", "C", 34, 40
	duz := func(r models.Role, sektor *string, podrucje *int) models.Duty {
		return models.Duty{Role: r, SectorID: sektor, AreaID: podrucje, IsActive: true}
	}
	users := []models.User{
		{FullName: "Zamjenik", IsActive: true, Duties: []models.Duty{duz(models.RoleSectorAreaDeputy, &b, &p34)}},
		{FullName: "Rukovoditelj", IsActive: true, Duties: []models.Duty{duz(models.RoleSectorLeader, &b, nil)}},
		{FullName: "Drugi sektor", IsActive: true, Duties: []models.Duty{duz(models.RoleSectorLeader, &c, nil), duz(models.RoleAreaLeader, &c, &p40)}},
		{FullName: "Isključen", IsActive: false, Duties: []models.Duty{duz(models.RoleSectorLeader, &b, nil)}},
		{FullName: "Područje", IsActive: true, Duties: []models.Duty{duz(models.RoleAreaDeputy, &b, &p34)}},
		{FullName: "Vodočuvar", IsActive: true, Duties: []models.Duty{duz(models.RoleWaterGuard, &b, &p34)}},
	}
	got := service.OvjeriteljiSektora(users, "B", map[int]bool{34: true})
	if len(got) != 3 || got[0].Ime != "Rukovoditelj" || !got[0].SveFaze || got[0].AreaID != 0 ||
		got[1].Ime != "Područje" || got[1].SveFaze || got[2].Ime != "Zamjenik" || !got[2].SveFaze || got[2].AreaID != 34 {
		t.Errorf("%+v", got)
	}
	if got[0].Uloga != models.RoleSectorLeader.Label() {
		t.Errorf("uloga bez naziva dužnosti: %q", got[0].Uloga)
	}
}
