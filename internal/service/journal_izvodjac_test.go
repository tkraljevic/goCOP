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
		if got := s.VidiUslugePodrucja(sl.perms, podrucje1); got != sl.prvo {
			t.Errorf("%s: dnevnik područja 1 %v, očekivano %v", sl.naziv, got, sl.prvo)
		}
		if got := s.VidiUslugePodrucja(sl.perms, podrucje2); got != sl.drugo {
			t.Errorf("%s: dnevnik područja 2 %v, očekivano %v", sl.naziv, got, sl.drugo)
		}
	}
}

// Na svom području izvođač čita samo dnevnik svoje firme: naslovnica kao
// izvođača navodi firmu s njegova računa ili njega kao voditelja usluga.
// Dnevnik druge firme i dnevnik bez izvođača na naslovnici ne čita;
// administrator i HV strana čitaju svaki. Podaci su izmišljeni.
func TestIzvodjacVidiSamoDnevnikSvojeFirme(t *testing.T) {
	s := &JournalService{}
	sektor := "P"
	p1 := 1
	podrucje1 := models.OpsegPodrucja(models.Area{ID: 1, SectorID: "P"})
	osoba := func(firma string, d models.Duty) *models.UserPermissions {
		d.IsActive, d.SectorID, d.AreaID = true, &sektor, &p1
		return models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Marko Markić", OrgName: firma, Duties: []models.Duty{d}})
	}
	poslovodja := osoba("Primjer d.o.o.", models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea})
	bezFirme := osoba("", models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea})
	administrator := osoba("Primjer d.o.o.", models.Duty{Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea})
	administrator.IsGlobalAdmin = true
	rukovoditelj := osoba("Hrvatske vode", models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea})
	svoj := &models.Journal{AreaID: 1, Contractor: "PRIMJER d.o.o., Primjerska 1"}
	vodi := &models.Journal{AreaID: 1, Contractor: "Treći radovi d.o.o.", ContractorLead: "Marko Markić"}
	drugi := &models.Journal{AreaID: 1, Contractor: "Drugi radovi d.o.o."}
	prazan := &models.Journal{AreaID: 1}
	slucajevi := []struct {
		naziv                     string
		perms                     *models.UserPermissions
		svoj, vodi, drugi, prazan bool
	}{
		{"poslovođa firme Primjer", poslovodja, true, true, false, false},
		{"poslovođa bez firme na računu", bezFirme, false, true, false, false},
		{"poslovođa s administracijom", administrator, true, true, true, true},
		{"rukovoditelj područja", rukovoditelj, true, true, true, true},
		{"bez ovlasti", nil, false, false, false, false},
	}
	for _, sl := range slucajevi {
		for _, x := range []struct {
			j    *models.Journal
			vidi bool
		}{{svoj, sl.svoj}, {vodi, sl.vodi}, {drugi, sl.drugi}, {prazan, sl.prazan}} {
			if got := s.VidiDnevnikUsluge(sl.perms, podrucje1, x.j); got != x.vidi {
				t.Errorf("%s: dnevnik izvođača %q (voditelj %q) %v, očekivano %v", sl.naziv, x.j.Contractor, x.j.ContractorLead, got, x.vidi)
			}
		}
	}
	// svoj dnevnik na tuđem području ne čita; bez dnevnika nema čitanja
	if s.VidiDnevnikUsluge(poslovodja, models.OpsegPodrucja(models.Area{ID: 2, SectorID: "P"}), svoj) {
		t.Error("poslovođa čita dnevnik svoje firme na području bez dužnosti")
	}
	if s.VidiDnevnikUsluge(poslovodja, podrucje1, nil) {
		t.Error("poslovođa čita nepoznat dnevnik")
	}
}
