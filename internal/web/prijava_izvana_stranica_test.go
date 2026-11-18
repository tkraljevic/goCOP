package web

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Prijava izvana ima svoju stranicu: kvačice uvjeta za uključivanje PIN-a,
// broj djelatnika bez adrese za PIN i odjeljak sklopke i računa (Odluke
// (18. 11. 2026.), Administracija, novo ponašanje 1)
func TestStranicaPrijaveIzvana(t *testing.T) {
	o := novaOkolinaIzvana(t)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	o.racun("bezadrese", "lozinka-bez-adrese", "", false)
	o.aktiH.SetPrijavaIzvana(predlozakStranice(t, "administracija_prijava_izvana.html"))

	r := httptest.NewRequest(http.MethodGet, "/administracija/prijava-izvana", nil)
	r.RemoteAddr = "192.168.1.50:40000"
	r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyUser, admin), contextKeyPerms, &models.UserPermissions{User: *admin, IsGlobalAdmin: true}))
	w := httptest.NewRecorder()
	o.aktiH.ShowPrijavaIzvana(w, r)
	tijelo := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	for _, treba := range []string{"Što treba prije uključivanja", "Probni PIN s tim računom je stigao.",
		`<a href="/users?status=BEZ_EPOSTE">Tko ne dobiva PIN: 1 djelatnik</a>`, `action="/administracija/posta/pin/sklopka"`} {
		if !strings.Contains(tijelo, treba) {
			t.Errorf("na stranici nema %q", treba)
		}
	}
	if strings.Contains(tijelo, `class="ispunjen"`) {
		t.Error("bez akata, računa i probe nijedan uvjet nije ispunjen")
	}

	// bez drugog koraka stranica ostaje bez odjeljka PIN-a, a pokvaren
	// predložak javlja grešku umjesto prazne stranice
	o.aktiH.SetDrugiKorak(func() *service.DrugiKorak { return nil })
	w = httptest.NewRecorder()
	o.aktiH.ShowPrijavaIzvana(w, r)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Što treba prije uključivanja") {
		t.Errorf("bez drugog koraka: %d", w.Code)
	}
	o.aktiH.SetPrijavaIzvana(template.Must(template.New("x").Parse(`{{define "administracija_prijava_izvana.html"}}{{.NemaTogPolja}}{{end}}`)))
	w = httptest.NewRecorder()
	o.aktiH.ShowPrijavaIzvana(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("pokvaren predložak: %d", w.Code)
	}
}

func TestUvjetiPINa(t *testing.T) {
	sad := time.Now()
	sve := &PINStanje{StanjeSlanja: service.StanjeSlanja{Posiljatelj: &service.Posiljatelj{Korisnik: "pin"}, ProbaUspjela: true}, Izvana: true}
	for _, u := range uvjetiPINa(sve, true) {
		if !u.Ispunjen {
			t.Errorf("ispunjeno sve, a nije: %s", u.Tekst)
		}
	}
	los := &PINStanje{StanjeSlanja: service.StanjeSlanja{Posiljatelj: &service.Posiljatelj{Korisnik: "pin", NeispravanOd: &sad}}, Izvana: true, BezHTTPS: true}
	for i, u := range uvjetiPINa(los, false) {
		if u.Ispunjen {
			t.Errorf("uvjet %d ispunjen uz odbijenu lozinku i bez HTTPS-a: %s", i, u.Tekst)
		}
	}
}
