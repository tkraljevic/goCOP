package web

import (
	"context"
	"fmt"
	"html/template"
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

	"github.com/google/uuid"
)

// okolinaPrijave je čvor s nekoliko računa i rukovateljima prijave i
// uparivanja iza sloja porijekla zahtjeva (klijent.go)
type okolinaPrijave struct {
	t        *testing.T
	repo     *repository.UserRepository
	sessions *repository.SessionRepository
	auth     *service.AuthService
	authH    *AuthHandler
	mux      *http.ServeMux
	srv      http.Handler
}

func novaOkolinaPrijave(t *testing.T) *okolinaPrijave {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "prijava.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	sessions := repository.NewSessionRepository(baza)
	auth := service.NewAuthService(repo, sessions)
	tmpl := template.Must(template.New("login.html").Parse(`{{.Error}}|svjez={{.Fresh}}`))
	authH := NewAuthHandler(auth, tmpl)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", authH.ShowLogin)
	mux.HandleFunc("POST /login", authH.HandleLogin)
	mux.HandleFunc("POST /logout", authH.HandleLogout)
	ponovnaLozinka = newLoginLimiter()
	t.Cleanup(func() { ponovnaLozinka = newLoginLimiter() })
	return &okolinaPrijave{t: t, repo: repo, sessions: sessions, auth: auth, authH: authH, mux: mux,
		srv: (&Server{}).klijentSloj(mux)}
}

func (o *okolinaPrijave) racun(ime, lozinka string, admin, promijeniti bool) *models.User {
	o.t.Helper()
	hash, err := o.auth.HashPassword(lozinka)
	if err != nil {
		o.t.Fatal(err)
	}
	u := &models.User{Username: ime, FullName: ime, PasswordHash: hash, IsActive: true, IsGlobalAdmin: admin, MustChangePassword: promijeniti}
	if err := o.repo.CreateUser(u, nil); err != nil {
		o.t.Fatal(err)
	}
	return u
}

// izravno je zahtjev iz lokalne mreže, kroz tunel zahtjev s interneta preko
// cloudflareda na istom stroju
func izravno(r *http.Request) *http.Request {
	r.RemoteAddr = "192.168.1.50:40000"
	return r
}

func krozTunel(klijent string) func(*http.Request) *http.Request {
	return func(r *http.Request) *http.Request {
		r.RemoteAddr = "127.0.0.1:5555"
		r.Header.Set("CF-Connecting-IP", klijent)
		r.Header.Set("X-Forwarded-Proto", "https")
		return r
	}
}

func (o *okolinaPrijave) prijava(porijeklo func(*http.Request) *http.Request, ime, lozinka string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"username": {ime}, "password": {lozinka}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, porijeklo(r))
	return w
}

func kolacicSesije(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == imeKolacicaSesije {
			return c
		}
	}
	return nil
}

// Kolačić sesije: HttpOnly, SameSite=Lax, Path=/; Secure samo kad je izvorni
// zahtjev bio HTTPS (tunel). Odjava ga briše s istim svojstvima.
func TestKolacicSesijeLokalnoIKrozTunel(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)

	lokalno := kolacicSesije(o.prijava(izravno, "ana", "anina-lozinka"))
	if lokalno == nil || !lokalno.HttpOnly || lokalno.SameSite != http.SameSiteLaxMode || lokalno.Path != "/" || lokalno.Secure {
		t.Fatalf("kolačić u lokalnoj mreži (http): %+v", lokalno)
	}
	if id, err := uuid.Parse(lokalno.Value); err != nil || id.Version() != 4 {
		t.Errorf("token sesije mora biti UUIDv4: %q", lokalno.Value)
	}

	tunel := kolacicSesije(o.prijava(krozTunel("198.51.100.7"), "ana", "anina-lozinka"))
	if tunel == nil || !tunel.Secure || !tunel.HttpOnly || tunel.SameSite != http.SameSiteLaxMode {
		t.Fatalf("kolačić kroz tunel (https) mora biti Secure: %+v", tunel)
	}

	r := krozTunel("198.51.100.7")(httptest.NewRequest(http.MethodPost, "/logout", nil))
	r.AddCookie(tunel)
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, r)
	obrisan := kolacicSesije(w)
	if obrisan == nil || obrisan.MaxAge >= 0 || obrisan.Value != "" || !obrisan.Secure || !obrisan.HttpOnly || obrisan.SameSite != http.SameSiteLaxMode || obrisan.Path != "/" {
		t.Errorf("odjava mora brisati kolačić istim svojstvima i MaxAge<0: %+v", obrisan)
	}
	if s, _ := o.sessions.GetSession(uuid.MustParse(tunel.Value)); s != nil {
		t.Error("odjava mora ugasiti sesiju")
	}
}

// Zadana lozinka piše u dokumentaciji: prva prijava njome kroz tunel se
// odbija, iz lokalne mreže prolazi. Privremena lozinka od administratora
// vrijedi i izvana.
func TestZadanaLozinkaKrozTunelSeOdbija(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("admin", db.ZadanaLozinka, true, true)
	o.racun("ana", "nasip-vrba-most-472", false, true)

	w := o.prijava(krozTunel("198.51.100.7"), "admin", db.ZadanaLozinka)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), porukaZadaneLozinke) || kolacicSesije(w) != nil {
		t.Errorf("zadana lozinka kroz tunel: %d %q, kolačić %v", w.Code, w.Body.String(), kolacicSesije(w))
	}

	w = o.prijava(krozTunel("198.51.100.7"), "ana", "nasip-vrba-most-472")
	if w.Code != http.StatusSeeOther || kolacicSesije(w) == nil || !strings.HasPrefix(w.Header().Get("Location"), "/profile?force=1") {
		t.Errorf("privremena lozinka kroz tunel mora proći na promjenu lozinke: %d %q", w.Code, w.Header().Get("Location"))
	}

	w = o.prijava(izravno, "admin", db.ZadanaLozinka)
	if w.Code != http.StatusSeeOther || kolacicSesije(w) == nil || !strings.HasPrefix(w.Header().Get("Location"), "/profile?force=1") {
		t.Errorf("zadana lozinka iz lokalne mreže mora proći: %d %q", w.Code, w.Body.String())
	}
}

// Pet krivih lozinki s jedne adrese ne zaključava račun vlasniku na drugoj
func TestPetPokusajaNeZakljucavajuTudiRacun(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("ana", "anina-lozinka", false, false)
	napadac := krozTunel("203.0.113.66")
	for i := 0; i < loginMaxAttempts; i++ {
		o.prijava(napadac, "ana", fmt.Sprintf("pogadjam-%d", i))
	}
	if w := o.prijava(napadac, "ana", "anina-lozinka"); w.Code != http.StatusTooManyRequests {
		t.Errorf("napadač nakon pet pokušaja mora čekati, dobiveno %d", w.Code)
	}
	if w := o.prijava(krozTunel("198.51.100.7"), "ana", "anina-lozinka"); w.Code != http.StatusSeeOther || kolacicSesije(w) == nil {
		t.Errorf("vlasnik s druge adrese mora ući, dobiveno %d %q", w.Code, w.Body.String())
	}
}

// Uspješna prijava ne briše brojač adrese: napadač s jednim pravim računom
// ne smije između pokušaja na tuđe račune brisati vlastitu adresu
func TestUspjesnaPrijavaNeBriseBrojacAdrese(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.racun("mojracun", "moja-lozinka", false, false)
	napadac := krozTunel("203.0.113.66")
	for i := 0; i < 19; i++ {
		o.prijava(napadac, fmt.Sprintf("netko%d", i), "pogadjam")
	}
	if w := o.prijava(napadac, "mojracun", "moja-lozinka"); w.Code != http.StatusSeeOther {
		t.Fatalf("vlastiti račun: %d", w.Code)
	}
	o.prijava(napadac, "netko-dvadeseti", "pogadjam")
	if w := o.prijava(napadac, "jos-netko", "pogadjam"); w.Code != http.StatusTooManyRequests {
		t.Errorf("adresa s dvadeset neuspjeha mora čekati i nakon uspješne prijave između, dobiveno %d", w.Code)
	}
}

// Deaktiviran račun javlja se tek uz točnu lozinku
func TestDeaktiviranRacunNaStraniciPrijave(t *testing.T) {
	o := novaOkolinaPrijave(t)
	hash, err := o.auth.HashPassword("stara-lozinka")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.repo.CreateUser(&models.User{Username: "umirovljen", FullName: "Umirovljen", PasswordHash: hash}, nil); err != nil {
		t.Fatal(err)
	}
	if w := o.prijava(izravno, "umirovljen", "kriva"); !strings.Contains(w.Body.String(), "Neispravno korisničko ime ili lozinka") {
		t.Errorf("kriva lozinka na deaktiviranom računu: %q", w.Body.String())
	}
	if w := o.prijava(izravno, "umirovljen", "stara-lozinka"); !strings.Contains(w.Body.String(), "deaktiviran") {
		t.Errorf("točna lozinka na deaktiviranom računu: %q", w.Body.String())
	}
}

// Stranica prijave nudi uparivanje svježeg čvora samo izravnom klijentu
func TestPrijavaNudiUparivanjeSamoLokalno(t *testing.T) {
	o := novaOkolinaPrijave(t)
	o.authH.SetFresh(func() bool { return true })
	get := func(porijeklo func(*http.Request) *http.Request) string {
		w := httptest.NewRecorder()
		o.srv.ServeHTTP(w, porijeklo(httptest.NewRequest(http.MethodGet, "/login", nil)))
		return w.Body.String()
	}
	if b := get(izravno); !strings.Contains(b, "svjez=true") {
		t.Errorf("lokalno: %q", b)
	}
	if b := get(krozTunel("198.51.100.7")); !strings.Contains(b, "svjez=false") {
		t.Errorf("kroz tunel: %q", b)
	}
}

// promjenaLozinke šalje obrazac promjene lozinke iz zadane sesije
func (o *okolinaPrijave) promjenaLozinke(u *models.User, sesija uuid.UUID, trenutna, nova string) string {
	r := httptest.NewRequest(http.MethodPost, "/profile/change-password", strings.NewReader(url.Values{
		"current_password": {trenutna}, "new_password": {nova}, "confirm_password": {nova}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(r.Context(), contextKeyUser, u)
	ctx = context.WithValue(ctx, contextKeySession, sesija)
	w := httptest.NewRecorder()
	o.authH.HandleChangePassword(w, r.WithContext(ctx))
	loc, _ := url.QueryUnescape(w.Header().Get("Location"))
	return loc
}

// Promjena lozinke gasi ostale prijave, a tri kriva upisa trenutne lozinke
// blokiraju daljnje pokušaje
func TestPromjenaLozinkeKrozRukovatelja(t *testing.T) {
	o := novaOkolinaPrijave(t)
	ana := o.racun("ana", "anina-lozinka", false, false)
	ova, _, err := o.auth.Login("ana", "anina-lozinka", "192.168.1.50", "laptop")
	if err != nil {
		t.Fatal(err)
	}
	ukradena, _, err := o.auth.Login("ana", "anina-lozinka", "203.0.113.66", "napadač")
	if err != nil {
		t.Fatal(err)
	}

	// odbijena nova lozinka ne smije prekljucati potpisni ključ
	prekljucano := 0
	o.authH.SetPrekljucaj(func(ctx context.Context, userID, stara, nova string) error { prekljucano++; return nil })
	if loc := o.promjenaLozinke(ana, ova.ID, "anina-lozinka", "kra"); !strings.Contains(loc, "najmanje 6") || prekljucano != 0 {
		t.Errorf("prekratka nova lozinka: %q, ključ prekljucan %d puta", loc, prekljucano)
	}

	if loc := o.promjenaLozinke(ana, ova.ID, "anina-lozinka", "nova-lozinka"); !strings.Contains(loc, "success=") || prekljucano != 1 {
		t.Fatalf("promjena lozinke: %q", loc)
	}
	if s, _ := o.sessions.GetSession(ukradena.ID); s != nil {
		t.Error("druga prijava mora prestati nakon promjene lozinke")
	}
	if s, _ := o.sessions.GetSession(ova.ID); s == nil {
		t.Error("prijava iz koje je lozinka promijenjena mora ostati")
	}

	for i := 0; i < 3; i++ {
		if loc := o.promjenaLozinke(ana, ova.ID, fmt.Sprintf("pogadjam-%d", i), "treca-lozinka"); !strings.Contains(loc, "nije točna") {
			t.Fatalf("kriva lozinka %d: %q", i, loc)
		}
	}
	if loc := o.promjenaLozinke(ana, ova.ID, "nova-lozinka", "treca-lozinka"); !strings.Contains(loc, "previše krivih lozinki") {
		t.Errorf("nakon tri kriva upisa i točna lozinka mora čekati: %q", loc)
	}
}

// --- uparivanje ---

type okolinaUparivanja struct {
	*okolinaPrijave
	pair   *PairHandler
	racuna int
	upiti  int
}

func novaOkolinaUparivanja(t *testing.T) *okolinaUparivanja {
	o := &okolinaUparivanja{okolinaPrijave: novaOkolinaPrijave(t), racuna: 1}
	o.pair = &PairHandler{auth: o.auth}
	o.pair.brojiRacun = func() (int, error) { o.upiti++; return o.racuna, nil }
	return o
}

// zahtjev prolazi Gate i vraća status te ovlast koju je rukovatelj vidio
func (o *okolinaUparivanja) zahtjev(porijeklo func(*http.Request) *http.Request, sesija *models.Session, putanja string) (int, *ovlastUparivanja) {
	var vidio *ovlastUparivanja
	h := (&Server{}).klijentSloj(o.pair.Gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ov, ok := ovlastIz(r); ok {
			vidio = &ov
		}
	})))
	r := porijeklo(httptest.NewRequest(http.MethodPost, putanja, nil))
	if sesija != nil {
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, vidio
}

func (o *okolinaUparivanja) sesija(ime, lozinka string) *models.Session {
	o.t.Helper()
	s, _, err := o.auth.Login(ime, lozinka, "192.168.1.50", "test")
	if err != nil {
		o.t.Fatal(err)
	}
	return s
}

// Uparivanje vodi globalni administrator vlastitim očima s promijenjenom
// lozinkom; bez prijave samo izravan klijent svježeg čvora, nikad kroz tunel
func TestUparivanjeSamoAdministratoruIliLokalnoSvjezem(t *testing.T) {
	o := novaOkolinaUparivanja(t)
	tunel := krozTunel("198.51.100.7")
	const api = "/api/uparivanje/confirm"

	// svjež čvor: lokalno bez prijave da, kroz tunel ne
	if code, ov := o.zahtjev(izravno, nil, api); code != http.StatusOK || ov == nil || ov.korisnik != nil {
		t.Errorf("svjež čvor, izravan klijent: %d %+v", code, ov)
	}
	if code, ov := o.zahtjev(tunel, nil, api); code != http.StatusForbidden || ov != nil {
		t.Errorf("svjež čvor kroz tunel bez prijave mora biti odbijen: %d", code)
	}
	if code, _ := o.zahtjev(tunel, nil, "/uparivanje"); code != http.StatusSeeOther {
		t.Errorf("stranica kroz tunel bez prijave vodi na prijavu: %d", code)
	}

	admin := o.racun("sef", "sefova-lozinka", true, false)
	o.racun("zadani", db.ZadanaLozinka, true, true)
	ana := o.racun("ana", "anina-lozinka", false, false)
	o.racuna = 4
	o.pair.nijeSvjez.Store(false)
	o.pair.svjezKad = o.pair.svjezKad.AddDate(-1, 0, 0) // zaboravi zapamćeni odgovor

	if code, _ := o.zahtjev(izravno, nil, api); code != http.StatusForbidden {
		t.Errorf("čvor s djelatnicima bez prijave: %d", code)
	}
	if code, _ := o.zahtjev(izravno, o.sesija("ana", "anina-lozinka"), api); code != http.StatusForbidden {
		t.Errorf("djelatnik koji nije administrator: %d", code)
	}
	if code, _ := o.zahtjev(izravno, o.sesija("zadani", db.ZadanaLozinka), api); code != http.StatusForbidden {
		t.Errorf("administrator sa zadanom lozinkom: %d", code)
	}
	sef := o.sesija("sef", "sefova-lozinka")
	if code, ov := o.zahtjev(tunel, sef, api); code != http.StatusOK || ov == nil || ov.korisnik == nil || ov.korisnik.ID != admin.ID {
		t.Errorf("administrator i kroz tunel: %d %+v", code, ov)
	}

	perms, err := o.auth.PermissionsFor(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.auth.StartViewingAs(sef.ID, perms, ana.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := o.zahtjev(izravno, sef, api); code != http.StatusForbidden {
		t.Errorf("administrator koji gleda tuđim očima: %d", code)
	}
}

// Svježina čvora se pamti: čvor s djelatnicima više se ne pita bazu, a svjež
// najviše jednom u svjezRok
func TestSvjezinaCvoraSePamti(t *testing.T) {
	o := novaOkolinaUparivanja(t)
	for i := 0; i < 10; i++ {
		if !o.pair.Fresh() {
			t.Fatal("čvor s jednim računom je svjež")
		}
	}
	if o.upiti != 1 {
		t.Errorf("svjež čvor pitao je bazu %d puta unutar %v", o.upiti, svjezRok)
	}
	o.racuna = 2
	o.pair.svjezKad = o.pair.svjezKad.AddDate(-1, 0, 0)
	for i := 0; i < 10; i++ {
		if o.pair.Fresh() {
			t.Fatal("čvor s dva računa nije svjež")
		}
	}
	o.racuna = 1
	o.pair.svjezKad = o.pair.svjezKad.AddDate(-1, 0, 0)
	if o.pair.Fresh() || o.upiti != 2 {
		t.Errorf("čvor koji jednom nije svjež to i ostaje, bez novih upita (upita %d)", o.upiti)
	}
}
