package repository

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Na području se broji samo važeća dužnost tražene uloge s uključenim
// računom: istekla dužnost, opozvana dužnost, isključen račun, druga uloga i
// drugo područje ne broje se. Podaci su izmišljeni.
func TestImaVazeceZaduzenjeNaPodrucju(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	if _, err := n.db.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (17, 'B', 'BP 17', 'VGI')`); err != nil {
		t.Fatal(err)
	}
	sektor := "B"
	upisi := func(ime string, uloga models.Role, podrucje int, istek *time.Time) *models.User {
		t.Helper()
		u := &models.User{Username: ime, PasswordHash: "x", FullName: ime, OrgType: models.OrgHrvatskeVode, IsActive: true}
		nuzno(t, repo.CreateUser(u, &models.Duty{Title: string(uloga), Role: uloga, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, ExpiresAt: istek}))
		return u
	}
	a2 := models.UlogeUgovora("A.02")
	ima := func(podrucje int, uloge []models.Role) bool {
		t.Helper()
		da, err := repo.ImaVazeceZaduzenjeNaPodrucju(podrucje, uloge)
		nuzno(t, err)
		return da
	}

	proslo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	upisi("istekao", models.RoleContractOfficerA2, 16, &proslo)
	iskljucen := upisi("iskljucen", models.RoleContractOfficerA2, 16, nil)
	if _, err := n.db.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, iskljucen.ID.String()); err != nil {
		t.Fatal(err)
	}
	opozvan := upisi("opozvan", models.RoleContractDeputyA2, 16, nil)
	duznosti, err := repo.GetDutiesForUser(opozvan.ID)
	nuzno(t, err)
	nuzno(t, repo.RevokeDuty(duznosti[0].ID))
	upisi("drugi program", models.RoleContractOfficerA3, 16, nil)
	upisi("drugo podrucje", models.RoleContractOfficerA2, 17, nil)
	if ima(16, a2) {
		t.Error("na području 16 broji se ovlaštenik A.02 koji ne vrijedi")
	}
	if ima(0, a2) || ima(16, nil) {
		t.Error("bez područja ili bez uloga netko se broji")
	}

	// kontrola: drugi program i drugo područje imaju svog, a zamjenik s
	// rokom u budućnosti vrijedi
	if !ima(16, models.UlogeUgovora("A.03")) || !ima(17, a2) {
		t.Error("važeći ovlaštenik nije prepoznat")
	}
	buduce := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	upisi("zamjenik", models.RoleContractDeputyA2, 16, &buduce)
	if !ima(16, a2) {
		t.Error("zamjenik ovlaštenika s rokom u budućnosti nije prepoznat")
	}
}

// Rok koji se ne da pročitati i baza koja ne odgovara javljaju grešku, a ne
// „nema ovlaštenika”. Podaci su izmišljeni.
func TestImaVazeceZaduzenjeNaPodrucjuGreske(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	sektor, podrucje := "B", 16
	u := &models.User{Username: "ovlastenik", PasswordHash: "x", FullName: "Ovlaštenik", OrgType: models.OrgHrvatskeVode, IsActive: true}
	nuzno(t, repo.CreateUser(u, &models.Duty{Title: "Ovlaštenik", Role: models.RoleContractOfficerA2, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje}))
	a2 := models.UlogeUgovora("A.02")
	// kontrola: ovlaštenik bez roka vrijedi
	if da, err := repo.ImaVazeceZaduzenjeNaPodrucju(podrucje, a2); err != nil || !da {
		t.Fatalf("ovlaštenik bez roka: %v, %v", da, err)
	}
	if _, err := n.db.Exec(`UPDATE duties SET expires_at = 'nije datum' WHERE user_id = ?`, u.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ImaVazeceZaduzenjeNaPodrucju(podrucje, a2); err == nil {
		t.Error("nečitljiv rok nije javljen kao greška")
	}
	nuzno(t, n.db.Close())
	if _, err := repo.ImaVazeceZaduzenjeNaPodrucju(podrucje, a2); err == nil {
		t.Error("zatvorena baza nije javljena kao greška")
	}
}
