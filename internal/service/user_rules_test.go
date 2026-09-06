package service

import (
	"errors"
	"testing"

	"gocop/internal/models"

	"github.com/google/uuid"
)

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }

func permsWith(duties ...models.Duty) *models.UserPermissions {
	u := models.User{ID: uuid.New(), Duties: duties}
	for i := range u.Duties {
		u.Duties[i].IsActive = true
	}
	return models.NewUserPermissions(u)
}

var sectorsOf = func(area int) string {
	if area == 16 || area == 18 {
		return "B"
	}
	if area == 10 {
		return "D"
	}
	return ""
}

func TestDosegSamNeDajeUpravu(t *testing.T) {
	p := permsWith(models.Duty{Role: models.RoleViewer, ScopeType: models.ScopeAll, SectorID: strp("B")})
	if p.IsGlobalAdmin {
		t.Fatal("doseg ALL ne smije davati globalnog administratora")
	}
	if p.HasWriteAccess("B", 0, "") {
		t.Fatal("preglednik ne piše ni u svom sektoru")
	}
	g := permsWith(models.Duty{Role: models.RoleGuest, ScopeType: models.ScopeArea, AreaID: intp(16)})
	if g.HasWriteAccess("", 16, "") {
		t.Fatal("gost ne piše")
	}
}

func TestUpravaPodrucjaNijeUpravaSektora(t *testing.T) {
	p := permsWith(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: strp("B"), AreaID: intp(16)})
	if len(p.AdminSectors) != 0 || !p.AdminAreas[16] {
		t.Fatalf("rukovoditelj područja upravlja samo područjem: %+v", p)
	}
	f := permsWith(models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, SectorID: strp("B"), AreaID: intp(16)})
	if len(f.AdminAreas) != 0 || !f.HasWriteAccess("", 16, "") {
		t.Fatalf("voditelj usluga piše, ali ne upravlja računima: %+v", f)
	}
}

func TestTkoSmijeDodijelitiKojuUlogu(t *testing.T) {
	area := permsWith(models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: strp("B"), AreaID: intp(16)})
	sector := permsWith(models.Duty{Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: strp("B")})
	top := permsWith(models.Duty{Role: models.RoleNationalLeader, ScopeType: models.ScopeAll})

	cases := []struct {
		name   string
		actor  *models.UserPermissions
		role   models.Role
		sector *string
		area   *int
		ok     bool
	}{
		{"područje daje vodočuvara u svom području", area, models.RoleWaterGuard, nil, intp(16), true},
		{"područje ne daje vodočuvara tuđem području", area, models.RoleWaterGuard, nil, intp(18), false},
		{"područje ne daje ulogu sektora", area, models.RoleSectorLeader, strp("B"), nil, false},
		{"područje ne daje glavnog rukovoditelja", area, models.RoleNationalLeader, nil, intp(16), false},
		{"područje daje rukovoditelja područja (ista razina)", area, models.RoleAreaDeputy, nil, intp(16), true},
		{"sektor daje rukovoditelja područja u svom sektoru", sector, models.RoleAreaLeader, nil, intp(16), true},
		{"sektor ne daje u tuđem sektoru", sector, models.RoleAreaLeader, nil, intp(10), false},
		{"sektor ne daje zamjenika glavnog za sektor (razina 1)", sector, models.RoleSectorMainDeputy, strp("B"), nil, false},
		{"sektor daje voditelja COP-a", sector, models.RoleCopLeader, strp("B"), nil, true},
		{"uprava organizacije daje sve", top, models.RoleSectorMainDeputy, strp("D"), nil, true},
		{"nitko osim globalnog ne daje GLOBAL_ADMIN", sector, models.RoleGlobalAdmin, strp("B"), nil, false},
	}
	for _, c := range cases {
		err := mayAssign(c.actor, c.role, c.sector, c.area, sectorsOf)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v, očekivano ok=%v", c.name, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: greška mora biti ErrUnauthorized, dobiveno %v", c.name, err)
		}
	}
}

func TestDosegIzUloge(t *testing.T) {
	scope, sec, area, err := normalizeScope(models.RoleAreaLeader, nil, intp(16), "", sectorsOf)
	if err != nil || scope != models.ScopeArea || sec == nil || *sec != "B" || area == nil {
		t.Fatalf("područje: scope=%v sec=%v area=%v err=%v", scope, sec, area, err)
	}
	if _, _, _, err := normalizeScope(models.RoleSectorLeader, nil, nil, "", sectorsOf); err == nil {
		t.Fatal("uloga sektora bez sektora mora javiti grešku")
	}
	if _, _, _, err := normalizeScope(models.RoleAreaLeader, strp("B"), nil, "", sectorsOf); err == nil {
		t.Fatal("uloga područja bez područja mora javiti grešku")
	}
	scope, sec, area, err = normalizeScope(models.RoleNationalLeader, strp("B"), intp(16), "B.16.1", sectorsOf)
	if err != nil || scope != models.ScopeAll || sec != nil || area != nil {
		t.Fatalf("razina 1: scope=%v sec=%v area=%v err=%v", scope, sec, area, err)
	}
	scope, _, _, err = normalizeScope(models.RoleWaterGuard, nil, intp(16), "", sectorsOf)
	if err != nil || scope != models.ScopeArea {
		t.Fatalf("vodočuvar bez dionica pokriva područje: scope=%v err=%v", scope, err)
	}
	scope, _, _, _ = normalizeScope(models.RoleWaterGuard, nil, intp(16), "B.16.1", sectorsOf)
	if scope != models.ScopeSection {
		t.Fatalf("vodočuvar s dionicama: scope=%v", scope)
	}
}
