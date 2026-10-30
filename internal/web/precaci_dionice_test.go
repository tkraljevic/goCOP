package web

import (
	"slices"
	"testing"

	"gocop/internal/models"
)

// Prečac „Novo dnevno izvješće” vidi samo tko vodi dionicu na upisanom
// branjenom području; uprava područja i rukovoditelj dionice bez područja
// ga ne vide, jer izvješće ne bi mogli napisati (Odluke (30. 10. 2026.), 3)
func TestPrecacNovogIzvjescaSamoRukovoditeljuDionice(t *testing.T) {
	ima := func(p *models.UserPermissions) bool {
		return slices.ContainsFunc(precaciZa(p), func(p Precac) bool { return p.Link == "/izvjesca/novo" })
	}
	if !ima(pravaDuznosti(models.RoleSectionLeader, models.ScopeSection, "P", 1, "P.1.1")) {
		t.Error("rukovoditelj dionice ne vidi prečac")
	}
	if ima(pravaDuznosti(models.RoleSectionLeader, models.ScopeSection, "P", 0, "P.1.1")) {
		t.Error("rukovoditelj dionice bez područja vidi prečac")
	}
	if ima(pravaDuznosti(models.RoleAreaLeader, models.ScopeArea, "P", 1, "")) {
		t.Error("uprava područja vidi prečac")
	}
}
