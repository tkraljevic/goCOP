package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Skrivanje gumba nije ovlast.
//
// Gumbi za ugradnju i izvoz paketa prikazivali su se samo administratoru, ali
// rute su imale samo opći autentifikacijski sloj. Prijavljeni korisnik bez
// administratorskih prava mogao je poslati POST na /stations/{id}/paket/ugradi
// i zamijeniti kompletan historijat letve.
//
// Ovaj test zove rute izravno, kao običan korisnik, i traži 403.
func TestArhivskeRuteOdbijajuObicnogKorisnika(t *testing.T) {
	rute := []struct{ metoda, putanja string }{
		{http.MethodGet, "/stations/9f1c0b2e-0000-7000-8000-000000000000/paket.cop"},
		{http.MethodPost, "/stations/9f1c0b2e-0000-7000-8000-000000000000/paket/pregled"},
		{http.MethodPost, "/stations/9f1c0b2e-0000-7000-8000-000000000000/paket/ugradi"},
		{http.MethodGet, "/administracija/uvoz-niza"},
		{http.MethodPost, "/administracija/uvoz-niza/pregled"},
		{http.MethodPost, "/administracija/uvoz-niza/pregled-opet"},
		{http.MethodPost, "/administracija/uvoz-niza/upisi"},
		{http.MethodPost, "/administracija/uvoz-niza/makni-niz"},
		{http.MethodPost, "/administracija/uvoz-niza/zatecen"},
		{http.MethodPost, "/administracija/ulaganje/pregled"},
		{http.MethodPost, "/administracija/ulaganje"},
		{http.MethodPost, "/administracija/ulaganje/pospremi"},
		{http.MethodPost, "/administracija/izdavanje/provjera"},
		{http.MethodPost, "/administracija/izdavanje"},
		{http.MethodGet, "/administracija/izvori"},
		{http.MethodPost, "/administracija/izvori"},
	}

	// Ograda se ispituje sama, bez baze: authMiddleware traži sesiju, a ovdje
	// se gleda što radi sloj iznad njega kad prava nisu dovoljna.
	presao := false
	cilj := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { presao = true })

	for _, ruta := range rute {
		for _, slucaj := range []struct {
			ime   string
			perms *models.UserPermissions
			zelim int
		}{
			{"bez prava", nil, http.StatusForbidden},
			{"obican korisnik", &models.UserPermissions{IsGlobalAdmin: false}, http.StatusForbidden},
			{"administrator", &models.UserPermissions{IsGlobalAdmin: true}, http.StatusOK},
		} {
			presao = false
			r := httptest.NewRequest(ruta.metoda, ruta.putanja, strings.NewReader(""))
			r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, slucaj.perms))
			w := httptest.NewRecorder()
			trebaAdmina(cilj).ServeHTTP(w, r)

			if slucaj.zelim == http.StatusForbidden {
				if w.Code != http.StatusForbidden {
					t.Errorf("%s %s kao %s: %d, a mora biti 403", ruta.metoda, ruta.putanja, slucaj.ime, w.Code)
				}
				if presao {
					t.Errorf("%s %s kao %s: zahtjev je prošao do rukovatelja", ruta.metoda, ruta.putanja, slucaj.ime)
				}
				continue
			}
			if !presao {
				t.Errorf("%s %s kao %s: administrator nije prošao", ruta.metoda, ruta.putanja, slucaj.ime)
			}
		}
	}
}

// Svaka ruta koja mijenja arhivu mora biti registrirana kroz samoAdmin, a ne
// kroz opći authMiddleware. Ovo hvata rutu koja se doda poslije i zaboravi.
func TestSveArhivskeRuteIduKrozOgradu(t *testing.T) {
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	izvor := string(b)
	moraju := []string{
		"/stations/{id}/paket.cop",
		"/stations/{id}/paket/pregled",
		"/stations/{id}/paket/ugradi",
		"/administracija/uvoz-niza",
		"/administracija/uvoz-niza/upisi",
		"/administracija/uvoz-niza/makni-niz",
		"/administracija/ulaganje",
		"/administracija/ulaganje/pospremi",
		"/administracija/izdavanje",
		"/administracija/izvori",
	}
	for _, put := range moraju {
		for _, redak := range strings.Split(izvor, "\n") {
			if !strings.Contains(redak, `"`) || !strings.Contains(redak, put+`"`) {
				continue
			}
			if !strings.Contains(redak, "s.samoAdmin(") {
				t.Errorf("ruta %s nije registrirana kroz samoAdmin: %s", put, strings.TrimSpace(redak))
			}
		}
	}
}

// Poslužitelj ne smije pasti pri pokretanju zbog sudara putanja.
//
// "/dnevnici/vrsta/{kind}" sudarilo se s "/dnevnici/{id}/edit" — obje hvataju
// "/dnevnici/vrsta/edit" — i Go je to javio panikom pri pokretanju. Ovo drži da
// se rute daju registrirati.
func TestRuteSeDajuRegistriratiBezSudara(t *testing.T) {
	defer func() {
		if x := recover(); x != nil {
			t.Fatalf("registracija ruta je pukla: %v", x)
		}
	}()
	mux := http.NewServeMux()
	for _, uzorak := range []string{
		"GET /dnevnici",
		"GET /dnevnici/popis",
		"GET /dnevnici/new",
		"GET /dnevnici/{id}",
		"GET /dnevnici/{id}/edit",
		"GET /dnevnici/{id}/ispis",
		"GET /dnevnici/{id}/listovi/{sheet}",
	} {
		mux.Handle(uzorak, http.NotFoundHandler())
	}
}
