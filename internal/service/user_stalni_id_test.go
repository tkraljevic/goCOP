package service_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// praznaUprava je čvor bez sjemena i uprava organizacije koja na njemu
// otvara račune; ne ovisi o imeniku izvan repozitorija
func praznaUprava(t *testing.T) (*sql.DB, *service.UserService, *repository.UserRepository, *models.UserPermissions) {
	t.Helper()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(database, ledger.New(database, "cvor-a"))
	auth := service.NewAuthService(repo, repository.NewSessionRepository(database))
	// uprava je stvaran račun: zaduženje pamti tko ga je dodijelio
	glavni := &models.User{Username: "uprava", PasswordHash: "x", FullName: "Uprava", IsGlobalAdmin: true, IsActive: true}
	if err := repo.CreateUser(glavni, nil); err != nil {
		t.Fatal(err)
	}
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *glavni}
	return database, service.NewUserService(repo, auth, service.NewSSEBroker()), repo, uprava
}

func otvoriRacun(t *testing.T, s *service.UserService, uprava *models.UserPermissions, username string) *models.User {
	t.Helper()
	u, err := s.CreateUser(uprava, service.CreateUserRequest{
		Username: username, Password: "privremena", FullName: "Pero Perić",
		OrgType: models.OrgHrvatskeVode,
	})
	if err != nil {
		t.Fatalf("otvaranje računa %q: %v", username, err)
	}
	return u
}

// Novi korisnik dobije isti identifikator koji bi mu dao seed — iz
// korisničkog imena, bez rubnih razmaka — pa ga ništa poslije ne prekodira
func TestNoviKorisnikDobijeStalniIdentitet(t *testing.T) {
	_, s, _, uprava := praznaUprava(t)

	u := otvoriRacun(t, s, uprava, "  pperic ")
	if want := db.StableID("user", "pperic"); u.ID != want {
		t.Fatalf("identitet %s, očekivan stalni %s", u.ID, want)
	}
}

// Stalni identitet koji je već nečiji ne smije se dati novoj osobi: ni
// obrisanog računa (povijest mu ostaje u knjizi verzija), ni računa koji je
// s tog imena preimenovan. Tada novi korisnik dobije nasumični.
func TestStalniIdentitetSeNePosuduje(t *testing.T) {
	database, s, _, uprava := praznaUprava(t)
	stalni := db.StableID("user", "pperic")

	// obrisani račun: redak nestane, povijest ostane
	prvi := otvoriRacun(t, s, uprava, "pperic")
	if err := s.DeleteUser(uprava, prvi.ID); err != nil {
		t.Fatal(err)
	}
	var verzija int
	if err := database.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = 'users' AND entity_id = ?`, stalni.String()).Scan(&verzija); err != nil || verzija == 0 {
		t.Fatalf("obrisani račun nema povijest u knjizi (%d, %v) — test ne mjeri ništa", verzija, err)
	}
	drugi := otvoriRacun(t, s, uprava, "pperic")
	if drugi.ID == stalni {
		t.Fatal("novi račun je preuzeo identitet obrisanog i njegovu povijest")
	}
	if drugi.ID.Version() != 7 {
		t.Errorf("očekivan nasumični UUID v7, dobiven v%d", drugi.ID.Version())
	}

	// preimenovani račun: redak sa stalnim identitetom i dalje postoji
	_, s, _, uprava = praznaUprava(t)
	treci := otvoriRacun(t, s, uprava, "pperic")
	if _, err := s.UpdateUser(uprava, service.UpdateUserRequest{
		ID: treci.ID, Username: "pperic2", FullName: treci.FullName, OrgType: treci.OrgType, IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}
	cetvrti := otvoriRacun(t, s, uprava, "pperic")
	if cetvrti.ID == treci.ID {
		t.Fatal("novi račun dobio je identitet preimenovanog")
	}
	if cetvrti.ID.Version() != 7 {
		t.Errorf("očekivan nasumični UUID v7, dobiven v%d", cetvrti.ID.Version())
	}
}

// Preimenovani korisnik zadržava svoj identifikator i nakon ponovnog
// pokretanja: migracija ga više ne izvodi iz novog korisničkog imena
func TestPreimenovaniKorisnikZadrzavaIdentitet(t *testing.T) {
	database, s, repo, uprava := praznaUprava(t)

	u := otvoriRacun(t, s, uprava, "pperic")
	if err := s.AddDuty(uprava, service.AddDutyRequest{UserID: u.ID, Role: models.RoleGlobalAdmin}); err != nil {
		t.Fatal(err)
	}
	var zaduzenje string
	if err := database.QueryRow(`SELECT id FROM duties WHERE user_id = ?`, u.ID.String()).Scan(&zaduzenje); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateUser(uprava, service.UpdateUserRequest{
		ID: u.ID, Username: "pperic.novi", FullName: u.FullName, OrgType: u.OrgType, IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(database); err != nil {
		t.Fatalf("ponovno pokretanje: %v", err)
	}

	po, err := repo.GetUserByUsername("pperic.novi")
	if err != nil || po == nil {
		t.Fatalf("preimenovani korisnik nije pronađen: %v", err)
	}
	if po.ID != u.ID {
		t.Fatalf("preimenovani korisnik promijenio je identitet: %s → %s", u.ID, po.ID)
	}
	if len(po.Duties) != 1 || po.Duties[0].ID.String() != zaduzenje {
		t.Errorf("zaduženje nakon pokretanja: %+v, očekivano jedno s identitetom %s", po.Duties, zaduzenje)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = 'users' AND entity_id <> ? AND payload LIKE '%pperic%'`,
		u.ID.String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d verzija korisnika pod drugim identitetom nakon pokretanja", n)
	}
}
