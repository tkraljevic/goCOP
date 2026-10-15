package service_test

import (
	"encoding/json"
	"os"
	"testing"

	"gocop/internal/db"
	"gocop/internal/models"
)

// imenaIzmisljenihOsoba su korisnička imena osoba izmišljenog skupa
// (testdata/sektor-p/imenik.json)
func imenaIzmisljenihOsoba(t *testing.T) []string {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	raw, err := os.ReadFile(db.ImenikPath)
	if err != nil {
		t.Fatal(err)
	}
	var osobe []struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &osobe); err != nil {
		t.Fatal(err)
	}
	imena := make([]string, 0, len(osobe))
	for _, o := range osobe {
		imena = append(imena, o.Username)
	}
	if len(imena) == 0 {
		t.Fatal("izmišljeni skup nema nijednu osobu")
	}
	return imena
}

// Okolina oporavka je podloga i drugim provjerama (uprava sektora, ista
// razina), a one na nju dodaju izmišljene osobe pod imenima iz
// testdata/sektor-p. Zato ni glavni administrator okoline ne nosi ime neke od
// tih osoba: drugi račun istog imena pao bi na jedinstvenosti imena čim se
// grane spoje (Odluke (8. 10. 2026.), pitanje 13).
func TestOkolinaOporavkaOstavljaImenaIzmisljenihOsoba(t *testing.T) {
	o := novaOkolinaOporavka(t)
	for _, ime := range imenaIzmisljenihOsoba(t) {
		u := &models.User{Username: ime, PasswordHash: "x", FullName: ime, IsActive: true, OrgType: models.OrgHrvatskeVode}
		if err := o.repo.CreateUser(u, nil); err != nil {
			t.Errorf("izmišljena osoba %s ne može se dodati na okolinu oporavka: %v", ime, err)
		}
	}
}
