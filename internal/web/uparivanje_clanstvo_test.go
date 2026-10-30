package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/service"
)

// potvrdaUparivanja šalje odluku o kodu kroz Gate i vraća status, odgovor i
// je li odluka stigla do razmjene s pravom primanja
func (o *okolinaUparivanja) potvrdaUparivanja(porijeklo func(*http.Request) *http.Request, sesija *models.Session) (int, map[string]any, *bool) {
	o.t.Helper()
	var primio *bool
	o.pair.potvrdi = func(_ context.Context, approved, primi bool) (peers.PairOutcome, error) {
		primio = &primi
		return peers.PairOutcome{Paired: approved, Member: approved && primi}, nil
	}
	h := (&Server{}).klijentSloj(o.pair.Gate(http.HandlerFunc(o.pair.HandleConfirm)))
	r := porijeklo(httptest.NewRequest(http.MethodPost, "/api/uparivanje/confirm", strings.NewReader(`{"approved":true}`)))
	r.Header.Set("Content-Type", "application/json")
	if sesija != nil {
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var odgovor map[string]any
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &odgovor); err != nil {
			o.t.Fatalf("odgovor potvrde: %v (%s)", err, w.Body.String())
		}
	}
	return w.Code, odgovor, primio
}

// Bez prijave se svjež čvor samo upari: potvrda koda ne daje drugom
// računalu članstvo u mreži. Prijavljeni administrator ga i dalje prima.
func TestPotvrdaBezPrijaveNeDajeClanstvo(t *testing.T) {
	o := novaOkolinaUparivanja(t)
	o.pair.primaClanove = func() bool { return false }

	code, odgovor, primi := o.potvrdaUparivanja(izravnoSKodom, nil)
	if code != http.StatusOK || primi == nil {
		t.Fatalf("svjež čvor bez prijave mora moći upariti: %d", code)
	}
	if *primi || odgovor["member"] != false || odgovor["paired"] != true {
		t.Errorf("bez prijave drugo računalo ne smije postati član: primi=%v %v", *primi, odgovor)
	}

	o.racun("sef", "sefova-lozinka", true, false)
	code, odgovor, primi = o.potvrdaUparivanja(izravno, o.sesija("sef", "sefova-lozinka"))
	if code != http.StatusOK || primi == nil || !*primi || odgovor["member"] != true {
		t.Errorf("prijavljeni administrator prima u mrežu: %d %v", code, odgovor)
	}
}

// Čvor koji drži ključ mreže ili ovlast za primanje ne otvara uparivanje bez
// prijave ni kad ima samo jedan račun; administrator i dalje uparuje i prima
func TestCvorKojiPrimaNeUparujeBezPrijave(t *testing.T) {
	o := novaOkolinaUparivanja(t)
	prima := true
	o.pair.primaClanove = func() bool { return prima }

	if code, _, primi := o.potvrdaUparivanja(izravnoSKodom, nil); code != http.StatusForbidden || primi != nil {
		t.Errorf("čvor s ključem mreže bez prijave: %d, potvrda stigla do razmjene: %v", code, primi != nil)
	}
	if code, ov := o.zahtjev(izravnoSKodom, nil, "/api/uparivanje/listen"); code != http.StatusForbidden || ov != nil {
		t.Errorf("čekanje uparivanja bez prijave na čvoru s ključem: %d", code)
	}
	o.racun("sef", "sefova-lozinka", true, false)
	if code, _, primi := o.potvrdaUparivanja(izravno, o.sesija("sef", "sefova-lozinka")); code != http.StatusOK || primi == nil || !*primi {
		t.Errorf("administrator na čvoru s ključem: %d", code)
	}

	prima = false
	if code, _, _ := o.potvrdaUparivanja(izravnoSKodom, nil); code != http.StatusOK {
		t.Errorf("svjež čvor bez ključa i ovlasti uparuje bez prijave: %d", code)
	}
}

// Neuspio poziv drugog računala: iz lokalne mreže vidi se greška veze, a
// izvana opća poruka
func TestPorukaPozivaIzvanaJeOpca(t *testing.T) {
	greska := errors.New("dial tcp 10.1.2.3:22: connect: connection refused")
	poruka := func(porijeklo func(*http.Request) *http.Request) string {
		var p string
		h := (&Server{}).klijentSloj(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p = porukaPoziva(r, greska)
		}))
		h.ServeHTTP(httptest.NewRecorder(), porijeklo(httptest.NewRequest(http.MethodPost, "/api/uparivanje/dial", nil)))
		return p
	}
	if p := poruka(izravno); p != greska.Error() {
		t.Errorf("iz lokalne mreže: %q", p)
	}
	for ime, porijeklo := range map[string]func(*http.Request) *http.Request{
		"kroz tunel": krozTunel("198.51.100.7"), "s javne adrese": javnoIzravno,
	} {
		if p := poruka(porijeklo); strings.Contains(p, "10.1.2.3") || !strings.Contains(p, "nije uspostavljena") {
			t.Errorf("%s: %q", ime, p)
		}
	}
}

// S pravom razmjenom: čim čvor osnuje mrežu i drži njezin ključ, uparivanje
// bez prijave prestaje, a administratorova potvrda stiže do razmjene
func TestOsnovanaMrezaZatvaraUparivanjeBezPrijave(t *testing.T) {
	o := novaOkolinaPostavljanja(t)
	h := sKodomPostavljanja(NewPairHandler(o.mreza, o.auth, service.NewUserService(o.repo, o.auth, service.NewSSEBroker()), nil))
	srv := (&Server{}).klijentSloj(h.Gate(http.HandlerFunc(h.HandleConfirm)))
	potvrda := func(sesija *models.Session) *httptest.ResponseRecorder {
		r := izravnoSKodom(httptest.NewRequest(http.MethodPost, "/api/uparivanje/confirm", strings.NewReader(`{"approved":true}`)))
		r.Header.Set("Content-Type", "application/json")
		if sesija != nil {
			r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}

	// svjež čvor bez mreže: Gate pušta, a razmjena javlja da nema čekanja
	if w := potvrda(nil); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nema uparivanja") {
		t.Fatalf("svjež čvor bez mreže: %d %q", w.Code, w.Body.String())
	}
	if err := o.mreza.CreateNetwork(context.Background(), "COP Osijek"); err != nil {
		t.Fatal(err)
	}
	if w := potvrda(nil); w.Code != http.StatusForbidden {
		t.Errorf("čvor koji drži ključ mreže, bez prijave: %d %q", w.Code, w.Body.String())
	}
	o.racun("sef", "sefova-lozinka", true, false)
	sesija, _, err := o.auth.Login("sef", "sefova-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	if w := potvrda(sesija); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nema uparivanja") {
		t.Errorf("administrator na čvoru s ključem: %d %q", w.Code, w.Body.String())
	}
}

// Administrator izvana na neuspio poziv dobiva opću poruku, a iz lokalne
// mreže grešku veze; prazna adresa se odbija prije poziva
func TestPozivDrugogRacunalaIzvana(t *testing.T) {
	o := novaOkolinaPostavljanja(t)
	o.racun("sef", "sefova-lozinka", true, false)
	sesija, _, err := o.auth.Login("sef", "sefova-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	h := NewPairHandler(o.mreza, o.auth, service.NewUserService(o.repo, o.auth, service.NewSSEBroker()), nil)
	srv := (&Server{}).klijentSloj(h.Gate(http.HandlerFunc(h.HandleDial)))
	poziv := func(porijeklo func(*http.Request) *http.Request, adresa string) *httptest.ResponseRecorder {
		r := porijeklo(httptest.NewRequest(http.MethodPost, "/api/uparivanje/dial", strings.NewReader(`{"addr":"`+adresa+`"}`)))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}
	if w := poziv(izravno, " "); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "obavezna") {
		t.Errorf("prazna adresa: %d %q", w.Code, w.Body.String())
	}
	if w := poziv(izravno, "127.0.0.1:1"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "127.0.0.1:1") {
		t.Errorf("iz lokalne mreže: %d %q", w.Code, w.Body.String())
	}
	if w := poziv(krozTunel("198.51.100.7"), "127.0.0.1:1"); w.Code != http.StatusBadRequest ||
		strings.Contains(w.Body.String(), "127.0.0.1") || !strings.Contains(w.Body.String(), "nije uspostavljena") {
		t.Errorf("izvana: %d %q", w.Code, w.Body.String())
	}
}

// zahtjevSPorijeklom propušta zahtjev kroz sloj koji utvrđuje porijeklo i
// zove f s tim zahtjevom
func zahtjevSPorijeklom(porijeklo func(*http.Request) *http.Request, f func(*http.Request)) {
	h := (&Server{}).klijentSloj(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { f(r) }))
	h.ServeHTTP(httptest.NewRecorder(), porijeklo(httptest.NewRequest(http.MethodGet, "/api/uparivanje/status", nil)))
}

// Bilješke razmjene i greška čekanja uparivanja nose adrese iz lokalne
// mreže: izvana ih zamijeni opća poruka, a iz lokalne mreže ostaju cijele
func TestGreskeVezeIzvanaSuOpce(t *testing.T) {
	rez := map[string]string{"cop-osijek": "primljeno 3, poslano 1", "cop-vukovar": "greška: dial tcp 10.1.2.3:4710: connection refused"}
	st := peers.PairStatus{Waiting: true, Error: "tls: veza s 10.1.2.3:4711 prekinuta"}
	zahtjevSPorijeklom(izravno, func(r *http.Request) {
		if got := rezultatiRazmjene(r, rez); got["cop-vukovar"] != rez["cop-vukovar"] {
			t.Errorf("iz lokalne mreže: %v", got)
		}
		if got := stanjeUparivanja(r, st); got.Error != st.Error {
			t.Errorf("stanje iz lokalne mreže: %+v", got)
		}
	})
	zahtjevSPorijeklom(krozTunel("198.51.100.7"), func(r *http.Request) {
		got := rezultatiRazmjene(r, rez)
		if strings.Contains(got["cop-vukovar"], "10.1.2.3") || !strings.HasPrefix(got["cop-vukovar"], "greška: ") || got["cop-osijek"] != rez["cop-osijek"] {
			t.Errorf("izvana: %v", got)
		}
		if got := stanjeUparivanja(r, st); strings.Contains(got.Error, "10.1.2.3") || got.Error == "" || !got.Waiting {
			t.Errorf("stanje izvana: %+v", got)
		}
		if got := stanjeUparivanja(r, peers.PairStatus{Waiting: true}); got.Error != "" {
			t.Errorf("stanje izvana bez greške: %+v", got)
		}
	})
}

// Uparivanje i razmjena iz Postavki: administrator izvana na neuspio poziv
// ili razmjenu dobiva opću poruku, iz lokalne mreže cijelu grešku, a račun
// koji nije administrator ne zove ništa
func TestPozivIRazmjenaIzPostavkiIzvana(t *testing.T) {
	o := novaOkolinaPostavljanja(t)
	h := &SettingsHandler{peers: o.mreza}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	zovi := func(f http.HandlerFunc, porijeklo func(*http.Request) *http.Request, perms *models.UserPermissions, tijelo, cvor string) *httptest.ResponseRecorder {
		r := porijeklo(httptest.NewRequest(http.MethodPost, "/api/peers/pair/dial", strings.NewReader(tijelo)))
		r.Header.Set("Content-Type", "application/json")
		r.SetPathValue("node", cvor)
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, perms))
		w := httptest.NewRecorder()
		(&Server{}).klijentSloj(f).ServeHTTP(w, r)
		return w
	}
	dial := `{"addr":"127.0.0.1:1"}`
	if w := zovi(h.HandlePairDial, izravno, &models.UserPermissions{}, dial, ""); w.Code != http.StatusForbidden {
		t.Errorf("poziv bez administratora: %d", w.Code)
	}
	if w := zovi(h.HandlePairDial, izravno, admin, dial, ""); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "127.0.0.1:1") {
		t.Errorf("poziv iz lokalne mreže: %d %q", w.Code, w.Body.String())
	}
	if w := zovi(h.HandlePairDial, krozTunel("198.51.100.7"), admin, dial, ""); w.Code != http.StatusBadRequest ||
		strings.Contains(w.Body.String(), "127.0.0.1") || !strings.Contains(w.Body.String(), "nije uspostavljena") {
		t.Errorf("poziv izvana: %d %q", w.Code, w.Body.String())
	}
	if w := zovi(h.HandleSyncNow, izravno, &models.UserPermissions{}, "", "cop-vukovar"); w.Code != http.StatusForbidden {
		t.Errorf("razmjena bez administratora: %d", w.Code)
	}
	lokalno := zovi(h.HandleSyncNow, izravno, admin, "", "cop-vukovar")
	izvana := zovi(h.HandleSyncNow, krozTunel("198.51.100.7"), admin, "", "cop-vukovar")
	if lokalno.Code != http.StatusBadGateway || izvana.Code != http.StatusBadGateway ||
		strings.TrimSpace(izvana.Body.String()) != opcaPorukaRazmjene || lokalno.Body.String() == izvana.Body.String() {
		t.Errorf("razmjena s nepoznatim računalom: lokalno %d %q, izvana %d %q", lokalno.Code, lokalno.Body.String(), izvana.Code, izvana.Body.String())
	}
	if w := zovi(h.HandleSyncNow, krozTunel("198.51.100.7"), admin, "", "all"); w.Code != http.StatusOK {
		t.Errorf("razmjena sa svima izvana: %d %q", w.Code, w.Body.String())
	}
	if w := zovi(h.HandlePairStatus, krozTunel("198.51.100.7"), admin, "", ""); w.Code != http.StatusOK {
		t.Errorf("stanje uparivanja izvana: %d", w.Code)
	}
}

// Čvor primljen na daljinu s ovlašću za primanje drži tu ovlast odmah, s
// pravom razmjenom: uparivanje bez prijave tada prestaje, a administrator i
// dalje uparuje
func TestOvlastZaPrimanjeZatvaraUparivanjeBezPrijave(t *testing.T) {
	o := novaOkolinaPostavljanja(t)
	ured := primateljNaDaljinu(t)
	h := sKodomPostavljanja(NewPairHandler(o.mreza, o.auth, service.NewUserService(o.repo, o.auth, service.NewSSEBroker()), nil))
	srv := (&Server{}).klijentSloj(h.Gate(http.HandlerFunc(h.HandleStatus)))
	status := func(sesija *models.Session) int {
		r := izravnoSKodom(httptest.NewRequest(http.MethodGet, "/api/uparivanje/status", nil))
		if sesija != nil {
			r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w.Code
	}
	if code := status(nil); code != http.StatusOK {
		t.Fatalf("svjež čvor bez mreže: %d", code)
	}
	zahtjev, kod, err := o.mreza.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	potvrda, err := ured.PrimiZahtjev(context.Background(), zahtjev, kod, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.mreza.UveziPotvrdu(context.Background(), potvrda); err != nil {
		t.Fatal(err)
	}
	if n := o.mreza.NetworkInfo(); n == nil || !n.CanAdmit {
		t.Fatalf("čvor primljen s ovlašću: %+v", n)
	}
	if code := status(nil); code != http.StatusForbidden {
		t.Errorf("čvor s ovlašću za primanje, bez prijave: %d", code)
	}
	o.racun("sef", "sefova-lozinka", true, false)
	sesija, _, err := o.auth.Login("sef", "sefova-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	if code := status(sesija); code != http.StatusOK {
		t.Errorf("administrator na čvoru s ovlašću: %d", code)
	}
}
