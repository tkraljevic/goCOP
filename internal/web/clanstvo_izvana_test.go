package web

// Članstvo u mreži izvana (Odluke (30. 10. 2026.), 1): primanje računala,
// ovlast za primanje i opoziv članstva izvana traže lozinku računa, a s
// ovog računala i iz lokalne mreže ne. Tko je to napravio i s koje adrese,
// piše u dnevniku programa. Tuđim očima se mreža ne mijenja.

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/service"
)

// dnevnikPrograma hvata dnevnik programa do kraja testa
func dnevnikPrograma(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(stari) })
	return &b
}

// zahtjevMreze je poziv rukovatelja mreže s porijeklom, ovlastima i osobom
type zahtjevMreze struct {
	porijeklo func(*http.Request) *http.Request
	perms     *models.UserPermissions
	osoba     *models.User
	tudjim    bool
}

func (z zahtjevMreze) zovi(t *testing.T, f http.HandlerFunc, cvor string, tijelo any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(tijelo)
	if err != nil {
		t.Fatal(err)
	}
	r := z.porijeklo(httptest.NewRequest(http.MethodPost, "/api/network/x", bytes.NewReader(b)))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("node", cvor)
	ctx := context.WithValue(r.Context(), contextKeyPerms, z.perms)
	ctx = context.WithValue(ctx, contextKeyUser, z.osoba)
	ctx = context.WithValue(ctx, contextKeyViewing, z.tudjim)
	w := httptest.NewRecorder()
	(&Server{}).klijentSloj(f).ServeHTTP(w, r.WithContext(ctx))
	return w
}

// lozinkaTesta prihvaća samo "anina-lozinka"
func lozinkaTesta(_ uuid.UUID, lozinka string) error {
	if lozinka != "anina-lozinka" {
		return service.ErrKrivaLozinka
	}
	return nil
}

func TestClanstvoIzvanaTraziLozinku(t *testing.T) {
	dnevnik := dnevnikPrograma(t)
	ured := primateljNaDaljinu(t)
	h := &SettingsHandler{peers: ured}
	h.SetZastitaKljuca(lozinkaTesta)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	ana := &models.User{ID: uuid.New(), Username: "aivic", FullName: "Ana Ivić"}
	izvana := zahtjevMreze{porijeklo: krozTunel("198.51.100.7"), perms: admin, osoba: ana}
	javno := zahtjevMreze{porijeklo: javnoIzravno, perms: admin, osoba: ana}
	lokalno := zahtjevMreze{porijeklo: izravno, perms: admin, osoba: ana}
	tudjim := zahtjevMreze{porijeklo: izravno, perms: admin, osoba: ana, tudjim: true}

	f := svjeziCvorZaWeb(t, "pperic-thinkpad")
	zahtjev, kod, err := f.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	primi := func(lozinka string) map[string]any {
		return map[string]any{"zahtjev": string(zahtjev), "kod": kod, "ovlast": true, "lozinka_racuna": lozinka}
	}
	for ime, c := range map[string]struct {
		w   *httptest.ResponseRecorder
		ima string
	}{
		"izvana bez lozinke":     {izvana.zovi(t, h.HandlePrimiZahtjev, "", primi("")), "uz lozinku vašeg računa"},
		"s javne adrese":         {javno.zovi(t, h.HandlePrimiZahtjev, "", primi("")), "uz lozinku vašeg računa"},
		"izvana krivom lozinkom": {izvana.zovi(t, h.HandlePrimiZahtjev, "", primi("kriva")), "lozinka nije točna"},
		"tuđim očima":            {tudjim.zovi(t, h.HandlePrimiZahtjev, "", primi("")), "tuđim očima"},
	} {
		if c.w.Code != http.StatusForbidden || !strings.Contains(c.w.Body.String(), c.ima) {
			t.Errorf("primanje %s: %d %q", ime, c.w.Code, c.w.Body.String())
		}
	}
	w := izvana.zovi(t, h.HandlePrimiZahtjev, "", primi("anina-lozinka"))
	var odg struct {
		Potvrda string `json:"potvrda"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &odg); w.Code != http.StatusOK || err != nil {
		t.Fatalf("primanje izvana s lozinkom: %d %s", w.Code, w.Body.String())
	}
	if _, err := f.UveziPotvrdu(context.Background(), []byte(odg.Potvrda)); err != nil {
		t.Fatal(err)
	}

	// ovlast za primanje i opoziv: izvana uz lozinku, lokalno bez nje
	tijelo := func(lozinka string) map[string]string { return map[string]string{"lozinka_racuna": lozinka} }
	for _, f := range []http.HandlerFunc{h.HandleOpozoviOvlast, h.HandleIzdajOvlast, h.HandleRevokeMember} {
		if w := izvana.zovi(t, f, "pperic-thinkpad", tijelo("")); w.Code != http.StatusForbidden {
			t.Errorf("izvana bez lozinke: %d %q", w.Code, w.Body.String())
		}
		if w := tudjim.zovi(t, f, "pperic-thinkpad", tijelo("")); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "tuđim očima") {
			t.Errorf("tuđim očima: %d %q", w.Code, w.Body.String())
		}
		if w := lokalno.zovi(t, f, "pperic-thinkpad", "nije objekt"); w.Code != http.StatusBadRequest {
			t.Errorf("neispravno tijelo: %d", w.Code)
		}
	}
	if w := izvana.zovi(t, h.HandleOpozoviOvlast, "pperic-thinkpad", tijelo("anina-lozinka")); w.Code != http.StatusOK {
		t.Errorf("oduzimanje ovlasti izvana s lozinkom: %d %q", w.Code, w.Body.String())
	}
	if w := lokalno.zovi(t, h.HandleIzdajOvlast, "pperic-thinkpad", tijelo("")); w.Code != http.StatusOK {
		t.Errorf("ovlast iz lokalne mreže bez lozinke: %d %q", w.Code, w.Body.String())
	}
	if w := javno.zovi(t, h.HandleRevokeMember, "pperic-thinkpad", tijelo("anina-lozinka")); w.Code != http.StatusOK {
		t.Errorf("opoziv s javne adrese s lozinkom: %d %q", w.Code, w.Body.String())
	}
	for _, d := range []string{
		`mreža: "Ana Ivić (aivic)" primio je u mrežu na daljinu računalo "pperic-thinkpad" i dao mu ovlast za primanje, s adrese`,
		`"Ana Ivić (aivic)" oduzeo je ovlast za primanje računalu "pperic-thinkpad", s adrese`,
		`"Ana Ivić (aivic)" dao je ovlast za primanje računalu "pperic-thinkpad", s adrese 192.168.1.50`,
		`"Ana Ivić (aivic)" opozvao je članstvo računala "pperic-thinkpad", s adrese 203.0.113.9`,
		`mreža: izvana bez točne lozinke računa odbijeno: "Ana Ivić (aivic)", POST "/api/network/x", s adrese`,
	} {
		if !strings.Contains(dnevnik.String(), d) {
			t.Errorf("dnevnik programa nema %q:\n%s", d, dnevnik.String())
		}
	}
	if strings.Contains(dnevnik.String(), "anina-lozinka") {
		t.Error("lozinka je u dnevniku programa")
	}
}

// Primanje na daljinu bez ovlasti ide u dnevnik bez spomena ovlasti
func TestOpisPrimanja(t *testing.T) {
	if got := opisPrimanja(false); got != "primio je u mrežu na daljinu računalo %q" {
		t.Errorf("bez ovlasti: %q", got)
	}
}

// Ime osobe i oznaka računala pišu se u navodnicima, pa ni prijelom retka u
// njima ne započinje novi redak dnevnika programa
func TestZapisClanstvaUJednomRetku(t *testing.T) {
	dnevnik := dnevnikPrograma(t)
	u := &models.User{Username: "aivic", FullName: "Ana\nmreža: netko"}
	zahtjevSPorijeklom(izravno, func(r *http.Request) {
		zapisiClanstvo(r, u, "primio je u mrežu računalo %q uparivanjem", "laptop\nmreža: x")
		zapisiClanstvo(r, nil, "opozvao je članstvo računala %q", "cop-ilok")
	})
	if n := strings.Count(dnevnik.String(), "\n"); n != 2 {
		t.Errorf("zapis u %d redaka, a trebaju dva:\n%s", n, dnevnik.String())
	}
	if !strings.Contains(dnevnik.String(), `mreža: "nepoznata osoba" opozvao je članstvo računala "cop-ilok"`) {
		t.Errorf("zapis bez osobe:\n%s", dnevnik.String())
	}
}

// Potvrda koda u Postavkama na računalu koje prima u mrežu: izvana uz
// lozinku, a odbijanje koda i potvrda iz lokalne mreže bez nje
func TestPotvrdaUparivanjaIzvanaUzLozinku(t *testing.T) {
	ured := primateljNaDaljinu(t)
	h := &SettingsHandler{peers: ured}
	h.SetZastitaKljuca(lozinkaTesta)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	ana := &models.User{ID: uuid.New(), Username: "aivic", FullName: "Ana Ivić"}
	izvana := zahtjevMreze{porijeklo: krozTunel("198.51.100.7"), perms: admin, osoba: ana}
	lokalno := zahtjevMreze{porijeklo: izravno, perms: admin, osoba: ana}
	potvrda := func(approved bool, lozinka string) map[string]any {
		return map[string]any{"approved": approved, "lozinka_racuna": lozinka}
	}
	if w := izvana.zovi(t, h.HandlePairConfirm, "", potvrda(true, "")); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "lozinku vašeg računa") {
		t.Errorf("potvrda izvana bez lozinke: %d %q", w.Code, w.Body.String())
	}
	// do razmjene stiže: nema uparivanja koje čeka odluku
	for ime, w := range map[string]*httptest.ResponseRecorder{
		"izvana s lozinkom": izvana.zovi(t, h.HandlePairConfirm, "", potvrda(true, "anina-lozinka")),
		"izvana odbijanje":  izvana.zovi(t, h.HandlePairConfirm, "", potvrda(false, "")),
		"lokalno":           lokalno.zovi(t, h.HandlePairConfirm, "", potvrda(true, "")),
	} {
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nema uparivanja") {
			t.Errorf("%s: %d %q", ime, w.Code, w.Body.String())
		}
	}
	if w := (zahtjevMreze{porijeklo: izravno, perms: admin, osoba: ana, tudjim: true}).zovi(t, h.HandlePairConfirm, "", potvrda(true, "")); w.Code != http.StatusForbidden {
		t.Errorf("potvrda tuđim očima: %d", w.Code)
	}
	if w := lokalno.zovi(t, h.HandlePairConfirm, "", "nije objekt"); w.Code != http.StatusBadRequest {
		t.Errorf("potvrda s neispravnim tijelom: %d", w.Code)
	}
	if w := lokalno.zovi(t, h.HandleRevokeMember, "cop-osijek", map[string]string{}); w.Code != http.StatusBadRequest {
		t.Errorf("opoziv samog sebe: %d %q", w.Code, w.Body.String())
	}
}

// Čarobnjak uparivanja: prijavljeni administrator izvana na računalu koje
// prima u mrežu potvrđuje kod uz lozinku računa; potvrda ide u dnevnik
func TestCarobnjakIzvanaUzLozinku(t *testing.T) {
	dnevnik := dnevnikPrograma(t)
	o := novaOkolinaUparivanja(t)
	o.pair.primaClanove = func() bool { return true }
	o.pair.lozinkaRacuna = o.auth.ProvjeriLozinku
	o.racun("sef", "sefova-lozinka", true, false)
	sef := o.sesija("sef", "sefova-lozinka")
	tunel := krozTunel("198.51.100.7")

	// drugaJeVecClan: druga strana je već član, pa potvrda nikoga ne prima
	drugaJeVecClan := false
	potvrdi := func(porijeklo func(*http.Request) *http.Request, tijelo string) (int, *bool) {
		var primio *bool
		o.pair.potvrdi = func(_ context.Context, approved, primi bool) (peers.PairOutcome, error) {
			primio = &primi
			return peers.PairOutcome{Paired: approved, Member: approved, Primljen: approved && primi && !drugaJeVecClan}, nil
		}
		r := porijeklo(httptest.NewRequest(http.MethodPost, "/api/uparivanje/confirm", strings.NewReader(tijelo)))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sef.ID.String()})
		w := httptest.NewRecorder()
		(&Server{}).klijentSloj(o.pair.Gate(http.HandlerFunc(o.pair.HandleConfirm))).ServeHTTP(w, r)
		return w.Code, primio
	}
	if code, primio := potvrdi(tunel, `{"approved":true}`); code != http.StatusForbidden || primio != nil {
		t.Errorf("izvana bez lozinke: %d, do razmjene: %v", code, primio != nil)
	}
	if code, primio := potvrdi(tunel, `{"approved":true,"lozinka_racuna":"kriva"}`); code != http.StatusForbidden || primio != nil {
		t.Errorf("izvana s krivom lozinkom: %d", code)
	}
	if code, primio := potvrdi(tunel, `{"approved":true,"lozinka_racuna":"sefova-lozinka"}`); code != http.StatusOK || primio == nil || !*primio {
		t.Errorf("izvana s lozinkom: %d", code)
	}
	if code, primio := potvrdi(izravno, `{"approved":true}`); code != http.StatusOK || primio == nil || !*primio {
		t.Errorf("iz lokalne mreže bez lozinke: %d", code)
	}
	if code, _ := potvrdi(tunel, `{"approved":false}`); code != http.StatusOK {
		t.Errorf("odbijanje izvana bez lozinke: %d", code)
	}
	drugaJeVecClan = true
	if code, _ := potvrdi(izravno, `{"approved":true}`); code != http.StatusOK {
		t.Errorf("uparivanje s članom: %d", code)
	}
	if n := strings.Count(dnevnik.String(), "primio je u mrežu računalo"); n != 2 {
		t.Errorf("u dnevniku %d primanja, a trebaju dva:\n%s", n, dnevnik.String())
	}

	// stranica čarobnjaka traži lozinku samo izvana
	ov := ovlastUparivanja{korisnik: &models.User{}}
	for ime, c := range map[string]struct {
		porijeklo func(*http.Request) *http.Request
		o         ovlastUparivanja
		treba     bool
	}{
		"izvana": {tunel, ov, true}, "lokalno": {izravno, ov, false}, "bez prijave": {tunel, ovlastUparivanja{}, false},
	} {
		zahtjevSPorijeklom(c.porijeklo, func(r *http.Request) {
			if got := o.pair.primaIzvana(r, c.o); got != c.treba {
				t.Errorf("%s: traži lozinku %v", ime, got)
			}
		})
	}
}

// Uparivanje bez prijave na svježem računalu: s tog računala bez koda, iz
// lokalne mreže samo s kodom iz dnevnika, kroz tunel nikad (Odluke
// (30. 10. 2026.), 15). Vrijedi i kad čvor ne radi pod Postavom.
func TestUparivanjeBezPrijaveTraziKod(t *testing.T) {
	o := novaOkolinaUparivanja(t)
	const api = "/api/uparivanje/status"
	sKodom := func(kod string) func(*http.Request) *http.Request {
		return func(r *http.Request) *http.Request {
			r.Header.Set(zaglavljeKoda, kod)
			return izravno(r)
		}
	}
	ovoRacunalo := izravnoS("127.0.0.1:40000")
	for ime, c := range map[string]struct {
		porijeklo func(*http.Request) *http.Request
		kod       int
	}{
		"iz lokalne mreže bez koda": {izravno, http.StatusForbidden},
		"s krivim kodom":            {sKodom("AAAA-BBBB"), http.StatusForbidden},
		"kroz tunel s kodom": {func(r *http.Request) *http.Request {
			r.Header.Set(zaglavljeKoda, testniKodPostavljanja)
			return krozTunel("198.51.100.7")(r)
		}, http.StatusForbidden},
		"s kodom":                  {sKodom(testniKodPostavljanja), http.StatusOK},
		"kod malim slovima":        {sKodom("7kq4-m2xd"), http.StatusOK},
		"s ovog računala bez koda": {ovoRacunalo, http.StatusOK},
	} {
		if code, _ := o.zahtjev(c.porijeklo, nil, api); code != c.kod {
			t.Errorf("%s: %d, a treba %d", ime, code, c.kod)
		}
	}
	// stranica: kod u adresi; bez njega iz lokalne mreže vodi na Postavljanje
	if code, ov := o.zahtjev(izravnoS("192.168.1.50:40000"), nil, "/uparivanje?kod="+testniKodPostavljanja); code != http.StatusOK || ov == nil {
		t.Errorf("stranica s kodom u adresi: %d", code)
	}
	h := o.pair.Gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	w := httptest.NewRecorder()
	(&Server{}).klijentSloj(h).ServeHTTP(w, izravno(httptest.NewRequest(http.MethodGet, "/uparivanje", nil)))
	if loc := w.Header().Get("Location"); w.Code != http.StatusSeeOther || loc != "/postavljanje?put=postojeca" {
		t.Errorf("stranica bez koda: %d %q", w.Code, loc)
	}
	w = httptest.NewRecorder()
	(&Server{}).klijentSloj(h).ServeHTTP(w, krozTunel("198.51.100.7")(httptest.NewRequest(http.MethodGet, "/uparivanje", nil)))
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("stranica kroz tunel vodi na prijavu: %q", loc)
	}
	// bez provjere koda nitko ne uparuje bez prijave
	o.pair.kodPostavljanja = nil
	if code, _ := o.zahtjev(ovoRacunalo, nil, api); code != http.StatusForbidden {
		t.Errorf("bez provjere koda: %d", code)
	}
}

// Stranica čarobnjaka nosi kod iz dnevnika u pozive i polje za lozinku kad
// ga traži; Postavljanje kod prenosi na čarobnjak
func TestPredlosciUparivanjaKodILozinka(t *testing.T) {
	var b bytes.Buffer
	if err := predlozakStranice(t, "uparivanje.html").ExecuteTemplate(&b, "uparivanje.html", PairPageData{Kod: testniKodPostavljanja, TraziLozinku: true}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{`const KOD = "7KQ4-M2XD"`, `'X-Kod-Postavljanja'`, `id="lozinka-racuna"`} {
		if !strings.Contains(b.String(), d) {
			t.Errorf("čarobnjak nema %q", d)
		}
	}
	b.Reset()
	if err := predlozakStranice(t, "postavljanje.html").ExecuteTemplate(&b, "postavljanje.html", PostavljanjeData{Dopusteno: true, Kod: testniKodPostavljanja}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `href="/uparivanje?kod=7KQ4-M2XD"`) {
		t.Error("Postavljanje ne prenosi kod na čarobnjak")
	}
}

// Postavke izvana nude polja za lozinku računa uz obrasce članstva, a iz
// lokalne mreže ne
func TestPostavkeIzvanaPoljaLozinke(t *testing.T) {
	ured := primateljNaDaljinu(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	polja := []string{`id="clan-lozinka"`, `id="zahtjev-lozinka"`, `id="pair-lozinka"`}
	for _, izvana := range []bool{true, false} {
		var b bytes.Buffer
		data := SettingsPageData{CurrentUser: adminIvic, Permissions: admin, Network: ured.NetworkInfo(), Izvana: izvana}
		if err := predlozakStranice(t, "settings.html").ExecuteTemplate(&b, "settings.html", data); err != nil {
			t.Fatal(err)
		}
		for _, p := range polja {
			if strings.Contains(b.String(), p) != izvana {
				t.Errorf("izvana=%v: polje %s", izvana, p)
			}
		}
	}
}

// Radnje nad mrežom tuđim očima se odbijaju, a administrator svojim očima
// prolazi do razmjene; adrese se ne mijenjaju kroz oznaku stalno izložen
func TestMrezaSeNeMijenjaTudjimOcima(t *testing.T) {
	ured := primateljNaDaljinu(t)
	h := &SettingsHandler{peers: ured}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	ana := &models.User{ID: uuid.New(), Username: "aivic", FullName: "Ana Ivić"}
	tudjim := zahtjevMreze{porijeklo: izravno, perms: admin, osoba: ana, tudjim: true}
	svojim := zahtjevMreze{porijeklo: izravno, perms: admin, osoba: ana}
	for ime, f := range map[string]http.HandlerFunc{
		"čekanje": h.HandlePairListen, "prekid": h.HandlePairStop, "poziv": h.HandlePairDial,
		"osnivanje": h.HandleCreateNetwork, "zaborav": h.HandleForgetPeer, "adrese": h.HandlePublicAddress,
		"izloženost": h.HandleSetBootstrap, "obnova": h.HandleObnoviClanstvo, "obnova člana": h.HandleObnoviClana,
	} {
		if w := tudjim.zovi(t, f, "laptop-ilok", map[string]string{}); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "tuđim očima") {
			t.Errorf("%s tuđim očima: %d %q", ime, w.Code, w.Body.String())
		}
	}
	if w := svojim.zovi(t, h.HandleForgetPeer, "cop-osijek", map[string]string{}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "samo sebe") {
		t.Errorf("zaborav samog sebe: %d %q", w.Code, w.Body.String())
	}
	if err := ured.SavePeer(context.Background(), peers.Peer{NodeID: "laptop-ilok", Name: "Laptop Ilok", PublicKey: svjeziCvorZaWeb(t, "laptop-ilok").Node().PublicKey(), Addresses: []string{"192.168.1.30:4710"}}); err != nil {
		t.Fatal(err)
	}
	w := svojim.zovi(t, h.HandleSetBootstrap, "laptop-ilok", map[string]any{"bootstrap": true, "addresses": []string{"drugi.primjer.hr:4710"}})
	if w := svojim.zovi(t, h.HandleSetBootstrap, "nepoznat", map[string]any{"bootstrap": true}); w.Code != http.StatusNotFound {
		t.Errorf("stalno izložen nepoznat čvor: %d", w.Code)
	}
	if w := svojim.zovi(t, h.HandleSetBootstrap, "laptop-ilok", "nije objekt"); w.Code != http.StatusBadRequest {
		t.Errorf("stalno izložen s neispravnim tijelom: %d", w.Code)
	}
	p, err := ured.GetPeer(context.Background(), "laptop-ilok")
	if w.Code != http.StatusOK || err != nil || p == nil || !p.IsBootstrap || len(p.Addresses) != 1 || p.Addresses[0] != "192.168.1.30:4710" {
		t.Errorf("stalno izložen: %d %+v %v", w.Code, p, err)
	}
}

// Krivi upisi lozinke izvana broje se kao pri promjeni lozinke: nakon tri
// i točna lozinka dobiva 403, a iz lokalne mreže radnja i dalje prolazi
func TestClanstvoIzvanaBrojiKriveLozinke(t *testing.T) {
	ured := primateljNaDaljinu(t)
	h := &SettingsHandler{peers: ured}
	h.SetZastitaKljuca(lozinkaTesta)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	ana := &models.User{ID: uuid.New(), Username: "aivic", FullName: "Ana Ivić"}
	izvana := zahtjevMreze{porijeklo: krozTunel("198.51.100.7"), perms: admin, osoba: ana}
	lokalno := zahtjevMreze{porijeklo: izravno, perms: admin, osoba: ana}
	t.Cleanup(func() { ponovnaLozinka.Reset(kljucPonovneLozinke("", ana.ID.String())) })
	for range 3 {
		izvana.zovi(t, h.HandleOpozoviOvlast, "laptop-ilok", map[string]string{"lozinka_racuna": "kriva"})
	}
	if w := izvana.zovi(t, h.HandleOpozoviOvlast, "laptop-ilok", map[string]string{"lozinka_racuna": "anina-lozinka"}); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "previše krivih lozinki") {
		t.Errorf("točna lozinka nakon tri krive: %d %q", w.Code, w.Body.String())
	}
	if w := lokalno.zovi(t, h.HandleOpozoviOvlast, "laptop-ilok", map[string]string{}); w.Code == http.StatusForbidden {
		t.Errorf("iz lokalne mreže: %d %q", w.Code, w.Body.String())
	}
}

// Stranica čarobnjaka: bez prijave nosi kod iz dnevnika za pozive, a
// administratoru izvana na računalu koje prima u mrežu nudi polje lozinke
func TestStranicaCarobnjaka(t *testing.T) {
	h := &PairHandler{peers: primateljNaDaljinu(t), tmpl: predlozakStranice(t, "uparivanje.html")}
	h.brojiRacun = func() (int, error) { return 1, nil }
	h.primaClanove = func() bool { return true }
	prikazi := func(porijeklo func(*http.Request) *http.Request, adresa string, o ovlastUparivanja) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		zahtjevSPorijeklom(porijeklo, func(r *http.Request) {
			u, err := r.URL.Parse(adresa)
			if err != nil {
				t.Fatal(err)
			}
			r.URL = u
			h.ShowWizard(w, r.WithContext(context.WithValue(r.Context(), kljucUparivanja{}, o)))
		})
		return w
	}
	w := prikazi(izravno, "/uparivanje?kod="+testniKodPostavljanja, ovlastUparivanja{})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `const KOD = "7KQ4-M2XD"`) || strings.Contains(w.Body.String(), `id="lozinka-racuna"`) {
		t.Errorf("bez prijave s kodom: %d", w.Code)
	}
	w = prikazi(krozTunel("198.51.100.7"), "/uparivanje?kod=AAAA-BBBB", ovlastUparivanja{korisnik: &models.User{Username: "sef"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="lozinka-racuna"`) || strings.Contains(w.Body.String(), "AAAA-BBBB") {
		t.Errorf("administrator izvana: %d", w.Code)
	}
	h.tmpl = predlozakStranice(t, "postavljanje.html")
	if w := prikazi(izravno, "/uparivanje", ovlastUparivanja{}); w.Code != http.StatusInternalServerError {
		t.Errorf("bez predloška: %d", w.Code)
	}
}

// Prijavljeni s početnom lozinkom ide s čarobnjaka na promjenu lozinke
func TestCarobnjakTraziPromjenuLozinke(t *testing.T) {
	o := novaOkolinaUparivanja(t)
	o.racuna = 2
	o.racun("novi", "pocetna-lozinka", true, true)
	if code, _ := o.zahtjev(izravno, o.sesija("novi", "pocetna-lozinka"), "/uparivanje"); code != http.StatusSeeOther {
		t.Errorf("stranica uz početnu lozinku: %d", code)
	}
}
