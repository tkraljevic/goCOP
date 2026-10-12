package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// posluziteljPogleda stavlja pravu provjeru sesije ispred rukovatelja
// pregleda tuđim očima, kao u programu
func posluziteljPogleda(o *okolinaPrijave) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /view-as/stop", o.authH.HandleStopViewAs)
	mux.HandleFunc("POST /view-as/{userID}", o.authH.HandleViewAs)
	moduli := service.NewModuleService(repository.NewModuleRepository(o.baza, ledger.New(o.baza, "test")))
	return (&Server{authService: o.auth, moduleService: moduli}).authMiddleware(mux)
}

// Pregled tuđim očima pokreće samo globalni administrator. Dok gleda tuđim
// očima, pravo se traži njegovo, ne gledanog djelatnika; odabir sebe vraća
// vlastite oči, a povratak ide samo na stranicu ovog programa.
func TestPregledTudjimOcimaKrozPrijavu(t *testing.T) {
	o := novaOkolinaPrijave(t)
	srv := posluziteljPogleda(o)
	pero := o.osoba("pperic", "perina-lozinka", "pero.peric@primjer.test", true)
	ana := o.osoba("aanic", "anina-lozinka", "ana.anic@primjer.test", false)
	marko := o.osoba("mmarkic", "markova-lozinka", "marko.markic@primjer.test", false)
	prijavi := func(ime, lozinka string) uuid.UUID {
		t.Helper()
		s, _, err := o.auth.Login(ime, lozinka, "192.168.1.50", "test")
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	posalji := func(sesija uuid.UUID, putanja string, forma url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader(forma.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.String()})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}
	gleda := func(sesija uuid.UUID) string {
		t.Helper()
		v, err := o.auth.AuthenticateSessionView(sesija)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Viewing {
			return ""
		}
		return v.User.Username
	}
	sesija := prijavi("pperic", "perina-lozinka")

	for _, s := range []struct {
		opis, putanja string
		forma         url.Values
		status        int
		kamo, gleda   string
	}{
		{"ulazak u tuđe oči", "/view-as/" + ana.ID.String(), nil, http.StatusSeeOther, "/dashboard", "aanic"},
		{"djelatnik koji ne postoji", "/view-as/" + uuid.NewString(), nil, http.StatusForbidden, "", "aanic"},
		{"nečitljiv identifikator", "/view-as/nije-uuid", nil, http.StatusBadRequest, "", "aanic"},
		{"prelazak na drugog dok se gleda", "/view-as/" + marko.ID.String(), nil, http.StatusSeeOther, "/dashboard", "mmarkic"},
		{"odabir sebe", "/view-as/" + pero.ID.String(), nil, http.StatusSeeOther, "/dashboard", ""},
		{"ponovni ulazak", "/view-as/" + ana.ID.String(), nil, http.StatusSeeOther, "/dashboard", "aanic"},
		{"povratak na stranicu programa", "/view-as/stop", url.Values{"back": {"/sections"}}, http.StatusSeeOther, "/sections", ""},
		{"povratak ne vodi izvan programa", "/view-as/stop", url.Values{"back": {"https://primjer.test/zlo"}}, http.StatusSeeOther, "/users", ""},
	} {
		w := posalji(sesija, s.putanja, s.forma)
		if w.Code != s.status || (s.kamo != "" && w.Header().Get("Location") != s.kamo) {
			t.Errorf("%s: %d %q, očekivano %d %q", s.opis, w.Code, w.Header().Get("Location"), s.status, s.kamo)
		}
		if got := gleda(sesija); got != s.gleda {
			t.Errorf("%s: gleda se očima %q, očekivano %q", s.opis, got, s.gleda)
		}
	}

	anina := prijavi("aanic", "anina-lozinka")
	w := posalji(anina, "/view-as/"+marko.ID.String(), nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "samo globalni administrator") {
		t.Errorf("djelatnica bez administratorskog prava: %d %q", w.Code, w.Body.String())
	}
	if got := gleda(anina); got != "" {
		t.Errorf("odbijen pregled ipak gleda očima %q", got)
	}
}

// Bez prijave pregled tuđim očima vodi na prijavu; kad se pravo
// administratora ne da provjeriti ili se povratak ne da zapisati, to je
// greška poslužitelja, a ne tiho nastavljanje.
func TestPregledTudjimOcimaBezPrijaveIGreske(t *testing.T) {
	o := novaOkolinaPrijave(t)
	ana := o.osoba("aanic", "anina-lozinka", "ana.anic@primjer.test", false)
	zovi := func(h http.HandlerFunc, ctx context.Context, putanja string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, putanja, nil).WithContext(ctx)
		r.SetPathValue("userID", ana.ID.String())
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}
	for _, h := range []http.HandlerFunc{o.authH.HandleViewAs, o.authH.HandleStopViewAs} {
		if w := zovi(h, context.Background(), "/view-as/x"); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("bez prijave: %d %q", w.Code, w.Header().Get("Location"))
		}
	}

	ctx := context.WithValue(context.Background(), contextKeySession, uuid.New())
	ctx = context.WithValue(ctx, contextKeyRealUsr, &models.User{ID: uuid.New(), Username: "nestali"})
	ctx = context.WithValue(ctx, contextKeyViewing, true)
	w := zovi(o.authH.HandleViewAs, ctx, "/view-as/"+ana.ID.String())
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Greška pri provjeri ovlasti") {
		t.Errorf("pravo koje se ne da provjeriti: %d %q", w.Code, w.Body.String())
	}

	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	w = zovi(o.authH.HandleStopViewAs, ctx, "/view-as/stop")
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Greška pri povratku") {
		t.Errorf("povratak koji se ne da zapisati: %d %q", w.Code, w.Body.String())
	}
}

// Prijavljen preglednik sa stranice prijave ide na početnu; kolačić koji
// nije sesija ili sesija koje nema otvaraju prijavu.
func TestStranicaPrijaveZaPrijavljenog(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.osoba("aanic", "anina-lozinka", "ana.anic@primjer.test", false)
	s, _, err := o.auth.Login("aanic", "anina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, sl := range []struct {
		opis, kolacic string
		status        int
	}{
		{"prijavljen", s.ID.String(), http.StatusSeeOther},
		{"bez kolačića", "", http.StatusOK},
		{"kolačić nije sesija", "nije-uuid", http.StatusOK},
		{"sesija koje nema", uuid.NewString(), http.StatusOK},
	} {
		var kolacici []*http.Cookie
		if sl.kolacic != "" {
			kolacici = append(kolacici, &http.Cookie{Name: imeKolacicaSesije, Value: sl.kolacic})
		}
		w := o.zahtjev(izravno, http.MethodGet, "/login", nil, kolacici...)
		if w.Code != sl.status || (sl.status == http.StatusSeeOther && w.Header().Get("Location") != "/") {
			t.Errorf("%s: %d %q", sl.opis, w.Code, w.Header().Get("Location"))
		}
		if sl.status == http.StatusOK && !strings.Contains(w.Body.String(), "svjez=false") {
			t.Errorf("%s: nije stranica prijave: %q", sl.opis, w.Body.String())
		}
	}
}

// Telefon za poveznicu tel: zadržava samo znamenke, a domaći broj s nulom
// dobiva pozivni broj Hrvatske.
func TestTelefonZaPoveznicu(t *testing.T) {
	for upisano, zeli := range map[string]string{
		"031 123 456":     "+38531123456",
		"031/123-456":     "+38531123456",
		"+385 31 123 456": "+38531123456",
		"(098) 765 4321":  "+385987654321",
		"":                "",
		"nema telefona":   "",
	} {
		if got := TelLink(upisano); got != zeli {
			t.Errorf("%q: %q, očekivano %q", upisano, got, zeli)
		}
	}
}

// Kontakt za pomoć oko prijave: osoba iz registra u trenutku otvaranja,
// centar iz postavki čvora; redak o dežurnom ide samo s centrom i telefonom.
func TestKontaktZaPomocOkoPrijave(t *testing.T) {
	h := &AuthHandler{}
	h.SetSupport(SupportContact{Center: "COP Primjerovo", CenterPhone: "031 000 111"})
	c := h.supportNow()
	if c.Name != "" || c.HasPerson() || !c.HasCenter() {
		t.Errorf("bez osobe iz registra: %+v", c)
	}
	h.SetAdminContact(func() (string, string, string, bool) { return "", "", "", false })
	if c := h.supportNow(); c.Name != "" {
		t.Errorf("registar bez administratora: %+v", c)
	}
	h.SetAdminContact(func() (string, string, string, bool) {
		return "Pero Perić", "098 765 4321", "pero.peric@primjer.test", true
	})
	c = h.supportNow()
	if c.Name != "Pero Perić" || c.Phone != "098 765 4321" || c.PhoneLink != "+385987654321" ||
		c.Email != "pero.peric@primjer.test" || c.Center != "COP Primjerovo" || !c.HasPerson() {
		t.Errorf("kontakt s osobom iz registra: %+v", c)
	}
	for _, s := range []SupportContact{{}, {Center: "COP Primjerovo"}, {CenterPhone: "031 000 111"}} {
		if s.HasCenter() {
			t.Errorf("redak o dežurnom bez centra ili telefona: %+v", s)
		}
	}
}

// Kad se sesija ne da otvoriti, prijava javlja grešku i ne postavlja kolačić.
func TestPrijavaBezSesijeJeGreska(t *testing.T) {
	o := novaOkolinaPrijave(t)
	u := o.osoba("aanic", "anina-lozinka", "ana.anic@primjer.test", false)
	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	o.authH.otvoriPrijavu(w, izravno(httptest.NewRequest(http.MethodPost, "/login", nil)), u, nil)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Prijava nije uspjela") || kolacicSesije(w) != nil {
		t.Errorf("sesija se ne da otvoriti: %d %q", w.Code, w.Body.String())
	}
}

// okolinaPINPrimjer je čvor s PIN-om izvana kojemu je dopuštena domena
// primjer.test, i Ana Anić s adresom u njoj
func okolinaPINPrimjer(t *testing.T) *okolinaPIN {
	t.Helper()
	o := novaOkolinaPIN(t)
	if err := o.dk.SpremiOpcije(context.Background(), o.ovlasti(o.admin),
		service.OpcijePrijaveIzvana{PIN: true, Domena: "primjer.test"}, true); err != nil {
		t.Fatal(err)
	}
	o.osoba("aanic", "anina-lozinka", "ana.anic@primjer.test", false)
	return o
}

// Stranica PIN-a nakon traženja novog: kad je otišao, kaže kamo i da stari
// ne vrijedi; kad nije, kaže zašto i da vrijedi raniji. Nepoznata oznaka
// razloga ne dopisuje ništa.
func TestStranicaPINaNakonNovogSlanja(t *testing.T) {
	o := okolinaPINPrimjer(t)
	tunel := krozTunel("198.51.100.7")
	cek := naCekanju(t, o.prijava(tunel, "aanic", "anina-lozinka"))

	w := o.zahtjev(tunel, http.MethodGet, "/login/pin?poslano=1", nil, cek)
	if b := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(b, "|Novi PIN poslan je na a***@primjer.test; prethodni više ne vrijedi.|") {
		t.Errorf("novi PIN otišao: %d %q", w.Code, b)
	}
	w = o.zahtjev(tunel, http.MethodGet, "/login/pin?razlog=zastalo", nil, cek)
	if b := w.Body.String(); !strings.HasPrefix(b, "Novi PIN nije poslan: Slanje PIN-a privremeno ne radi") ||
		!strings.Contains(b, "Vrijedi PIN poslan ranije.|") {
		t.Errorf("novi PIN nije otišao: %q", b)
	}
	w = o.zahtjev(tunel, http.MethodGet, "/login/pin?razlog=nepoznato", nil, cek)
	if b := w.Body.String(); !strings.HasPrefix(b, "||poslan=true") {
		t.Errorf("nepoznata oznaka razloga: %q", b)
	}
}

// Prekinuta prijava izvana briše prijavu na čekanju i vraća na prijavu:
// upis bez nje kaže da je istekla, isključen račun kaže da je deaktiviran, a
// kvar je greška poslužitelja bez pojedinosti.
func TestPrekinutaPrijavaIzvana(t *testing.T) {
	o := okolinaPINPrimjer(t)
	tunel := krozTunel("198.51.100.7")

	w := o.zahtjev(tunel, http.MethodPost, "/login/pin", url.Values{"kod": {"123456"}})
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "Prijava je istekla; prijavite se ponovno.|") || kolacicSesije(w) != nil {
		t.Errorf("upis bez prijave na čekanju: %d %q", w.Code, w.Body.String())
	}

	cek := naCekanju(t, o.prijava(tunel, "aanic", "anina-lozinka"))
	if _, err := o.baza.Exec(`UPDATE users SET is_active = 0 WHERE username = 'aanic'`); err != nil {
		t.Fatal(err)
	}
	w = o.upisi(tunel, cek, o.posta.pin(t), false)
	if !strings.HasPrefix(w.Body.String(), "Korisnički račun je privremeno deaktiviran|") || kolacicSesije(w) != nil {
		t.Errorf("račun isključen nakon lozinke: %d %q", w.Code, w.Body.String())
	}
	if c := kolacic(w, imeKolacicaPrijave); c == nil || c.MaxAge >= 0 {
		t.Errorf("prekinuta prijava mora brisati kolačić prijave na čekanju: %+v", c)
	}

	if _, err := o.baza.Exec(`UPDATE users SET is_active = 1 WHERE username = 'aanic'`); err != nil {
		t.Fatal(err)
	}
	cek = naCekanju(t, o.prijava(tunel, "aanic", "anina-lozinka"))
	if err := o.baza.Close(); err != nil {
		t.Fatal(err)
	}
	w = o.zahtjev(tunel, http.MethodGet, "/login/pin", nil, cek)
	if w.Code != http.StatusInternalServerError || !strings.HasPrefix(w.Body.String(), "Prijava nije uspjela. Pokušajte ponovno ili se prijavite iz ureda.|") {
		t.Errorf("kvar baze: %d %q", w.Code, w.Body.String())
	}
}

// Stranica PIN-a bez predloška piše bar poruku; razlog koji servis ne zna
// vodi se kao neposlan PIN, a prazan tekst ostaje prazan.
func TestStranicaPINaRubniSlucajevi(t *testing.T) {
	w := httptest.NewRecorder()
	(&AuthHandler{}).prikaziPIN(w, http.StatusTooManyRequests, PINPageData{Error: "Previše pokušaja."})
	if w.Code != http.StatusTooManyRequests || w.Body.String() != "Previše pokušaja.\n" || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("stranica PIN-a bez predloška: %d %q", w.Code, w.Body.String())
	}
	if oznakaRazloga(errors.New("nešto drugo")) != "nije" || oznakaRazloga(nil) != "nije" {
		t.Error("nepoznat razlog mora biti neposlan PIN")
	}
	if upperFirst("") != "" || upperFirst("čekanje") != "Čekanje" {
		t.Error("veliko početno slovo")
	}
	p := (&AuthHandler{}).podaciPIN(&service.StanjePrijave{})
	if p.PINPoslan || p.Razlog != "" || !p.MozePonovno {
		t.Errorf("neposlan PIN bez razloga: %+v", p)
	}
}

// Novi PIN: bez prijave na čekanju prijava je istekla; kad slanje zastane,
// stranica PIN-a dobiva razlog, a stari PIN vrijedi; isključen račun ne
// dobiva novi PIN nego prekinutu prijavu.
func TestNoviPINRubniSlucajevi(t *testing.T) {
	o := okolinaPINPrimjer(t)
	tunel := krozTunel("198.51.100.7")
	ponovno := func(kolacici ...*http.Cookie) *httptest.ResponseRecorder {
		return o.zahtjev(tunel, http.MethodPost, "/login/pin/ponovno", url.Values{}, kolacici...)
	}

	if w := ponovno(); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "Prijava je istekla; prijavite se ponovno.|") {
		t.Errorf("novi PIN bez prijave na čekanju: %d %q", w.Code, w.Body.String())
	}

	cek := naCekanju(t, o.prijava(tunel, "aanic", "anina-lozinka"))
	poslano := o.posta.broj()
	o.posta.mu.Lock()
	o.posta.greska = errors.New("veza s poslužiteljem e-pošte je pukla")
	o.posta.mu.Unlock()
	o.pomakni(2 * time.Minute)
	w := ponovno(cek)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login/pin?razlog=zastalo" {
		t.Errorf("slanje zastalo: %d %q", w.Code, w.Header().Get("Location"))
	}
	if o.posta.broj() != poslano+1 {
		t.Errorf("novi PIN nije ni pokušan: %d", o.posta.broj()-poslano)
	}

	if _, err := o.baza.Exec(`UPDATE users SET is_active = 0 WHERE username = 'aanic'`); err != nil {
		t.Fatal(err)
	}
	o.pomakni(2 * time.Minute)
	w = ponovno(cek)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "Prijava je istekla; prijavite se ponovno.|") || kolacicSesije(w) != nil {
		t.Errorf("novi PIN za isključen račun: %d %q", w.Code, w.Body.String())
	}
}
