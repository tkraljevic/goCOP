package web

import (
	"context"
	"net/http"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Potpis e-pošte i sken vlastoručnog potpisa tuđim se očima ne otvaraju ni
// ne mijenjaju, ni uz uključene upise tuđim očima. Ostatak profila tuđim
// očima se otvara, a administrator svojim očima otvara svoj potpis.
func TestTudjimOcimaPotpisSeNeOtvara(t *testing.T) {
	o := novaOkolinaPrijave(t)
	uprava := o.osoba("uprava", "upravina-lozinka", "uprava@example.com", true)
	ana := o.osoba("aanic", "anina-lozinka", "aanic@example.com", false)
	rec := ledger.New(o.baza, "test")
	akti := service.NewAktService(repository.NewAktiRepository(o.baza, rec), nil, nil, nil, nil, nil, nil, "test")
	if err := akti.SpremiOpcije(context.Background(), o.ovlasti(uprava), models.Opcije{UpisTudjimOcima: true}); err != nil {
		t.Fatal(err)
	}
	s := &Server{authService: o.auth, moduleService: service.NewModuleService(repository.NewModuleRepository(o.baza, rec)), akti: akti}
	pozvan := false
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pozvan = true
		w.WriteHeader(http.StatusNoContent)
	}))
	svoja, _, err := o.auth.Login("uprava", "upravina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	tudja, _, err := o.auth.Login("uprava", "upravina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.auth.StartViewingAs(tudja.ID, o.ovlasti(uprava), ana.ID); err != nil {
		t.Fatal(err)
	}
	zovi := func(metoda, putanja, sesija string) (int, bool) {
		pozvan = false
		return mwZahtjev(h, metoda, putanja, sesija).Code, pozvan
	}

	for _, z := range []struct{ metoda, putanja string }{
		{http.MethodGet, "/profile/potpis"},
		{http.MethodPost, "/profile/potpis"},
		{http.MethodGet, "/profile/potpis-slika"},
		{http.MethodPost, "/profile/potpis-slika"},
	} {
		if kod, dosao := zovi(z.metoda, z.putanja, tudja.ID.String()); kod != http.StatusForbidden || dosao {
			t.Errorf("%s %s tuđim očima: %d (rukovatelj pozvan: %v)", z.metoda, z.putanja, kod, dosao)
		}
		if kod, dosao := zovi(z.metoda, z.putanja, svoja.ID.String()); kod != http.StatusNoContent || !dosao {
			t.Errorf("%s %s svojim očima: %d", z.metoda, z.putanja, kod)
		}
	}
	if kod, dosao := zovi(http.MethodGet, "/profile", tudja.ID.String()); kod != http.StatusNoContent || !dosao {
		t.Errorf("profil tuđim očima: %d", kod)
	}
}

// Koje se putanje tuđim očima ne otvaraju i što tada piše
func TestZatvorenoTudjimOcima(t *testing.T) {
	for putanja, zatvoreno := range map[string]bool{
		"/posta": true, "/profile/posta": true, "/profile/potpis": true, "/profile/potpis-slika": true,
		"/profile": false, "/profile/potpisni-kljuc": false, "/posta/logo.png": false, "/akti/1": false,
	} {
		if got := zatvorenoTudjimOcima(putanja) != ""; got != zatvoreno {
			t.Errorf("%s: zatvoreno = %v, očekivano %v", putanja, got, zatvoreno)
		}
	}
}
