package web

import (
	"context"
	"gocop/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Zapis pod tuđim imenom ne smije nastati ni omaškom, pa se u tuđem pogledu
// propušta samo čitanje i izlaz natrag k sebi.
func TestUTudemPogleduProlaziSamoCitanje(t *testing.T) {
	slucajevi := []struct {
		metoda, putanja string
		prolazi         bool
	}{
		{http.MethodGet, "/users", true},
		{http.MethodGet, "/api/sections/A.19.1", true},
		{http.MethodHead, "/dashboard", true},
		{http.MethodPost, "/users/create", false},
		{http.MethodPost, "/users/delete", false},
		{http.MethodPost, "/sections/update", false},
		{http.MethodPost, "/profile/change-password", false},
		{http.MethodPost, "/api/network/create", false},
		{http.MethodPost, "/view-as/stop", true},
		{http.MethodPost, "/view-as/9f1c0b2e-0000-7000-8000-000000000000", true},
	}

	for _, s := range slucajevi {
		r := httptest.NewRequest(s.metoda, s.putanja, nil)
		if got := readOnlyRequest(r); got != s.prolazi {
			t.Errorf("%s %s: prolazi = %v, očekivano %v", s.metoda, s.putanja, got, s.prolazi)
		}
	}
}

// Tuđi sandučić ne otvara se ni tuđim očima, ni samo za čitanje
func TestTudjimOcimaNemaTudjePoste(t *testing.T) {
	for putanja, tudja := range map[string]bool{
		"/posta": true, "/posta/pismo": true, "/posta/privitak": true, "/posta/novo": true,
		"/profile/posta": true, "/postaje": false, "/profile": false, "/akti/posta": false, "/posta/logo.png": false,
	} {
		if got := tudjaPosta(putanja); got != tudja {
			t.Errorf("%s: tuđa pošta = %v, očekivano %v", putanja, got, tudja)
		}
	}
}

// Kroz pravu provjeru prijave: administrator koji gleda tuđim očima ne
// otvara sandučić ni stranicu lozinke e-pošte te osobe
func TestTudjimOcimaSanducicSeNeOtvara(t *testing.T) {
	o := novaOkolinaPrijave(t)
	uprava := o.osoba("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ana := o.osoba("ana", "anina-lozinka", "ana@voda.hr", false)
	sesija, _, err := o.auth.Login("uprava", "upravina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.auth.StartViewingAs(sesija.ID, o.ovlasti(uprava), ana.ID); err != nil {
		t.Fatal(err)
	}
	dosao := false
	h := (&Server{authService: o.auth}).authMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { dosao = true }))
	for _, putanja := range []string{"/posta", "/posta/pismo?id=1", "/posta/privitak?id=1", "/profile/posta"} {
		r := httptest.NewRequest(http.MethodGet, putanja, nil)
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || dosao {
			t.Errorf("%s tuđim očima: %d (rukovatelj pozvan: %v)", putanja, w.Code, dosao)
		}
	}
}

// Usporedba imenika s adresarom ide tuđom lozinkom e-pošte, pa se tuđim
// očima ne pokreće (ni nakon spremanja odabira iz adresara)
func TestTudjimOcimaNemaUsporedbeImenika(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/users/exchange/usporedi", nil)
	r = r.WithContext(context.WithValue(r.Context(), contextKeyViewing, true))
	h := &AktiHandler{}
	adresa, razlog := h.pokreniUsporedbu(r, nil, &models.User{}, &models.UserPermissions{IsGlobalAdmin: true}, "B")
	if adresa != "" || !strings.Contains(razlog, "tuđim očima") {
		t.Fatalf("usporedba tuđim očima: %q, %q", adresa, razlog)
	}
}
