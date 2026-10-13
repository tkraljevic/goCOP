package service_test

import (
	"errors"
	"strings"
	"testing"

	"gocop/internal/service"
)

// Prijava i sesija kad baza zakaže: prijava koja se ne da zabilježiti ipak
// otvara sesiju, odjava je briše, a sesija koju baza ne primi ne pušta
// unutra. Sesija isključenog računa se odbacuje. Gledani račun koji se ne
// da pročitati zatvara pregled tuđim očima, a nečitljiv račun ili sesija
// prijavljenog javljaju grešku, ne istek.
func TestPrijavaISesijaKadBazaZakaze(t *testing.T) {
	o := okolinaDrugogKoraka(t)
	o.zabrani(t, "zabrana_traga", "UPDATE OF last_login_at", "users")
	sess, u, err := o.auth.Login("pperic", "lozinka1", "127.0.0.1", "proba")
	if err != nil || sess == nil || u.LastLoginAt != nil {
		t.Fatalf("prijava kad se trag ne da zabilježiti: %+v %+v %v", sess, u, err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana_traga`)
	if err := o.auth.Logout(sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.auth.AuthenticateSession(sess.ID); !errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("sesija nakon odjave: %v", err)
	}
	if _, err := o.auth.AuthenticateSessionView(sess.ID); !errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("pregled sesije nakon odjave: %v", err)
	}
	if sess, _, err = o.auth.Login("pperic", "lozinka1", "127.0.0.1", "proba"); err != nil {
		t.Fatal(err)
	}
	o.zabrani(t, "zabrana_sesije", "INSERT", "sessions")
	if _, _, err := o.auth.Login("pperic", "lozinka1", "127.0.0.1", "proba"); err == nil || !strings.Contains(err.Error(), "zabranjeno") {
		t.Errorf("prijava kad se sesija ne da spremiti: %v", err)
	}
	o.izvrsi(t, `DROP TRIGGER zabrana_sesije`)

	o.izvrsi(t, `UPDATE users SET is_active = 0 WHERE username = 'pperic'`)
	if _, err := o.auth.AuthenticateSessionView(sess.ID); !errors.Is(err, service.ErrAccountInactive) {
		t.Errorf("pregled sesije isključenog računa: %v", err)
	}
	if _, _, err := o.auth.AuthenticateSession(sess.ID); !errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("sesija isključenog računa nije odbačena: %v", err)
	}
	o.izvrsi(t, `UPDATE users SET is_active = 1 WHERE username = 'pperic'`)

	o.izvrsi(t, `UPDATE users SET is_global_admin = 1 WHERE username = 'uprava'`)
	uprava, _, err := o.auth.Login("uprava", "lozinka1", "127.0.0.1", "proba")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.auth.StartViewingAs(uprava.ID, o.admin, o.osoba.ID); err != nil {
		t.Fatal(err)
	}
	if v, err := o.auth.AuthenticateSessionView(uprava.ID); err != nil || !v.Viewing || v.User.ID != o.osoba.ID {
		t.Fatalf("pregled tuđim očima: %+v %v", v, err)
	}
	o.izvrsi(t, `UPDATE users SET created_at = 'nije vrijeme' WHERE username = 'pperic'`)
	if v, err := o.auth.AuthenticateSessionView(uprava.ID); err != nil || v.Viewing || v.User.ID != o.admin.User.ID {
		t.Errorf("pregled kad se gledani račun ne da pročitati: %+v %v", v, err)
	}
	o.izvrsi(t, `UPDATE users SET created_at = '2026-10-03 10:00:00' WHERE username = 'pperic'`)
	if v, err := o.auth.AuthenticateSessionView(uprava.ID); err != nil || v.Viewing {
		t.Errorf("pregled se vratio nakon popravka računa: %+v %v", v, err)
	}

	o.izvrsi(t, `UPDATE users SET created_at = 'nije vrijeme' WHERE username = 'uprava'`)
	if _, err := o.auth.AuthenticateSessionView(uprava.ID); err == nil || errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("pregled kad se vlastiti račun ne da pročitati: %v", err)
	}
	if _, _, err := o.auth.AuthenticateSession(uprava.ID); err == nil || errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("sesija kad se vlastiti račun ne da pročitati: %v", err)
	}
	if err := o.auth.ProvjeriLozinku(o.admin.User.ID, "lozinka1"); err == nil || errors.Is(err, service.ErrKrivaLozinka) {
		t.Errorf("provjera lozinke nečitljivog računa: %v", err)
	}
	if _, err := o.auth.ProvjeriPrijavu("uprava", "lozinka1"); err == nil || errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("prijava nečitljivog računa: %v", err)
	}
	o.izvrsi(t, `UPDATE users SET created_at = '2026-10-03 10:00:00' WHERE username = 'uprava'`)

	o.izvrsi(t, `UPDATE sessions SET created_at = 'nije vrijeme'`)
	if _, err := o.auth.AuthenticateSessionView(uprava.ID); err == nil || errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("pregled nečitljive sesije: %v", err)
	}
	if _, _, err := o.auth.AuthenticateSession(uprava.ID); err == nil || errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("nečitljiva sesija: %v", err)
	}
}
