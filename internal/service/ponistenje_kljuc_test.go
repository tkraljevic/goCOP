package service_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"gocop/internal/pdfw"
	"gocop/internal/repository"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// Lozinku koju postavi netko drugi (poništenje u Korisnicima ili
// administrator u obrascu) osobni potpisni ključ ne prati: ključ zaključan
// starom lozinkom uklanja se, pa obavezna promjena lozinke ne zapne na
// prekljucavanju. Već potpisan dokument ostaje provjerljiv.
func TestPonistenjeIzKorisnikaUklanjaPotpisniKljuc(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOporavka(t)
	_, kljucCvora, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	users := service.NewUserService(o.repo, o.auth, service.NewSSEBroker())
	ps := service.NewPotpisService(repository.NewPotpisRepository(o.db, o.rec), users, "cvor-web", kljucCvora, o.auth.CheckPassword)
	if err := ps.Pokreni(ctx); err != nil {
		t.Fatal(err)
	}
	users.SetUklanjanjeKljuca(ps.UkloniKljuc)

	// dokument potpisan dok je ključ još postojao
	if _, err := ps.Novi(ctx, o.sef, "stara-lozinka"); err != nil {
		t.Fatal(err)
	}
	pk, err := ps.Potpisnik(ctx, o.sef, "stara-lozinka")
	if err != nil {
		t.Fatal(err)
	}
	d := pdfw.Novi("Dnevni list", "goCOP")
	d.SviZnakovi()
	d.Tekst(56, 80, 12, true, "DNEVNI LIST")
	potpisan, err := pk.PotpisiPDF(d.Bajtovi(), pdfw.Dodatak{Stranica: 1, X: 56, Y: 600, W: 200, H: 46,
		Crtaj: func(*pdfw.Doc) {}, Razlog: "Ovjera", Kad: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}

	_, temp, err := users.ResetPassword(o.admin, o.sef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ps.Ima(ctx, o.sef.ID.String()) {
		t.Fatal("potpisni ključ zaključan starom lozinkom ostao je nakon poništenja")
	}
	// obavezna promjena lozinke, kao na /profile?force=1
	const nova = "nova-lozinka-2026"
	if err := ps.Prekljucaj(ctx, o.sef.ID.String(), temp, nova); err != nil {
		t.Fatalf("promjena lozinke zapinje na potpisnom ključu: %v", err)
	}
	if err := o.auth.ChangePassword(o.sef.ID, temp, nova, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps.Provjeri(ctx, potpisan) {
		if !p.Valjan {
			t.Errorf("potpis dan prije uklanjanja ključa više nije valjan: %s", p.Greska)
		}
	}
	if n := len(ps.Provjeri(ctx, potpisan)); n != 1 {
		t.Fatalf("potpisa u dokumentu: %d", n)
	}

	// i lozinka koju administrator upiše u obrascu
	svjez, err := o.repo.GetUserByID(o.sef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Novi(ctx, svjez, nova); err != nil {
		t.Fatal(err)
	}
	if _, err := users.UpdateUser(o.admin, service.UpdateUserRequest{ID: svjez.ID, Username: svjez.Username,
		FullName: svjez.FullName, OrgType: svjez.OrgType, Email: svjez.Email, IsActive: true,
		IsGlobalAdmin: svjez.IsGlobalAdmin, Password: "lozinka-od-administratora"}); err != nil {
		t.Fatal(err)
	}
	if ps.Ima(ctx, svjez.ID.String()) {
		t.Error("potpisni ključ ostao je nakon lozinke koju je upisao administrator")
	}

	// bez ključa poništenje radi kao i prije
	if _, _, err := users.ResetPassword(o.admin, o.sef.ID); err != nil {
		t.Fatalf("poništenje bez ključa: %v", err)
	}
}
