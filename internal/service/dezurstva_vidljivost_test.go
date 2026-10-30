package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Plan dežurstava centra sektora P vide uprava i djelatnici sektora P (i s
// dužnošću samo na području ili dionici u njemu, i uz privremenu dužnost u
// njemu), uprava organizacije i globalni administrator. Djelatnik drugog
// sektora, gost, poslovođa izvođača i osoba bez prijave ga ne vide, a
// dnevnik usluge plana nema (Odluke (30. 10. 2026.)). Podaci su izmišljeni.
func TestTkoVidiPlanDezurstava(t *testing.T) {
	s := &JournalService{}
	cop := &models.Journal{ID: "cop", Kind: models.JournalKindDefense, CentarSektor: "P"}
	o := models.OpsegDnevnika(*cop, nil, []models.Area{{ID: 1, SectorID: "P"}, {ID: 2, SectorID: "P"}, {ID: 7, SectorID: "D"}})
	ovlasti := func(d models.Duty) *models.UserPermissions {
		d.IsActive = true
		return models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{d}})
	}
	p, d, prvo, sedmo := "P", "D", 1, 7
	vide := map[string]*models.UserPermissions{
		"voditelj centra":                  ovlasti(models.Duty{Role: models.RoleCopLeader, SectorID: &p}),
		"dežurni operater":                 ovlasti(models.Duty{Role: models.RoleOperator, SectorID: &p}),
		"rukovoditelj područja":            ovlasti(models.Duty{Role: models.RoleAreaLeader, SectorID: &p, AreaID: &prvo}),
		"dužnost samo na području":         ovlasti(models.Duty{Role: models.RoleAreaDeputy, AreaID: &prvo}),
		"vodočuvar na dionici":             ovlasti(models.Duty{Role: models.RoleWaterGuard, SectorID: &p, AreaID: &prvo, SectionCodes: "P.1.1"}),
		"ispomoć iz drugog sektora":        ovlasti(models.Duty{Role: models.RoleOperator, SectorID: &p, IsTemporary: true}),
		"glavni rukovoditelj":              ovlasti(models.Duty{Role: models.RoleNationalLeader}),
		"globalni administrator":           {IsGlobalAdmin: true},
		"globalni administrator i izvođač": models.NewUserPermissions(models.User{IsGlobalAdmin: true, Duties: []models.Duty{{Role: models.RoleServiceLeaderForeman, SectorID: &p, AreaID: &prvo, IsActive: true}}}),
	}
	ne := map[string]*models.UserPermissions{
		"rukovoditelj područja drugog sektora": ovlasti(models.Duty{Role: models.RoleAreaLeader, SectorID: &d, AreaID: &sedmo}),
		"dežurni drugog sektora":               ovlasti(models.Duty{Role: models.RoleOperator, SectorID: &d}),
		"gost":                                 ovlasti(models.Duty{Role: models.RoleGuest, SectorID: &p}),
		"poslovođa izvođača":                   ovlasti(models.Duty{Role: models.RoleServiceLeaderForeman, SectorID: &p, AreaID: &prvo}),
		"bez prijave":                          nil,
	}
	for tko, perms := range ne {
		if s.VidiPlanDezurstava(perms, o, cop) {
			t.Errorf("%s vidi plan dežurstava sektora P", tko)
		}
	}
	for tko, perms := range vide {
		if !s.VidiPlanDezurstava(perms, o, cop) {
			t.Errorf("%s ne vidi plan dežurstava sektora P", tko)
		}
	}
	usluga := &models.Journal{ID: "a02", Kind: models.JournalKindMaintenanceA02, AreaID: 1}
	if s.VidiPlanDezurstava(vide["globalni administrator"], o, usluga) || s.VidiPlanDezurstava(vide["globalni administrator"], o, nil) {
		t.Error("plan dežurstava uz dnevnik usluge ili bez dnevnika")
	}
}

// Izvođač ne predaje smjenu sam, pa ni ne piše u dnevnik COP-a pri predaji;
// smjenu izvođača predaje uprava centra (Odluke (30. 10. 2026.)). Podaci su
// izmišljeni.
func TestSmjenuIzvodjacaPredajeUpravaCentra(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	p, prvo := "P", 1
	izvodjac := &models.User{ID: uuid.New(), FullName: "Ivo Izvođač", Duties: []models.Duty{
		{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, SectorID: &p, AreaID: &prvo, IsActive: true}}}
	ovlIzv := models.NewUserPermissions(*izvodjac)
	// izvođač je ostao dežurni iz ranijeg izdanja programa
	od := time.Now().In(models.Zagreb).Add(-2 * time.Hour)
	o.dnevnik.DezurniID, o.dnevnik.DezurniIme, o.dnevnik.DezurniOd = izvodjac.ID.String(), izvodjac.FullName, &od
	if err := o.svc.repo.SaveJournal(ctx, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PredajDezurstvo(ctx, izvodjac, ovlIzv, o.dnevnik); err == nil || !strings.Contains(err.Error(), "predaje voditelj ili zamjenik centra") {
		t.Errorf("izvođač predaje svoju smjenu: %v", err)
	}
	if err := o.svc.PredajDezurstvo(ctx, izvodjac, nil, o.dnevnik); err == nil {
		t.Error("predaja bez ovlasti prošla")
	}
	if zapisi, err := o.svc.EntriesForJournal(ctx, o.dnevnik.ID); err != nil || len(zapisi) != 0 {
		t.Errorf("predaja izvođača upisala je zapis u dnevnik COP-a: %d, %v", len(zapisi), err)
	}
	if !o.dnevnik.NetkoDezura() {
		t.Fatal("smjena izvođača je zaključena")
	}

	// kontrola: uprava centra predaje smjenu izvođača pod svojim imenom
	if err := o.svc.PredajDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik); err != nil {
		t.Fatalf("uprava centra ne predaje smjenu izvođača: %v", err)
	}
	zapisi, err := o.svc.EntriesForJournal(ctx, o.dnevnik.ID)
	if err != nil || len(zapisi) != 1 || zapisi[0].UserID != o.uprava.ID.String() || !strings.Contains(zapisi[0].Text, "Ivo Izvođač") {
		t.Errorf("zapis predaje: %+v, %v", zapisi, err)
	}
	if o.dnevnik.NetkoDezura() {
		t.Error("nakon predaje netko i dalje dežura")
	}
}
