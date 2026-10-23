package repository

// Globalni administratori za obavijesti o roku članstva u mreži (G9b):
// aktivni, računom ili dužnošću, a ne i onaj kojem je račun isključen.

import (
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

func TestGlobalniAdministratori(t *testing.T) {
	baza := bazaRacuna(t)
	users := NewUserRepository(baza, ledger.New(baza, "cop-osijek"))
	if a, err := users.GlobalniAdministratori(); err != nil || len(a) != 0 {
		t.Fatalf("prazna baza: %v %v", a, err)
	}
	for _, u := range []*models.User{
		{Username: "uprava", FullName: "Ana Anić", Email: "ana@voda.hr", IsActive: true, IsGlobalAdmin: true, OrgType: models.OrgHrvatskeVode},
		{Username: "umirovljen", FullName: "Branko Brkić", Email: "branko@voda.hr", IsActive: true, IsGlobalAdmin: true, OrgType: models.OrgHrvatskeVode},
		{Username: "dezurni", FullName: "Ivo Ivić", Email: "ivo@voda.hr", IsActive: true, OrgType: models.OrgHrvatskeVode},
	} {
		if err := users.CreateUser(u, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := baza.Exec(`UPDATE users SET is_active = 0 WHERE username = 'umirovljen'`); err != nil {
		t.Fatal(err)
	}
	a, err := users.GlobalniAdministratori()
	if err != nil || len(a) != 1 || a[0].Username != "uprava" {
		t.Fatalf("administratori %+v %v", a, err)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := users.GlobalniAdministratori(); err == nil {
		t.Error("popis iz zatvorene baze")
	}
}
