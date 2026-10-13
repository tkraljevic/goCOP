package repository

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// imenaDjelatnika su imena s popisa djelatnika, redom popisa
func imenaDjelatnika(popis []models.User) []string {
	out := []string{}
	for _, u := range popis {
		out = append(out, u.FullName)
	}
	return out
}

// Popis djelatnika po filtru: sektor (Direkcija i po organizaciji i po
// ulozi državne razine), područje, uloga (globalni administrator i po
// zastavici), stanje računa, računi bez adrese za PIN i pretraga po
// tekstu. Filtri se zbrajaju, a popis ide po imenu.
func TestPopisDjelatnikaPoFiltru(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	adresa := func(posta string) func(*models.User) { return func(u *models.User) { u.Email = posta } }
	ana := novaOsoba(t, repo, "aanic", "Ana Anić", func(u *models.User) { u.Email, u.Title = "ana.anic@voda.hr", "Inženjerka" })
	novaOsoba(t, repo, "iivic", "Ivo Ivić", adresa(""))
	novaOsoba(t, repo, "mmarkic", "Marko Markić", func(u *models.User) { u.Email, u.IsActive = "marko.markic@voda.hr", false })
	petra := novaOsoba(t, repo, "pperic", "Petra Perić", func(u *models.User) { u.Email, u.OrgName = "petra.peric@primjer.hr", "Direkcija" })
	novaOsoba(t, repo, "llukic", "Luka Lukić", func(u *models.User) {
		u.Email, u.IsGlobalAdmin, u.PINAdresaPotvrdena = "luka.lukic@primjer.hr", true, "luka.lukic@primjer.hr"
	})
	novaOsoba(t, repo, "mmikic", "Mia Mikić", adresa("dezurni@voda.hr"))
	nora := novaOsoba(t, repo, "nnoric", "Nora Norić", adresa("Dezurni@voda.hr "))
	nuzno(t, repo.AddDuty(vodocuvarDuznost(ana.ID, osnovaDionica)))
	nuzno(t, repo.AddDuty(&models.Duty{UserID: nora.ID, Title: "Glavna rukovoditeljica", Role: models.RoleNationalLeader, ScopeType: models.ScopeAll}))
	kad := time.Date(2026, 3, 10, 7, 0, 0, 0, time.UTC)
	nuzno(t, repo.MarkLogin(ana.ID, kad))
	nuzno(t, repo.MarkLogin(petra.ID, kad))

	for _, slucaj := range []struct {
		naziv                        string
		sektor                       string
		podrucje                     int
		uloga, trazi, stanje, domena string
		ocek                         []string
	}{
		{naziv: "sektor", sektor: "B", ocek: []string{"Ana Anić"}},
		{naziv: "Direkcija", sektor: "DIREKCIJA", ocek: []string{"Nora Norić", "Petra Perić"}},
		{naziv: "područje", podrucje: 16, ocek: []string{"Ana Anić"}},
		{naziv: "globalni administrator", uloga: "GLOBAL_ADMIN", ocek: []string{"Luka Lukić"}},
		{naziv: "uloga", uloga: string(models.RoleWaterGuard), ocek: []string{"Ana Anić"}},
		{naziv: "isključeni", stanje: string(models.AccountDisabled), ocek: []string{"Marko Markić"}},
		{naziv: "čekaju prvu prijavu", stanje: string(models.AccountPending),
			ocek: []string{"Ivo Ivić", "Luka Lukić", "Mia Mikić", "Nora Norić", "Proba Probić"}},
		{naziv: "aktivni", stanje: string(models.AccountActive), ocek: []string{"Ana Anić", "Petra Perić"}},
		{naziv: "bez adrese za PIN, s domenom", stanje: string(StanjeBezEposte), domena: " VODA.hr ",
			ocek: []string{"Ivo Ivić", "Mia Mikić", "Nora Norić", "Petra Perić", "Proba Probić"}},
		{naziv: "bez adrese za PIN, bez domene", stanje: string(StanjeBezEposte),
			ocek: []string{"Ivo Ivić", "Mia Mikić", "Nora Norić", "Proba Probić"}},
		{naziv: "pretraga po imenu", trazi: "lukić", ocek: []string{"Luka Lukić"}},
		{naziv: "pretraga po adresi", trazi: "primjer.hr", ocek: []string{"Luka Lukić", "Petra Perić"}},
		{naziv: "pretraga po dionici dužnosti", trazi: osnovaDionica, ocek: []string{"Ana Anić"}},
		{naziv: "pretraga po zvanju", trazi: "inženj", ocek: []string{"Ana Anić"}},
		{naziv: "prazna pretraga", trazi: "   ",
			ocek: []string{"Ana Anić", "Ivo Ivić", "Luka Lukić", "Marko Markić", "Mia Mikić", "Nora Norić", "Petra Perić", "Proba Probić"}},
		{naziv: "sektor i stanje", sektor: "B", stanje: string(models.AccountPending), ocek: []string{}},
	} {
		popis, err := repo.ListUsersDomena(slucaj.sektor, slucaj.podrucje, slucaj.uloga, slucaj.trazi, slucaj.stanje, slucaj.domena)
		if err != nil {
			t.Fatalf("%s: %v", slucaj.naziv, err)
		}
		if dobiveno := imenaDjelatnika(popis); !reflect.DeepEqual(dobiveno, slucaj.ocek) {
			t.Errorf("%s: %v, očekivano %v", slucaj.naziv, dobiveno, slucaj.ocek)
		}
	}

	popis, err := repo.ListUsersDomena("", 16, "", "", "", "")
	nuzno(t, err)
	if len(popis) != 1 || len(popis[0].Duties) != 1 || popis[0].Duties[0].Role != models.RoleWaterGuard {
		t.Errorf("djelatnik s dužnostima: %+v", popis)
	}
}

// Greška baze ne prolazi tiho: popis s nečitljivim računom i popis iz
// zatvorene baze vraćaju grešku, a ne skraćen popis.
func TestPopisDjelatnikaGreskeBaze(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	novaOsoba(t, repo, "aanic", "Ana Anić", nil)
	if _, err := n.db.Exec(`UPDATE users SET created_at = 'nije vrijeme' WHERE username = 'aanic'`); err != nil {
		t.Fatal(err)
	}
	if popis, err := repo.ListUsers("", 0, "", "", ""); err == nil || !strings.Contains(err.Error(), "greška pri čitanju reda") {
		t.Errorf("popis s nečitljivim računom: %v %v", imenaDjelatnika(popis), err)
	}
	n.db.Close()
	if _, err := repo.ListUsers("", 0, "", "", ""); err == nil || !strings.Contains(err.Error(), "greška pri listanju korisnika") {
		t.Errorf("popis iz zatvorene baze: %v", err)
	}
}
