package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// opozivBezBaze je drugi korak prijave čiji opoziv ne uspije
type opozivBezBaze struct{ pozvan []uuid.UUID }

func (o *opozivBezBaze) Opozovi(_ context.Context, userID uuid.UUID) error {
	o.pozvan = append(o.pozvan, userID)
	return errors.New("baza drugog koraka nije dostupna")
}
func (o *opozivBezBaze) DopustenaAdresa(context.Context, string) error { return nil }
func (o *opozivBezBaze) JaviPromjenuAdrese(context.Context, *models.User, string, string) {
}

// Promjena lozinke traži točnu trenutnu lozinku i novu od barem 6 znakova,
// različitu od trenutne, koja stane u bcrypt (do 72 bajta). Odbijena ili
// neupisana promjena ne mijenja lozinku i ne gasi prijave. Upisana promjena
// gasi sve prijave osim one iz koje se mijenja; ako gašenje prijava ili
// opoziv zapamćenih računala ne uspije, javlja se da je lozinka ipak
// promijenjena.
func TestPromjenaLozinkeSvaPravila(t *testing.T) {
	const stara = "anina-lozinka"
	for _, x := range []struct {
		opis      string
		pripremi  func(t *testing.T, c *prazanCvorPrijave)
		trenutna  string
		nova      string
		tko       func(ana *models.User) uuid.UUID
		greska    string // početak poruke; prazno = uspjeh
		krivaLoz  bool   // greška je ErrKrivaLozinka
		upisana   bool   // nova lozinka vrijedi
		odjavljen bool   // druga prijava je ugašena
	}{
		{opis: "kriva trenutna lozinka", trenutna: "kriva", nova: "nova-lozinka",
			greska: "trenutna lozinka nije točna", krivaLoz: true},
		{opis: "nepostojeća osoba", trenutna: stara, nova: "nova-lozinka",
			tko: func(*models.User) uuid.UUID { return uuid.New() }, greska: "korisnik nije pronađen"},
		{opis: "prekratka nova lozinka", trenutna: stara, nova: "12345",
			greska: "nova lozinka mora imati najmanje 6 znakova"},
		{opis: "nova jednaka trenutnoj", trenutna: stara, nova: stara,
			greska: "nova lozinka mora se razlikovati od trenutne"},
		{opis: "nova dulja od 72 bajta", trenutna: stara, nova: strings.Repeat("ž", 37),
			greska: "greška pri hashiranju"},
		{opis: "upis lozinke ne uspije", trenutna: stara, nova: "nova-lozinka",
			pripremi: func(t *testing.T, c *prazanCvorPrijave) {
				if _, err := c.db.Exec(`CREATE TRIGGER lozinka_zakljucana BEFORE UPDATE OF password_hash ON users
					BEGIN SELECT RAISE(ABORT, 'lozinka zaključana'); END`); err != nil {
					t.Fatal(err)
				}
			}, greska: "greška pri ažuriranju lozinke u bazi"},
		{opis: "ostale prijave se ne daju ugasiti", trenutna: stara, nova: "nova-lozinka",
			pripremi: func(t *testing.T, c *prazanCvorPrijave) {
				if _, err := c.db.Exec(`CREATE TRIGGER prijave_zakljucane BEFORE DELETE ON sessions
					BEGIN SELECT RAISE(ABORT, 'prijave zaključane'); END`); err != nil {
					t.Fatal(err)
				}
			}, greska: "lozinka je promijenjena, ali ostale prijave nisu odjavljene", upisana: true},
		{opis: "opoziv zapamćenih računala ne uspije", trenutna: stara, nova: "nova-lozinka",
			pripremi: func(t *testing.T, c *prazanCvorPrijave) { c.auth.SetZastitaPrijave(&opozivBezBaze{}) },
			greska:   "lozinka je promijenjena, ali zapamćena računala nisu zaboravljena: baza drugog koraka nije dostupna",
			upisana:  true, odjavljen: true},
		{opis: "uspjeh", trenutna: stara, nova: "nova-lozinka", upisana: true, odjavljen: true},
	} {
		t.Run(x.opis, func(t *testing.T) {
			c := noviCvorPrijave(t)
			ana := c.racun(t, "aanic", stara, true)
			ova, err := c.auth.OtvoriSesiju(ana, "192.168.1.5", "Firefox")
			if err != nil {
				t.Fatal(err)
			}
			druga, err := c.auth.OtvoriSesiju(ana, "192.168.1.6", "Chrome")
			if err != nil {
				t.Fatal(err)
			}
			if x.pripremi != nil {
				x.pripremi(t, c)
			}
			id := ana.ID
			if x.tko != nil {
				id = x.tko(ana)
			}

			err = c.auth.ChangePassword(id, x.trenutna, x.nova, ova.ID)
			switch {
			case x.greska == "" && err != nil:
				t.Fatalf("promjena: %v", err)
			case x.greska != "" && (err == nil || !strings.HasPrefix(err.Error(), x.greska)):
				t.Fatalf("greška %v, želi %q", err, x.greska)
			}
			if errors.Is(err, service.ErrKrivaLozinka) != x.krivaLoz {
				t.Errorf("ErrKrivaLozinka: %v", err)
			}

			vrijedi := x.nova
			if !x.upisana {
				vrijedi = stara
			}
			if err := c.auth.ProvjeriLozinku(ana.ID, vrijedi); err != nil {
				t.Errorf("lozinka %q ne vrijedi: %v", vrijedi, err)
			}
			if _, _, err := c.auth.AuthenticateSession(ova.ID); err != nil {
				t.Errorf("prijava iz koje se mijenja ugašena: %v", err)
			}
			_, _, err = c.auth.AuthenticateSession(druga.ID)
			if x.odjavljen != errors.Is(err, service.ErrSessionExpired) {
				t.Errorf("druga prijava: %v, želi ugašena=%v", err, x.odjavljen)
			}
		})
	}
}

// Pregled tuđim očima pokreće samo globalni administrator i samo za
// postojećeg djelatnika; pogled vlastitim očima gasi pregled. Odbijen
// pregled ne mijenja sesiju.
func TestPocetakPregledaTudjimOcima(t *testing.T) {
	c := noviCvorPrijave(t)
	uprava := c.racun(t, "uprava", "lozinka-uprave", true)
	ana := c.racun(t, "aanic", "anina-lozinka", true)
	sesija, err := c.auth.OtvoriSesiju(uprava, "", "")
	if err != nil {
		t.Fatal(err)
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true, User: *uprava}
	gleda := func() *uuid.UUID {
		t.Helper()
		s, err := c.sessions.GetSession(sesija.ID)
		if err != nil || s == nil {
			t.Fatalf("sesija: %+v %v", s, err)
		}
		return s.ViewingAs
	}

	if err := c.auth.StartViewingAs(sesija.ID, nil, ana.ID); err == nil {
		t.Error("pregled bez ovlasti")
	}
	if err := c.auth.StartViewingAs(sesija.ID, admin, uuid.New()); err == nil || err.Error() != "djelatnik nije pronađen" {
		t.Errorf("nepostojeći djelatnik: %v", err)
	}
	if g := gleda(); g != nil {
		t.Fatalf("odbijen pregled je pokrenut: %v", g)
	}
	if err := c.auth.StartViewingAs(sesija.ID, admin, ana.ID); err != nil {
		t.Fatal(err)
	}
	if g := gleda(); g == nil || *g != ana.ID {
		t.Fatalf("pregled Aninim očima: %v", g)
	}
	if err := c.auth.StartViewingAs(sesija.ID, admin, uprava.ID); err != nil {
		t.Fatal(err)
	}
	if g := gleda(); g != nil {
		t.Errorf("vlastite oči ne gase pregled: %v", g)
	}
	if _, err := c.db.Exec(`ALTER TABLE users RENAME TO users_nema`); err != nil {
		t.Fatal(err)
	}
	if err := c.auth.StartViewingAs(sesija.ID, admin, ana.ID); err == nil {
		t.Error("greška čitanja djelatnika nije vraćena")
	}
}
