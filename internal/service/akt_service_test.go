package service

import (
	"testing"

	"gocop/internal/models"
)

func TestPotpisNosiNajvisuFunkciju(t *testing.T) {
	b := "B"
	u := &models.User{FullName: "Ivo Ivić", Duties: []models.Duty{
		{Title: "Rukovoditelj dionice B.34.1", Role: models.RoleSectionLeader, SectorID: &b, IsActive: true, IsPrimary: true},
		{Title: "Zamjenik rukovoditelja obrane Sektora B", Role: models.RoleSectorDeputy, SectorID: &b, IsActive: true},
		{Title: "Bivši rukovoditelj sektora", Role: models.RoleSectorLeader, SectorID: &b, IsActive: false},
	}}
	if d := najvisaDuznost(u); d == nil || d.Role != models.RoleSectorDeputy {
		t.Fatalf("najviša dužnost: %+v", d)
	}
	if d := najvisaDuznost(&models.User{Duties: []models.Duty{{Title: "Vodočuvar", Role: models.RoleWaterGuard, IsActive: true}}}); d == nil || d.Title != "Vodočuvar" {
		t.Fatalf("jedina dužnost: %+v", d)
	}
}
