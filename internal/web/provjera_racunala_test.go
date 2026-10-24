package web

// Provjera veze s računalom na stranici Sinkronizacija (provjera_racunala.go)

import (
	"context"
	"crypto/ed25519"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	webassets "gocop/web"
)

// ishodRacunala je provjera laptopa s tri adrese: jedna prošla, jedna bez
// odgovora i jedna s greškom koja se ne prepozna
func ishodRacunala() peers.ProvjeraRacunala {
	return peers.ProvjeraRacunala{NodeID: "laptop-baranja", Naziv: "Laptop Baranja", Kad: time.Now(), Adrese: []peers.AdresaProvjere{
		{Adresa: "192.168.1.30:4710", Put: razmjena.NazivLAN, Izvor: peers.IzvorLokalnaMreza, RTT: 3 * time.Millisecond},
		{Adresa: "192.0.2.8:4710", Put: razmjena.NazivServer, Izvor: peers.IzvorZapis, Greska: "dial tcp 192.0.2.8:4710: i/o timeout", Razlog: peers.RazlogAdreseNemaOdgovor},
		{Adresa: "https://cop.primjer.hr", Put: razmjena.NazivServer, Izvor: peers.IzvorTunel, Greska: "tunel https://cop.primjer.hr: nešto treće"},
	}}
}

// Administrator iz lokalne mreže pokrene provjeru izabranog računala: ide
// jednom, uz mjerenje mreže ovog računala, a poruka nema adresa. Stranica
// pokazuje izbornik s računalima, ishod po adresi s izvorom, putom i
// razlogom te mrežu ovog računala. Drugi korisnik bloka ne vidi, a izvana
// nema gumba ni provjere.
func TestProvjeraVezeSRacunalomNaSinkronizaciji(t *testing.T) {
	s, _ := cvorIBazaZaWeb(t, "cop-osijek")
	for _, p := range []peers.Peer{{NodeID: "laptop-baranja", Name: "Laptop Baranja"}, {NodeID: "ured-vukovar"}} {
		kljuc, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		p.PublicKey = razmjena.PublicKeyString(kljuc)
		if err := s.SavePeer(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("sinkronizacija.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSyncHandler(s, tmpl)
	var trazeno []string
	ishod, greska := ishodRacunala(), error(nil)
	h.provjeriVezuS = func(_ context.Context, cvor string) (peers.ProvjeraRacunala, error) {
		trazeno = append(trazeno, cvor)
		return ishod, greska
	}
	mreza := ishodProvjere()
	h.mjeriMrezu = func(context.Context) *ProvjeraVeze { return &mreza }
	salji := func(u uvjetiZahtjeva) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.handleProvjeriRacunalo(w, zahtjevPostavki(http.MethodPost, "/sinkronizacija/provjeri-vezu", url.Values{"cvor": {" laptop-baranja "}}, u))
		return w
	}
	stranica := func(u uvjetiZahtjeva) string {
		w := httptest.NewRecorder()
		r := zahtjevPostavki(http.MethodGet, "/sinkronizacija", nil, u)
		h.ShowDashboard(w, r.WithContext(context.WithValue(r.Context(), contextKeyUser, u.korisnik)))
		if w.Code != http.StatusOK {
			t.Fatalf("stranica %d\n%s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}

	if html := stranica(kaoAdmin); !strings.Contains(html, `action="/sinkronizacija/provjeri-vezu"`) || !strings.Contains(html, `value="ured-vukovar">ured-vukovar`) ||
		strings.Contains(html, "Zadnja provjera") {
		t.Error("prije provjere: nema gumba, računala bez naziva ili ima ishod")
	}
	w := salji(kaoAdmin)
	lok, _ := url.QueryUnescape(w.Header().Get("Location"))
	if w.Code != http.StatusSeeOther || !strings.HasSuffix(lok, "#provjera-racunala") || len(trazeno) != 1 || trazeno[0] != "laptop-baranja" ||
		!strings.Contains(lok, "Provjera veze s računalom Laptop Baranja: javilo se na 1 od 3 adrese.") || reAdresa.MatchString(lok) {
		t.Errorf("provjera: %d %q, tražena %v", w.Code, lok, trazeno)
	}
	html := stranica(kaoAdmin)
	blok := html[strings.Index(html, `id="provjera-racunala"`):]
	blok = blok[:strings.Index(blok, "</section>")]
	for _, mora := range []string{`value="laptop-baranja" selected`, "Zadnja provjera: Laptop Baranja", "Ovo računalo: UDP radi",
		"192.168.1.30:4710", "javilo se u lokalnoj mreži", "LAN sync", "javlja se pravim ključem, 3 ms",
		"192.0.2.8:4710", "iz zapisa računala", "nema odgovora", "i/o timeout", "tunel", "ne radi", "nešto treće"} {
		if !strings.Contains(blok, mora) {
			t.Errorf("blok: nema %q", mora)
		}
	}
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	if html := stranica(obican); strings.Contains(html, `id="provjera-racunala"`) || strings.Contains(html, "192.168.1.30") {
		t.Error("blok vidi korisnik koji nije administrator")
	}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	if html := stranica(izvana); strings.Contains(html, `action="/sinkronizacija/provjeri-vezu"`) || !strings.Contains(html, zabranaIzvana.provjera) {
		t.Error("izvana: gumb ili bez razloga")
	}
	if w := salji(izvana); w.Code != http.StatusForbidden || len(trazeno) != 1 {
		t.Errorf("izvana: %d, provjera %d puta", w.Code, len(trazeno))
	}

	greska = errors.New("čvor laptop-baranja nije član naše mreže")
	if w := salji(kaoAdmin); !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("greška: %q", w.Header().Get("Location"))
	}
	ishod, greska = peers.ProvjeraRacunala{NodeID: "ured-vukovar", Naziv: "ured-vukovar", Kad: time.Now()}, nil
	h.mjeriMrezu = nil
	lok, _ = url.QueryUnescape(salji(kaoAdmin).Header().Get("Location"))
	if !strings.Contains(lok, "Za računalo ured-vukovar ovo računalo ne zna nijednu adresu.") {
		t.Errorf("bez adresa: %q", lok)
	}
	if html := stranica(kaoAdmin); !strings.Contains(html, "ovo ne zna nijednu adresu") || strings.Contains(html, "Ovo računalo:") {
		t.Error("bez adresa i bez mjerenja mreže")
	}
}

// Bez ijednog drugog računala izbornika nema, nego rečenica; poslužitelj bez
// mjerenja mreže ga ne daje
func TestProvjeraVezeBezDrugihRacunala(t *testing.T) {
	h := &SyncHandler{}
	data := SyncPageData{Permissions: &models.UserPermissions{IsGlobalAdmin: true}}
	h.dodajProvjeruRacunala(zahtjevPostavki(http.MethodGet, "/sinkronizacija", nil, kaoAdmin), &data)
	if p := data.ProvjeraRacunala; p == nil || !p.Nudi || len(p.Racunala) != 0 || p.Zadnja != nil {
		t.Errorf("blok bez računala: %+v", p)
	}
	if (&Server{}).mjeriMrezu(context.Background()) != nil {
		t.Error("mjerenje bez provjere u programu")
	}
	s, _ := cvorSPostavkama(t, sadrzajPostavk, nil)
	l := &lazniProvjeravatelj{ishod: ishodProvjere()}
	l.postavi(s)
	if p := s.mjeriMrezu(context.Background()); p == nil || l.puta != 1 {
		t.Errorf("mjerenje: %+v, %d puta", p, l.puta)
	}
}
