package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Bez osobnog potpisnog ključa predaja i ovjera lista ne traže lozinku, pa
// ključ ne smije ukloniti onaj tko drži samo otvorenu prijavu: uklanjanje
// traži lozinku računa, kao i izrada, uz isto ograničenje krivih upisa. Dok
// se gleda tuđim očima (i uz uključene upise tuđim očima) ključ se ne pravi
// ni ne uklanja.
func TestPotpisniKljucSeUklanjaSamoUzLozinku(t *testing.T) {
	ctx := context.Background()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cvor")
	repo := repository.NewUserRepository(baza, rec)
	auth := service.NewAuthService(repo, repository.NewSessionRepository(baza))
	users := service.NewUserService(repo, auth, service.NewSSEBroker())
	novi := func(ime string) *models.User {
		hash, _ := auth.HashPassword("lozinka-" + ime)
		u := &models.User{Username: ime, PasswordHash: hash, FullName: ime, IsActive: true, OrgType: models.OrgHrvatskeVode}
		if err := repo.CreateUser(u, nil); err != nil {
			t.Fatal(err)
		}
		return u
	}
	ruk, drugi := novi("ruk"), novi("drugi")
	_, kc, _ := ed25519.GenerateKey(rand.Reader)
	ps := service.NewPotpisService(repository.NewPotpisRepository(baza, rec), users, "cvor", kc, auth.CheckPassword)
	if err := ps.Pokreni(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Novi(ctx, ruk, "lozinka-ruk"); err != nil {
		t.Fatal(err)
	}
	h := NewPotpisHandler(func() *service.PotpisService { return ps }, users, nil)
	ponovnaLozinka = newLoginLimiter()
	t.Cleanup(func() { ponovnaLozinka = newLoginLimiter() })

	posalji := func(u *models.User, gleda bool, v url.Values) string {
		r := httptest.NewRequest(http.MethodPost, "/profile/potpisni-kljuc", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c := context.WithValue(r.Context(), contextKeyUser, u)
		c = context.WithValue(c, contextKeyViewing, gleda)
		c = context.WithValue(c, contextKeyUpisiTudjim, gleda)
		w := httptest.NewRecorder()
		h.HandleKljuc(w, r.WithContext(c))
		return mustUnescape(w.Header().Get("Location"))
	}

	// ukradena sesija: bez lozinke i s krivom ključ ostaje
	if loc := posalji(ruk, false, url.Values{"radnja": {"obrisi"}}); !strings.Contains(loc, "error") || !ps.Ima(ctx, ruk.ID.String()) {
		t.Errorf("ključ uklonjen bez lozinke računa: %s", loc)
	}
	if loc := posalji(ruk, false, url.Values{"radnja": {"obrisi"}, "lozinka": {"kriva"}}); !strings.Contains(loc, "error") || !ps.Ima(ctx, ruk.ID.String()) {
		t.Errorf("ključ uklonjen krivom lozinkom: %s", loc)
	}
	// krivi upisi broje se zajedno s izradom: nakon granice ni točna ne prolazi
	for range 3 {
		posalji(ruk, false, url.Values{"radnja": {"novi"}, "lozinka": {"kriva"}})
	}
	if loc := posalji(ruk, false, url.Values{"radnja": {"obrisi"}, "lozinka": {"lozinka-ruk"}}); !strings.Contains(loc, "previše krivih lozinki") || !ps.Ima(ctx, ruk.ID.String()) {
		t.Errorf("nakon krivih upisa: %s", loc)
	}
	ponovnaLozinka = newLoginLimiter()

	// tuđim očima: ni uklanjanje ni izrada, ni s točnom lozinkom
	if loc := posalji(ruk, true, url.Values{"radnja": {"obrisi"}, "lozinka": {"lozinka-ruk"}}); !strings.Contains(loc, "error") || !ps.Ima(ctx, ruk.ID.String()) {
		t.Errorf("ključ uklonjen tuđim očima: %s", loc)
	}
	if loc := posalji(drugi, true, url.Values{"radnja": {"novi"}, "lozinka": {"lozinka-drugi"}}); !strings.Contains(loc, "error") || ps.Ima(ctx, drugi.ID.String()) {
		t.Errorf("ključ napravljen tuđim očima: %s", loc)
	}

	// vlasnik uz svoju lozinku
	if loc := posalji(ruk, false, url.Values{"radnja": {"obrisi"}, "lozinka": {"lozinka-ruk"}}); !strings.Contains(loc, "success") || ps.Ima(ctx, ruk.ID.String()) {
		t.Errorf("vlasnik nije uklonio ključ uz lozinku: %s", loc)
	}
	if loc := posalji(drugi, false, url.Values{"radnja": {"novi"}, "lozinka": {"lozinka-drugi"}}); !strings.Contains(loc, "success") || !ps.Ima(ctx, drugi.ID.String()) {
		t.Errorf("izrada ključa: %s", loc)
	}
}
