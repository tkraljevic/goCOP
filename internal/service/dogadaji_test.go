package service_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Događaji uživo javljaju da se nešto promijenilo, a ne podatke osobe: tok
// je bio otvoren bez prijave, a s izmjenom djelatnika slao mu je telefon i
// e-poštu.
func TestDogadajiNeNoseOsobnePodatke(t *testing.T) {
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(database, ledger.New(database, "cvor-a"))
	glavni := &models.User{Username: "uprava", PasswordHash: "x", FullName: "Uprava", IsGlobalAdmin: true, IsActive: true}
	if err := repo.CreateUser(glavni, nil); err != nil {
		t.Fatal(err)
	}
	broker := service.NewSSEBroker()
	s := service.NewUserService(repo, service.NewAuthService(repo, repository.NewSessionRepository(database)), broker)
	tok := broker.Subscribe()
	defer broker.Unsubscribe(tok)

	if _, err := s.CreateUser(&models.UserPermissions{IsGlobalAdmin: true, User: *glavni}, service.CreateUserRequest{
		Username: "pperic", Password: "privremena", FullName: "Pero Perić", OrgType: models.OrgHrvatskeVode,
		Email: "pero.peric@voda.hr", MobilePhone: "099-123-4567", Phone: "031-111-222",
	}); err != nil {
		t.Fatal(err)
	}
	var poslano []string
	for {
		select {
		case m := <-tok:
			poslano = append(poslano, m)
			continue
		case <-time.After(100 * time.Millisecond):
		}
		break
	}
	if len(poslano) == 0 {
		t.Fatal("stvaranje djelatnika nije javljeno")
	}
	for _, m := range poslano {
		for _, osobno := range []string{"pero.peric@voda.hr", "099-123-4567", "031-111-222", "password"} {
			if strings.Contains(m, osobno) {
				t.Errorf("događaj nosi osobni podatak %q: %s", osobno, m)
			}
		}
	}
}
