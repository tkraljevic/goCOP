package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Sesija pamti otisak lozinke računa kad je otvorena, a osoba koja sama
// promijeni lozinku zadržava svoju sesiju uz otisak nove. Lozinka koja stigne
// s drugog računala ne mijenja otisak sesija ovdje.
func TestSesijaPamtiOtisakLozinke(t *testing.T) {
	a, b := noviCvor(t, "ured"), noviCvor(t, "javni")
	u := &models.User{Username: "ivo", PasswordHash: "$2a$stari", FullName: "Ivo Ivić",
		OrgType: models.OrgPravnaOsoba, IsActive: true}
	repoA := NewUserRepository(a.db, a.rec)
	nuzno(t, repoA.CreateUser(u, nil))
	posalji(t, a, b)

	sesA, sesB := NewSessionRepository(a.db), NewSessionRepository(b.db)
	sa := &models.Session{UserID: u.ID, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	sb := &models.Session{UserID: u.ID, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	nuzno(t, sesA.CreateSession(sa))
	nuzno(t, sesB.CreateSession(sb))
	if sb.LozinkaOtisak != OtisakLozinke("$2a$stari") {
		t.Fatalf("otisak nove sesije: %q", sb.LozinkaOtisak)
	}

	nuzno(t, repoA.ChangePassword(u.ID, "$2a$novi"))
	nuzno(t, sesA.ZadrziUzNovuLozinku(sa.ID, u.ID, "$2a$novi"))
	g, err := sesA.GetSession(sa.ID)
	nuzno(t, err)
	if g == nil || g.LozinkaOtisak != OtisakLozinke("$2a$novi") {
		t.Fatalf("zadržana sesija: %+v", g)
	}
	posalji(t, a, b)
	g, err = sesB.GetSession(sb.ID)
	nuzno(t, err)
	if naB := korisnikNa(t, b, u.ID); naB.PasswordHash != "$2a$novi" || g == nil || g.LozinkaOtisak == OtisakLozinke(naB.PasswordHash) {
		t.Fatalf("na drugom računalu sesija mora ostati uz staru lozinku: %+v", g)
	}

	// tuđa sesija se ne prenosi na novu lozinku, a račun bez lozinke ima otisak
	nuzno(t, sesA.ZadrziUzNovuLozinku(sb.ID, uuid.New(), "$2a$novi"))
	bez := &models.Session{UserID: uuid.New(), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := sesA.CreateSession(bez); err == nil && bez.LozinkaOtisak != OtisakLozinke("") {
		t.Errorf("sesija računa kojeg nema: %q", bez.LozinkaOtisak)
	}
}

// Greške baze javljaju se, a ne otvaraju sesiju bez otiska
func TestSesijaOtisakGreske(t *testing.T) {
	c := noviCvor(t, "ured")
	s := NewSessionRepository(c.db)
	_, err := c.db.Exec(`CREATE TRIGGER sesije_zakljucane BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT, 'zaključano'); END`)
	nuzno(t, err)
	ses := &models.Session{UserID: osnovaKorisnik, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	nuzno(t, s.CreateSession(ses))
	if err := s.ZadrziUzNovuLozinku(ses.ID, osnovaKorisnik, "$2a$novi"); err == nil {
		t.Error("neuspjelo zadržavanje sesije mora javiti grešku")
	}
	_ = c.db.Close() // baza namjerno zatvorena: sljedeći pozivi moraju javiti grešku
	if err := s.CreateSession(&models.Session{UserID: osnovaKorisnik}); err == nil {
		t.Error("CreateSession bez baze")
	}
	if err := s.ZadrziUzNovuLozinku(ses.ID, osnovaKorisnik, "$2a$novi"); err == nil {
		t.Error("ZadrziUzNovuLozinku bez baze")
	}
}
