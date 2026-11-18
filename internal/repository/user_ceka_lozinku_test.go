package repository

import (
	"testing"

	"gocop/internal/models"
)

// Filtar „Čeka promjenu lozinke” daje račune koji pri sljedećoj prijavi
// moraju promijeniti lozinku, i isključene (kartica Djelatnici broji sve)
func TestPopisCekaPromjenuLozinke(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	ana := novaOsoba(t, repo, "aanic", "Ana Anić", nil)
	ivo := novaOsoba(t, repo, "iivic", "Ivo Ivić", func(u *models.User) { u.IsActive = false })
	novaOsoba(t, repo, "llukic", "Luka Lukić", nil)
	if _, err := n.db.Exec(`UPDATE users SET must_change_password = CASE WHEN id IN (?, ?) THEN 1 ELSE 0 END`, ana.ID.String(), ivo.ID.String()); err != nil {
		t.Fatal(err)
	}
	popis, err := repo.ListUsers("", 0, "", "", string(StanjeCekaLozinku))
	if err != nil {
		t.Fatal(err)
	}
	var imena []string
	for _, u := range popis {
		imena = append(imena, u.FullName)
	}
	if len(imena) != 2 || imena[0] != "Ana Anić" || imena[1] != "Ivo Ivić" {
		t.Errorf("čekaju promjenu lozinke: %q", imena)
	}
}
