package web

// Radnje nad mrežom izvana (Odluke (30. 10. 2026.), 12): osnivanje mreže,
// zaborav čvora, obnova članstva, adrese, oznaka „stalno izložen” i ulazak
// u tuđu mrežu uparivanjem traže izvana lozinku računa, a s ovog računala i
// iz lokalne mreže ne.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/peers"
)

// mrezaIzvana su ista osoba izvana, s javne adrese i iz lokalne mreže
type mrezaIzvana struct {
	izvana, javno, lokalno zahtjevMreze
}

func novaMrezaIzvana(t *testing.T) mrezaIzvana {
	t.Helper()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	ana := &models.User{ID: uuid.New(), Username: "aivic", FullName: "Ana Ivić"}
	t.Cleanup(func() { ponovnaLozinka.Reset(kljuceviLozinkeRacuna(context.Background(), ana.ID.String())...) })
	return mrezaIzvana{
		izvana:  zahtjevMreze{porijeklo: krozTunel("198.51.100.7"), perms: admin, osoba: ana},
		javno:   zahtjevMreze{porijeklo: javnoIzravno, perms: admin, osoba: ana},
		lokalno: zahtjevMreze{porijeklo: izravno, perms: admin, osoba: ana},
	}
}

// uparen upiše poznati čvor id u s
func uparen(t *testing.T, s *peers.Service, id string) {
	t.Helper()
	if err := s.SavePeer(context.Background(), peers.Peer{NodeID: id, Name: id, PublicKey: svjeziCvorZaWeb(t, id).Node().PublicKey(), Addresses: []string{"192.168.1.30:4710"}}); err != nil {
		t.Fatal(err)
	}
}

func TestMrezaIzvanaTraziLozinku(t *testing.T) {
	dnevnik := dnevnikPrograma(t)
	m := novaMrezaIzvana(t)
	ured := primateljNaDaljinu(t)
	h := &SettingsHandler{peers: ured}
	h.SetZastitaKljuca(lozinkaTesta)
	uparen(t, ured, "laptop-ilok")
	uparen(t, ured, "laptop-vukovar")
	s := func(lozinka string, polja ...any) map[string]any {
		tijelo := map[string]any{"lozinka_racuna": lozinka}
		for i := 0; i+1 < len(polja); i += 2 {
			tijelo[polja[i].(string)] = polja[i+1]
		}
		return tijelo
	}
	for _, c := range []struct {
		ime   string
		f     http.HandlerFunc
		cvor  string
		polja []any
	}{
		{"stalno izložen", h.HandleSetBootstrap, "laptop-ilok", []any{"bootstrap", true}},
		{"adresa", h.HandlePublicAddress, "cop-osijek", []any{"add", "cop.primjer.hr"}},
		{"obnova članstva", h.HandleObnoviClanstvo, "", nil},
		{"obnova člana", h.HandleObnoviClana, "cop-osijek", nil},
		{"zaborav čvora", h.HandleForgetPeer, "laptop-ilok", nil},
	} {
		if w := m.izvana.zovi(t, c.f, c.cvor, s("", c.polja...)); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "uz lozinku vašeg računa") {
			t.Errorf("%s izvana bez lozinke: %d %q", c.ime, w.Code, w.Body.String())
		}
		if w := m.javno.zovi(t, c.f, c.cvor, s("anina-lozinka", c.polja...)); w.Code != http.StatusOK {
			t.Errorf("%s s javne adrese s lozinkom: %d %q", c.ime, w.Code, w.Body.String())
		}
	}
	if w := m.izvana.zovi(t, h.HandlePublicAddress, "cop-osijek", s("kriva", "remove", "cop.primjer.hr")); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Ništa nije promijenjeno: lozinka nije točna") {
		t.Errorf("adresa izvana s krivom lozinkom: %d %q", w.Code, w.Body.String())
	}
	for _, c := range []struct {
		ime   string
		f     http.HandlerFunc
		cvor  string
		polja []any
	}{
		{"stalno izložen", h.HandleSetBootstrap, "laptop-vukovar", []any{"bootstrap", true}},
		{"adresa", h.HandlePublicAddress, "cop-osijek", []any{"remove", "cop.primjer.hr"}},
		{"obnova članstva", h.HandleObnoviClanstvo, "", nil},
		{"zaborav čvora", h.HandleForgetPeer, "laptop-vukovar", nil},
	} {
		if w := m.lokalno.zovi(t, c.f, c.cvor, s("", c.polja...)); w.Code != http.StatusOK {
			t.Errorf("%s iz lokalne mreže bez lozinke: %d %q", c.ime, w.Code, w.Body.String())
		}
	}
	for _, d := range []string{`POST "/api/network/x", s adrese 198.51.100.7`, `izvana bez točne lozinke računa odbijeno: "Ana Ivić (aivic)"`} {
		if !strings.Contains(dnevnik.String(), d) {
			t.Errorf("dnevnik programa nema %q:\n%s", d, dnevnik.String())
		}
	}
	if strings.Contains(dnevnik.String(), "anina-lozinka") {
		t.Error("lozinka je u dnevniku programa")
	}
	if p, err := ured.GetPeer(context.Background(), "laptop-ilok"); err != nil || p != nil {
		t.Errorf("zaboravljeni čvor: %+v %v", p, err)
	}
}

// Osnivanje mreže i ulazak u tuđu: na računalu koje nije ni u jednoj mreži
// izvana uz lozinku, a iz lokalne mreže bez nje. I na članu koji ne prima u
// mrežu potvrda koda izvana traži lozinku, jer upisuje poznati čvor.
func TestOsnivanjeIUlazakIzvanaUzLozinku(t *testing.T) {
	m := novaMrezaIzvana(t)
	samotnjak := &SettingsHandler{peers: svjeziCvorZaWeb(t, "samotnjak")}
	samotnjak.SetZastitaKljuca(lozinkaTesta)
	potvrda := func(approved bool, lozinka string) map[string]any {
		return map[string]any{"approved": approved, "lozinka_racuna": lozinka}
	}
	if w := m.izvana.zovi(t, samotnjak.HandlePairConfirm, "", potvrda(true, "")); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "uz lozinku vašeg računa") {
		t.Errorf("ulazak izvana bez lozinke: %d %q", w.Code, w.Body.String())
	}
	// do razmjene stiže: nema uparivanja koje čeka odluku
	for ime, w := range map[string]*httptest.ResponseRecorder{
		"izvana s lozinkom": m.izvana.zovi(t, samotnjak.HandlePairConfirm, "", potvrda(true, "anina-lozinka")),
		"izvana odbijanje":  m.izvana.zovi(t, samotnjak.HandlePairConfirm, "", potvrda(false, "")),
		"lokalno":           m.lokalno.zovi(t, samotnjak.HandlePairConfirm, "", potvrda(true, "")),
	} {
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nema uparivanja") {
			t.Errorf("ulazak %s: %d %q", ime, w.Code, w.Body.String())
		}
	}

	osnuj := func(ime, lozinka string) map[string]string {
		return map[string]string{"name": ime, "lozinka_racuna": lozinka}
	}
	if w := m.izvana.zovi(t, samotnjak.HandleCreateNetwork, "", osnuj("Hrvatske vode", "")); w.Code != http.StatusForbidden || samotnjak.peers.NetworkInfo() != nil {
		t.Errorf("osnivanje izvana bez lozinke: %d %q", w.Code, w.Body.String())
	}
	if w := m.izvana.zovi(t, samotnjak.HandleCreateNetwork, "", "nije objekt"); w.Code != http.StatusBadRequest {
		t.Errorf("osnivanje s neispravnim tijelom: %d", w.Code)
	}
	if w := m.izvana.zovi(t, samotnjak.HandleCreateNetwork, "", osnuj("Hrvatske vode", "anina-lozinka")); w.Code != http.StatusOK || samotnjak.peers.NetworkInfo() == nil {
		t.Errorf("osnivanje izvana s lozinkom: %d %q", w.Code, w.Body.String())
	}
	drugi := &SettingsHandler{peers: svjeziCvorZaWeb(t, "drugi")}
	if w := m.lokalno.zovi(t, drugi.HandleCreateNetwork, "", osnuj("Druga mreža", "")); w.Code != http.StatusOK {
		t.Errorf("osnivanje iz lokalne mreže: %d %q", w.Code, w.Body.String())
	}

	// član bez ovlasti za primanje: potvrda samo uparuje
	ured := primateljNaDaljinu(t)
	clan := svjeziCvorZaWeb(t, "pperic-thinkpad")
	zahtjev, kod, err := clan.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	p, err := ured.PrimiZahtjev(context.Background(), zahtjev, kod, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clan.UveziPotvrdu(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	clanH := &SettingsHandler{peers: clan}
	clanH.SetZastitaKljuca(lozinkaTesta)
	if w := m.izvana.zovi(t, clanH.HandlePairConfirm, "", potvrda(true, "")); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "uz lozinku vašeg računa") {
		t.Errorf("član izvana bez lozinke: %d %q", w.Code, w.Body.String())
	}
	for ime, w := range map[string]*httptest.ResponseRecorder{
		"izvana s lozinkom": m.izvana.zovi(t, clanH.HandlePairConfirm, "", potvrda(true, "anina-lozinka")),
		"izvana odbijanje":  m.izvana.zovi(t, clanH.HandlePairConfirm, "", potvrda(false, "")),
		"lokalno":           m.lokalno.zovi(t, clanH.HandlePairConfirm, "", potvrda(true, "")),
	} {
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nema uparivanja") {
			t.Errorf("član %s: %d %q", ime, w.Code, w.Body.String())
		}
	}
}

// Čarobnjak uparivanja: izvana svaka potvrda uz lozinku, i na računalu
// koje nije ni u jednoj mreži i na članu koji ne prima u mrežu, a iz
// lokalne mreže bez nje
func TestCarobnjakUlazakIzvanaUzLozinku(t *testing.T) {
	o := novaOkolinaUparivanja(t)
	o.pair.peers = svjeziCvorZaWeb(t, "samotnjak")
	o.pair.primaClanove = func() bool { return false }
	o.pair.lozinkaRacuna = o.auth.ProvjeriLozinku
	o.racun("sef", "sefova-lozinka", true, false)
	sef := o.sesija("sef", "sefova-lozinka")
	tunel := krozTunel("198.51.100.7")
	potvrdi := func(porijeklo func(*http.Request) *http.Request, tijelo string) int {
		o.pair.potvrdi = func(_ context.Context, approved, _ bool) (peers.PairOutcome, error) {
			return peers.PairOutcome{Paired: approved}, nil
		}
		r := porijeklo(httptest.NewRequest(http.MethodPost, "/api/uparivanje/confirm", strings.NewReader(tijelo)))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sef.ID.String()})
		w := httptest.NewRecorder()
		(&Server{}).klijentSloj(o.pair.Gate(http.HandlerFunc(o.pair.HandleConfirm))).ServeHTTP(w, r)
		return w.Code
	}
	for ime, c := range map[string]struct {
		porijeklo func(*http.Request) *http.Request
		tijelo    string
		kod       int
	}{
		"izvana bez lozinke": {tunel, `{"approved":true}`, http.StatusForbidden},
		"izvana s lozinkom":  {tunel, `{"approved":true,"lozinka_racuna":"sefova-lozinka"}`, http.StatusOK},
		"iz lokalne mreže":   {izravno, `{"approved":true}`, http.StatusOK},
	} {
		if code := potvrdi(c.porijeklo, c.tijelo); code != c.kod {
			t.Errorf("%s: %d, a treba %d", ime, code, c.kod)
		}
	}

	ov := ovlastUparivanja{korisnik: &models.User{}}
	clan := primateljNaDaljinu(t)
	for ime, c := range map[string]struct {
		peers     *peers.Service
		porijeklo func(*http.Request) *http.Request
		treba     bool
	}{
		"izvan mreže izvana":   {o.pair.peers, tunel, true},
		"izvan mreže lokalno":  {o.pair.peers, izravno, false},
		"u mreži bez primanja": {clan, tunel, true},
		"u mreži lokalno":      {clan, izravno, false},
		"bez razmjene":         {nil, tunel, true},
	} {
		o.pair.peers = c.peers
		zahtjevSPorijeklom(c.porijeklo, func(r *http.Request) {
			if got := o.pair.primaIzvana(r, ov); got != c.treba {
				t.Errorf("%s: traži lozinku %v", ime, got)
			}
		})
	}
}

// Postavke izvana nude polja za lozinku uz osnivanje mreže, ulazak u mrežu,
// adrese i poznate čvorove, a iz lokalne mreže ne
func TestPostavkeIzvanaPoljaLozinkeIzvanMreze(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	polja := []string{`id="mreza-lozinka"`, `id="pair-lozinka"`, `id="adresa-lozinka"`, `id="cvor-lozinka"`,
		`lozinka_racuna: lozinkaRacuna('cvor-lozinka')`, `lozinka_racuna: lozinkaRacuna('adresa-lozinka')`}
	for _, izvana := range []bool{true, false} {
		var b bytes.Buffer
		data := SettingsPageData{CurrentUser: adminIvic, Permissions: admin, Izvana: izvana,
			Peers: []peers.Peer{{NodeID: "laptop-ilok", Name: "Laptop Ilok"}}}
		if err := predlozakStranice(t, "settings.html").ExecuteTemplate(&b, "settings.html", data); err != nil {
			t.Fatal(err)
		}
		for _, p := range polja[:4] {
			if strings.Contains(b.String(), p) != izvana {
				t.Errorf("izvana=%v: polje %s", izvana, p)
			}
		}
		for _, p := range polja[4:] {
			if !strings.Contains(b.String(), p) {
				t.Errorf("poziv bez %s", p)
			}
		}
	}
}
