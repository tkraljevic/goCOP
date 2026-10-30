package service_test

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Nadzor u dnevniku usluge po imeniku: rukovoditelj područja je nadzor dok
// na području nema ovlaštenika za praćenje ugovora A.02, a kad ga uprava
// upiše, nadzor je samo ovlaštenik (Odluke (30. 10. 2026.)). Podaci su
// izmišljeni.
func TestNadzorUslugePoImeniku(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "nadzor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	dnevnici := service.NewJournalService(repository.NewJournalRepository(baza, rec), nil, nil)
	dnevnici.SetImaOvlastenika(users.ImaOvlastenikaUgovora)

	sektor, prvo := "P", 1
	upisi := func(ime string, uloga models.Role) (*models.User, *models.UserPermissions) {
		t.Helper()
		u := &models.User{ID: uuid.New(), Username: ime, PasswordHash: "x", FullName: ime, IsActive: true}
		if err := userRepo.CreateUser(u, &models.Duty{Title: string(uloga), Role: uloga, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &prvo, IsPrimary: true}); err != nil {
			t.Fatal(err)
		}
		perms, err := userRepo.GetUserPermissions(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return &perms.User, perms
	}
	usluga := &models.Journal{ID: "a02", Kind: models.JournalKindMaintenanceA02, AreaID: 1}
	o := models.OpsegPodrucja(models.Area{ID: 1, SectorID: "P"})
	ruk, ovlRuk := upisi("rukovoditelj", models.RoleAreaLeader)
	if ima, err := users.ImaOvlastenikaUgovora(1, "A.02"); err != nil || ima {
		t.Fatalf("ovlaštenik prije upisa: %v, %v", ima, err)
	}
	if !dnevnici.CanSupervise(ruk, ovlRuk, o, usluga) {
		t.Error("rukovoditelj područja nije nadzor dok ovlaštenika nema")
	}
	ovl, ovlOvl := upisi("ovlastenik", models.RoleContractOfficerA2)
	if dnevnici.CanSupervise(ruk, ovlRuk, o, usluga) {
		t.Error("rukovoditelj područja je nadzor iako ovlaštenik postoji")
	}
	if !dnevnici.CanSupervise(ovl, ovlOvl, o, usluga) {
		t.Error("ovlaštenik nije nadzor")
	}
}
