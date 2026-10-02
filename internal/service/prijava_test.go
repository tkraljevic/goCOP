package service_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// prazanCvorPrijave je baza bez imenika, s računima koje test sam stvori
type prazanCvorPrijave struct {
	db       *sql.DB
	rec      *ledger.Recorder
	repo     *repository.UserRepository
	sessions *repository.SessionRepository
	auth     *service.AuthService
	users    *service.UserService
}

func noviCvorPrijave(t *testing.T) *prazanCvorPrijave {
	t.Helper()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "prijava.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(database, "test")
	repo := repository.NewUserRepository(database, rec)
	sessions := repository.NewSessionRepository(database)
	auth := service.NewAuthService(repo, sessions)
	return &prazanCvorPrijave{db: database, rec: rec, repo: repo, sessions: sessions, auth: auth, users: service.NewUserService(repo, auth, service.NewSSEBroker())}
}

func (c *prazanCvorPrijave) racun(t *testing.T, ime, lozinka string, aktivan bool) *models.User {
	t.Helper()
	hash, err := c.auth.HashPassword(lozinka)
	if err != nil {
		t.Fatal(err)
	}
	u := &models.User{Username: ime, FullName: ime, PasswordHash: hash, IsActive: aktivan}
	if err := c.repo.CreateUser(u, nil); err != nil {
		t.Fatal(err)
	}
	return u
}

// Deaktiviran račun ne smije otkriti da ime postoji: s krivom lozinkom
// odgovor je isti kao za nepostojeće ime, a „deaktiviran” tek uz točnu
func TestDeaktiviranRacunTekNakonTocneLozinke(t *testing.T) {
	c := noviCvorPrijave(t)
	c.racun(t, "umirovljen", "stara-lozinka", false)

	if _, err := c.auth.ProvjeriPrijavu("umirovljen", "kriva"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("kriva lozinka na deaktiviranom računu: %v, očekivano ErrInvalidCredentials", err)
	}
	if _, err := c.auth.ProvjeriPrijavu("nitko", "kriva"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("nepostojeće ime: %v", err)
	}
	if _, err := c.auth.ProvjeriPrijavu("umirovljen", "stara-lozinka"); !errors.Is(err, service.ErrAccountInactive) {
		t.Errorf("točna lozinka na deaktiviranom računu: %v, očekivano ErrAccountInactive", err)
	}
}

// Nepostojeće ime mora proći istu provjeru sažetka kao postojeće, inače se
// iz vremena odgovora vidi tko ima račun. bcrypt traje desetke milisekundi,
// a odgovor bez njega djelić milisekunde, pa je trećina sigurna granica.
func TestNepostojeceImeTrajeKaoKrivaLozinka(t *testing.T) {
	c := noviCvorPrijave(t)
	c.racun(t, "ana", "anina-lozinka", true)

	najkrace := func(ime string) time.Duration {
		best := time.Hour
		for i := 0; i < 3; i++ {
			start := time.Now()
			_, _ = c.auth.ProvjeriPrijavu(ime, "kriva-lozinka")
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	postoji, nepostoji := najkrace("ana"), najkrace("nepostojeci")
	if nepostoji < postoji/3 {
		t.Errorf("nepostojeće ime odgovara za %v, postojeće s krivom lozinkom za %v", nepostoji, postoji)
	}
}

// Token sesije je UUIDv4 (122 slučajna bita), ne UUIDv7 s vremenom i brojačem
func TestTokenSesijeJeSlucajan(t *testing.T) {
	c := noviCvorPrijave(t)
	c.racun(t, "ana", "anina-lozinka", true)
	s, _, err := c.auth.Login("ana", "anina-lozinka", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if v := s.ID.Version(); v != 4 {
		t.Errorf("token sesije je UUID verzije %d, očekivano 4", v)
	}
	// i sesija stvorena bez tokena dobiva v4
	bez := &models.Session{UserID: s.UserID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := c.sessions.CreateSession(bez); err != nil {
		t.Fatal(err)
	}
	if v := bez.ID.Version(); v != 4 {
		t.Errorf("sesija bez tokena dobila je UUID verzije %d", v)
	}
}

func zivaSesija(t *testing.T, c *prazanCvorPrijave, id uuid.UUID) bool {
	t.Helper()
	s, err := c.sessions.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	return s != nil
}

// Promjena lozinke gasi sve druge prijave te osobe; ona iz koje je lozinka
// promijenjena ostaje. Kriva trenutna lozinka prepoznaje se kao ErrKrivaLozinka.
func TestPromjenaLozinkeGasiOstalePrijave(t *testing.T) {
	c := noviCvorPrijave(t)
	ana := c.racun(t, "ana", "anina-lozinka", true)
	ova, _, err := c.auth.Login("ana", "anina-lozinka", "127.0.0.1", "laptop")
	if err != nil {
		t.Fatal(err)
	}
	druga, _, err := c.auth.Login("ana", "anina-lozinka", "203.0.113.5", "ukraden")
	if err != nil {
		t.Fatal(err)
	}

	err = c.auth.ChangePassword(ana.ID, "kriva", "nova-lozinka", ova.ID)
	if !errors.Is(err, service.ErrKrivaLozinka) || err.Error() != "trenutna lozinka nije točna" {
		t.Errorf("kriva trenutna lozinka: %v", err)
	}
	if !zivaSesija(t, c, druga.ID) {
		t.Fatal("neuspjela promjena ne smije gasiti prijave")
	}

	if err := c.auth.ChangePassword(ana.ID, "anina-lozinka", "nova-lozinka", ova.ID); err != nil {
		t.Fatal(err)
	}
	if !zivaSesija(t, c, ova.ID) {
		t.Error("prijava iz koje je lozinka promijenjena mora ostati")
	}
	if zivaSesija(t, c, druga.ID) {
		t.Error("druga prijava mora prestati nakon promjene lozinke")
	}
}

// Administrator koji poništi lozinku gasi sve prijave te osobe
func TestPonistenaLozinkaGasiPrijave(t *testing.T) {
	c := noviCvorPrijave(t)
	admin := c.racun(t, "sef", "sefova-lozinka", true)
	ana := c.racun(t, "ana", "anina-lozinka", true)
	s, _, err := c.auth.Login("ana", "anina-lozinka", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.users.ResetPassword(&models.UserPermissions{User: *admin, IsGlobalAdmin: true}, ana.ID); err != nil {
		t.Fatal(err)
	}
	if zivaSesija(t, c, s.ID) {
		t.Error("poništena lozinka mora ugasiti prijave osobe")
	}
}

// Kriva lozinka pri izradi i otključavanju potpisnog ključa prepoznaje se kao
// ErrKrivaLozinka, da je rukovatelj može brojati; poruka ostaje ista
func TestKrivaLozinkaPotpisnogKljuca(t *testing.T) {
	c := noviCvorPrijave(t)
	ana := c.racun(t, "ana", "anina-lozinka", true)
	_, kljuc, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ps := service.NewPotpisService(repository.NewPotpisRepository(c.db, c.rec), c.users, "test", kljuc, c.auth.CheckPassword)
	if err := ps.Pokreni(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Novi(ctx, ana, "kriva"); !errors.Is(err, service.ErrKrivaLozinka) || err.Error() != "lozinka nije točna" {
		t.Errorf("izrada ključa krivom lozinkom: %v", err)
	}
	if _, err := ps.Novi(ctx, ana, ""); errors.Is(err, service.ErrKrivaLozinka) {
		t.Error("prazna lozinka nije kriva lozinka")
	}
	if _, err := ps.Novi(ctx, ana, "anina-lozinka"); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Potpisnik(ctx, ana, "kriva"); !errors.Is(err, service.ErrKrivaLozinka) {
		t.Errorf("otključavanje krivom lozinkom: %v", err)
	}
	if _, err := ps.Potpisnik(ctx, ana, "anina-lozinka"); err != nil {
		t.Errorf("otključavanje točnom lozinkom: %v", err)
	}
}
