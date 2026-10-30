package service

import (
	"testing"

	"gocop/internal/models"
)

// Dnevnik sektorskog COP-a prima upis od svakoga tko vodi sektor ili bilo
// koje područje u njemu: obrana proglašena na više područja vodi se iz
// sektora, a dežurni s područja pišu u isti dnevnik. Pravo se zato računa
// iz dosega, ne iz jednog područja — iz područja ga za COP nitko ne bi imao.
func TestPravoPisanjaUDnevnikCOPaIzDosega(t *testing.T) {
	podrucja := []models.Area{{ID: 15, SectorID: "B"}, {ID: 20, SectorID: "B"}, {ID: 3, SectorID: "A"}}
	sektorski := models.OpsegDnevnika(models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B"}, nil, podrucja)
	p20 := 20
	podrucni := models.OpsegDnevnika(models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", CentarPodrucje: &p20}, nil, podrucja)

	s := &JournalService{}
	vodiPodrucje := func(id int) *models.UserPermissions {
		return &models.UserPermissions{AllowedAreas: map[int]bool{id: true}}
	}
	vodiSektor := &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}}

	tests := []struct {
		tko   string
		perms *models.UserPermissions
		o     models.Opseg
		want  bool
	}{
		{"voditelj sektora B u sektorskom", vodiSektor, sektorski, true},
		{"voditelj područja 15 (B) u sektorskom", vodiPodrucje(15), sektorski, true},
		{"voditelj područja 3 (A) u sektorskom B", vodiPodrucje(3), sektorski, false},
		{"voditelj sektora B u područnom B.20", vodiSektor, podrucni, true},
		{"voditelj područja 20 u područnom B.20", vodiPodrucje(20), podrucni, true},
		{"voditelj područja 15 u područnom B.20", vodiPodrucje(15), podrucni, false},
		{"bez ovlasti", &models.UserPermissions{}, sektorski, false},
		{"prazan doseg", vodiSektor, models.Opseg{}, false},
	}
	for _, tc := range tests {
		if got := s.CanWrite(tc.perms, tc.o); got != tc.want {
			t.Errorf("%s: CanWrite = %v, želi %v", tc.tko, got, tc.want)
		}
	}

	// U dnevnik COP-a idu vrste dežurstva, ne građevinske.
	cop := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B"}
	vrste := s.AllowedKinds(&models.User{}, vodiSektor, sektorski, cop)
	if len(vrste) != len(models.EntryKindsCOP) || vrste[0] != models.EntryKindReport {
		t.Errorf("vrste u dnevniku COP-a = %v", vrste)
	}
	// U dnevniku usluge nalog i ocjenu piše samo nadzor, ovlaštenik za
	// praćenje ugovora (Odluke (30. 10. 2026.), 4); ostali rad i napomenu.
	usluga := models.OpsegPodrucja(podrucja[0])
	a02 := &models.Journal{Kind: models.JournalKindMaintenanceA02, AreaID: 15}
	if v := s.AllowedKinds(&models.User{}, vodiPodrucje(15), usluga, a02); len(v) != 2 || v[0] != models.EntryKindWork || v[1] != models.EntryKindNote {
		t.Errorf("vrste u dnevniku usluge bez nadzora = %v", v)
	}
	podrucje := 15
	ovlastenik := models.NewUserPermissions(models.User{Duties: []models.Duty{
		{Role: models.RoleContractOfficerA2, ScopeType: models.ScopeArea, AreaID: &podrucje, IsActive: true}}})
	if v := s.AllowedKinds(&ovlastenik.User, ovlastenik, usluga, a02); len(v) != len(models.EntryKinds) {
		t.Errorf("vrste u dnevniku usluge za ovlaštenika = %v", v)
	}
}
