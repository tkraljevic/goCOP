package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"

	"github.com/google/uuid"
)

// okolinaPrijave je čvor s nekoliko računa i rukovateljima prijave i
// uparivanja iza sloja porijekla zahtjeva (klijent.go)
type okolinaPrijave struct {
	t        *testing.T
	baza     *sql.DB
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
	mux.HandleFunc("GET /login/pin", authH.ShowPIN)
	mux.HandleFunc("POST /login/pin", authH.HandlePIN)
	mux.HandleFunc("POST /login/pin/ponovno", authH.HandlePonovniPIN)
	ponovnaLozinka = newLoginLimiter()
	t.Cleanup(func() { ponovnaLozinka = newLoginLimiter() })
	return &okolinaPrijave{t: t, baza: baza, repo: repo, sessions: sessions, auth: auth, authH: authH, mux: mux,
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

// javnoIzravno je zahtjev s javne adrese bez posrednika, npr. kroz port
// proslijeđen na čvor
func javnoIzravno(r *http.Request) *http.Request {
	r.RemoteAddr = "203.0.113.9:40000"
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

	w = o.prijava(javnoIzravno, "admin", db.ZadanaLozinka)
	if w.Code != http.StatusForbidden || kolacicSesije(w) != nil {
		t.Errorf("zadana lozinka s javne adrese bez posrednika: %d, kolačić %v", w.Code, kolacicSesije(w))
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
	if b := get(javnoIzravno); !strings.Contains(b, "svjez=false") {
		t.Errorf("s javne adrese: %q", b)
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

	// sa stranice novih rezervnih kodova (odgovor na POST, bez GET-a)
	// povratak ide na profil, ne na tu adresu (405)
	r := httptest.NewRequest(http.MethodPost, "/profile/change-password", strings.NewReader(url.Values{
		"current_password": {"anina-lozinka"}, "new_password": {"nova-lozinka"}, "confirm_password": {"druga-lozinka"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Referer", "http://example.com/profile/rezervni-kodovi")
	w := httptest.NewRecorder()
	o.authH.HandleChangePassword(w, r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyUser, ana), contextKeySession, ova.ID)))
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/profile?error=") {
		t.Errorf("lozinka sa stranice kodova: %q", loc)
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
	if code, ov := o.zahtjev(javnoIzravno, nil, api); code != http.StatusForbidden || ov != nil {
		t.Errorf("svjež čvor s javne adrese bez prijave mora biti odbijen: %d", code)
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

// --- drugi korak prijave izvana (PIN) ---

// laznaPostaPIN pamti poslane PIN-ove umjesto da ih šalje
type laznaPostaPIN struct {
	mu     sync.Mutex
	poruke []posta.Poruka
	greska error // greška prijave ili veze pri slanju
	pozivi int
}

func (l *laznaPostaPIN) Prijavi(_ context.Context, _ posta.Postavke, r posta.Racun) (string, error) {
	return r.Korisnik, nil
}

func (l *laznaPostaPIN) Posalji(_ context.Context, _ posta.Postavke, _ posta.Racun, poruke []posta.Poruka) ([]error, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pozivi++
	if l.greska != nil {
		return nil, l.greska
	}
	l.poruke = append(l.poruke, poruke...)
	return make([]error, len(poruke)), nil
}

func (l *laznaPostaPIN) Imenik(context.Context, posta.Postavke, posta.Racun, string) ([]posta.Kontakt, error) {
	return nil, nil
}

func (l *laznaPostaPIN) broj() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pozivi
}

var pinIzPoruke = regexp.MustCompile(`\b(\d{6})\b`)

// pin vraća PIN iz zadnje poslane poruke
func (l *laznaPostaPIN) pin(t *testing.T) string {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.poruke) == 0 {
		t.Fatal("PIN nije poslan")
	}
	m := pinIzPoruke.FindStringSubmatch(l.poruke[len(l.poruke)-1].Tekst)
	if m == nil {
		t.Fatalf("u poruci nema PIN-a: %q", l.poruke[len(l.poruke)-1].Tekst)
	}
	return m[1]
}

// okolinaPIN je čvor s uključenim PIN-om izvana: pošiljatelj je upisan i
// probni PIN je prošao
type okolinaPIN struct {
	*okolinaPrijave
	dk    *service.DrugiKorak
	posta *laznaPostaPIN
	admin *models.User
	sad   time.Time
}

func (o *okolinaPIN) pomakni(d time.Duration) { o.sad = o.sad.Add(d) }

// osoba je aktivan račun s adresom e-pošte (prazna = bez adrese)
func (o *okolinaPrijave) osoba(ime, lozinka, email string, admin bool) *models.User {
	o.t.Helper()
	hash, err := o.auth.HashPassword(lozinka)
	if err != nil {
		o.t.Fatal(err)
	}
	u := &models.User{Username: ime, FullName: ime, Email: email, PasswordHash: hash, IsActive: true, IsGlobalAdmin: admin, OrgType: models.OrgHrvatskeVode}
	if err := o.repo.CreateUser(u, nil); err != nil {
		o.t.Fatal(err)
	}
	svjez, err := o.repo.GetUserByID(u.ID)
	if err != nil {
		o.t.Fatal(err)
	}
	return svjez
}

func (o *okolinaPrijave) ovlasti(u *models.User) *models.UserPermissions {
	o.t.Helper()
	p, err := o.auth.PermissionsFor(u.ID)
	if err != nil {
		o.t.Fatal(err)
	}
	return p
}

func novaOkolinaPIN(t *testing.T) *okolinaPIN {
	t.Helper()
	o := &okolinaPIN{okolinaPrijave: novaOkolinaPrijave(t), posta: &laznaPostaPIN{}, sad: time.Now()}
	o.dk = service.NewDrugiKorak(repository.NewDrugiKorakRepository(o.baza), repository.NewRacuniSustavaRepository(o.baza),
		o.repo, repository.NewAktiRepository(o.baza, ledger.New(o.baza, "test")))
	o.dk.SetKljuc([]byte("sjeme-cvora-za-test-32-bajta-xxx"))
	o.dk.SetPostar(o.posta)
	o.dk.SetSat(func() time.Time { return o.sad })
	o.dk.SetPosta(func(context.Context) posta.Postavke {
		return posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: "owa.primjer.hr"}
	})
	o.auth.SetZastitaPrijave(o.dk)
	o.authH.SetDrugiKorak(func() *service.DrugiKorak { return o.dk }, template.Must(template.New("login_pin.html").Parse(
		`{{.Error}}|{{.Info}}|poslan={{.PINPoslan}}|adresa={{.Adresa}}|razlog={{.Razlog}}|ponovno={{.MozePonovno}}|preostalo={{.Preostalo}}`)))

	o.admin = o.osoba("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ctx, perms := context.Background(), o.ovlasti(o.admin)
	if _, err := o.dk.SpremiPosiljatelja(ctx, perms, `VODA\pin`, "tajna", "pin@voda.hr"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.dk.PosaljiProbniPIN(ctx, perms); err != nil {
		t.Fatal(err)
	}
	if err := o.dk.PostaviUkljuceno(ctx, perms, true, true); err != nil {
		t.Fatal(err)
	}
	return o
}

// zahtjev šalje zahtjev s obrascem (nil = bez tijela) i kolačićima
func (o *okolinaPrijave) zahtjev(porijeklo func(*http.Request) *http.Request, metoda, putanja string, forma url.Values, kolacici ...*http.Cookie) *httptest.ResponseRecorder {
	var tijelo io.Reader
	if forma != nil {
		tijelo = strings.NewReader(forma.Encode())
	}
	r := httptest.NewRequest(metoda, putanja, tijelo)
	if forma != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range kolacici {
		if c != nil {
			r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, porijeklo(r))
	return w
}

func kolacic(w *httptest.ResponseRecorder, ime string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == ime {
			return c
		}
	}
	return nil
}

func (o *okolinaPrijave) prijavaS(porijeklo func(*http.Request) *http.Request, ime, lozinka string, kolacici ...*http.Cookie) *httptest.ResponseRecorder {
	return o.zahtjev(porijeklo, http.MethodPost, "/login", url.Values{"username": {ime}, "password": {lozinka}}, kolacici...)
}

// naCekanju provjerava da je točna lozinka dala samo prijavu na čekanju i
// vraća njezin kolačić
func naCekanju(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	c := kolacic(w, imeKolacicaPrijave)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login/pin") || c == nil || c.MaxAge <= 0 || kolacicSesije(w) != nil {
		t.Fatalf("očekivana prijava na čekanju: %d %q, kolačić %v, sesija %v, tijelo %q", w.Code, w.Header().Get("Location"), c, kolacicSesije(w), w.Body.String())
	}
	// __Host- bez Secure preglednik odbaci, pa se prijava vrti u krug
	if !c.Secure {
		t.Fatalf("kolačić prijave na čekanju bez Secure preglednik ne prima: %+v", c)
	}
	return c
}

// upisi šalje PIN ili kod za prijavu na čekanju
func (o *okolinaPrijave) upisi(porijeklo func(*http.Request) *http.Request, cekanje *http.Cookie, kod string, zapamti bool, kolacici ...*http.Cookie) *httptest.ResponseRecorder {
	forma := url.Values{"kod": {kod}}
	if zapamti {
		forma.Set("zapamti", "1")
	}
	return o.zahtjev(porijeklo, http.MethodPost, "/login/pin", forma, append(kolacici, cekanje)...)
}

func krivPIN(pin string) string {
	if pin == "000000" {
		return "111111"
	}
	return "000000"
}

// PIN traži samo prijava izvana: kroz tunel, s javne adrese ili s lažnim
// zaglavljem posrednika (koje PIN samo nameće); lokalna mreža i ovo
// računalo ulaze odmah. Točna lozinka izvana daje samo kolačić prijave na
// čekanju, nikad sesiju. Isključen PIN izvana ne traži ništa.
func TestPINSamoZaPrijavuIzvana(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	poslano := o.posta.broj()

	for ime, porijeklo := range map[string]func(*http.Request) *http.Request{
		"lokalna mreža": izravno,
		"ovo računalo":  func(r *http.Request) *http.Request { r.RemoteAddr = "127.0.0.1:40000"; return r },
	} {
		w := o.prijava(porijeklo, "ana", "anina-lozinka")
		if kolacicSesije(w) == nil || kolacic(w, imeKolacicaPrijave) != nil || w.Header().Get("Location") != "/" {
			t.Errorf("%s: prijava bez PIN-a, dobiveno %d %q", ime, w.Code, w.Header().Get("Location"))
		}
	}
	if o.posta.broj() != poslano {
		t.Error("lokalnoj prijavi ne šalje se PIN")
	}

	c := naCekanju(t, o.prijava(krozTunel("198.51.100.7"), "ana", "anina-lozinka"))
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 600 || c.Domain != "" {
		t.Errorf("kolačić prijave na čekanju: %+v", c)
	}
	if o.posta.broj() != poslano+1 {
		t.Errorf("kroz tunel mora otići jedan PIN, otišlo %d", o.posta.broj()-poslano)
	}

	// Javna adresa bez posrednika preko nešifriranog http-a: kolačić
	// __Host- preglednik ondje ne prima, pa se prijava odbija jasnom
	// porukom, bez PIN-a i bez ikakvog kolačića
	t.Run("javna adresa bez posrednika", func(t *testing.T) {
		poslano := o.posta.broj()
		w := o.prijava(func(r *http.Request) *http.Request { r.RemoteAddr = "203.0.113.20:5000"; return r }, "ana", "anina-lozinka")
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "samo preko HTTPS-a") || len(w.Result().Cookies()) != 0 {
			t.Errorf("izvana preko http-a: %d, kolačići %v, tijelo %q", w.Code, w.Result().Cookies(), w.Body.String())
		}
		if o.posta.broj() != poslano {
			t.Error("izvana preko http-a PIN se ne šalje")
		}
	})

	// Posrednik preko nešifriranog http-a (nema X-Forwarded-Proto: https):
	// kolačić __Host- ni ondje ne bi stigao, pa ista jasna poruka
	t.Run("posrednik preko http-a", func(t *testing.T) {
		poslano := o.posta.broj()
		w := o.prijava(func(r *http.Request) *http.Request {
			r.RemoteAddr = "127.0.0.1:5555"
			r.Header.Set("CF-Connecting-IP", "198.51.100.8")
			return r
		}, "ana", "anina-lozinka")
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "samo preko HTTPS-a") || len(w.Result().Cookies()) != 0 || o.posta.broj() != poslano {
			t.Errorf("posrednik preko http-a: %d, kolačići %v, PIN-ova %d", w.Code, w.Result().Cookies(), o.posta.broj()-poslano)
		}
	})

	// Podmetnuto zaglavlje klijenta ne čini zahtjev lokalnim: preko HTTPS-a
	// traži PIN, preko http-a se odbija
	for ime, porijeklo := range map[string]func(*http.Request) *http.Request{
		"lažni X-Forwarded-For iz lokalne mreže": func(r *http.Request) *http.Request {
			r.RemoteAddr = "192.168.1.50:40000"
			r.Header.Set("X-Forwarded-For", "192.168.1.9")
			return r
		},
		"lažni CF-Connecting-IP s interneta": func(r *http.Request) *http.Request {
			r.RemoteAddr = "203.0.113.21:5000"
			r.Header.Set("CF-Connecting-IP", "192.168.1.9")
			return r
		},
	} {
		t.Run(ime, func(t *testing.T) {
			https := func(r *http.Request) *http.Request {
				r = porijeklo(r)
				r.Header.Set("X-Forwarded-Proto", "https")
				return r
			}
			naCekanju(t, o.prijava(https, "ana", "anina-lozinka"))
			if w := o.prijava(porijeklo, "ana", "anina-lozinka"); w.Code != http.StatusForbidden || kolacicSesije(w) != nil {
				t.Errorf("preko http-a: %d, sesija %v", w.Code, kolacicSesije(w))
			}
		})
	}

	if err := o.dk.PostaviUkljuceno(context.Background(), o.ovlasti(o.admin), false, false); err != nil {
		t.Fatal(err)
	}
	if w := o.prijava(krozTunel("198.51.100.7"), "ana", "anina-lozinka"); kolacicSesije(w) == nil || kolacic(w, imeKolacicaPrijave) != nil {
		t.Errorf("isključen PIN: prijava kroz tunel ulazi odmah, dobiveno %d %q", w.Code, w.Header().Get("Location"))
	}
}

// Točan PIN otvara sesiju i briše kolačić prijave na čekanju; isti PIN ne
// vrijedi dvaput. Odjava briše prijavu na čekanju.
func TestPINKrozTunelJednokratan(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	tunel := krozTunel("198.51.100.7")

	cek := naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	w := o.zahtjev(tunel, http.MethodGet, "/login/pin", nil, cek)
	if b := w.Body.String(); w.Code != http.StatusOK || !strings.Contains(b, "poslan=true") || !strings.Contains(b, "adresa=a***@voda.hr") || !strings.Contains(b, "ponovno=true") {
		t.Errorf("stranica PIN-a: %d %q", w.Code, b)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("stranica PIN-a ne smije ostati u pregledniku: %q", cc)
	}

	pin := o.posta.pin(t)
	w = o.upisi(tunel, cek, " "+pin+" ", false)
	sesija := kolacicSesije(w)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" || sesija == nil || !sesija.Secure {
		t.Fatalf("točan PIN: %d %q, sesija %v, tijelo %q", w.Code, w.Header().Get("Location"), sesija, w.Body.String())
	}
	if c := kolacic(w, imeKolacicaPrijave); c == nil || c.MaxAge >= 0 {
		t.Errorf("uspjeh mora brisati kolačić prijave na čekanju: %+v", c)
	}
	if kolacic(w, imeKolacicaRacunala) != nil {
		t.Error("bez kvačice računalo se ne pamti")
	}

	w = o.upisi(tunel, cek, pin, false)
	if kolacicSesije(w) != nil || !strings.Contains(w.Body.String(), "istekla") {
		t.Errorf("PIN vrijedi samo jednom: %d %q", w.Code, w.Body.String())
	}

	if w := o.zahtjev(tunel, http.MethodGet, "/login/pin", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("bez prijave na čekanju stranica PIN-a vodi na prijavu: %d %q", w.Code, w.Header().Get("Location"))
	}

	cek2 := naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	w = o.zahtjev(tunel, http.MethodPost, "/logout", nil, sesija, cek2)
	if c := kolacic(w, imeKolacicaPrijave); c == nil || c.MaxAge >= 0 || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Errorf("odjava mora brisati kolačić prijave na čekanju: %+v", c)
	}
}

// Pet krivih PIN-ova briše prijavu na čekanju (ni točan PIN tada ne
// prolazi); istekla prijava ne prolazi ni s točnim PIN-om
func TestPetKrivihPINovaIIstek(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	tunel := krozTunel("198.51.100.7")

	cek := naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	pin := o.posta.pin(t)
	for i := 1; i < service.NajviseKrivihUnosa; i++ {
		w := o.upisi(tunel, cek, krivPIN(pin), false)
		if kolacicSesije(w) != nil || !strings.Contains(w.Body.String(), fmt.Sprintf("preostalo=%d", service.NajviseKrivihUnosa-i)) {
			t.Fatalf("krivi PIN %d: %d %q", i, w.Code, w.Body.String())
		}
	}
	w := o.upisi(tunel, cek, krivPIN(pin), false)
	if kolacicSesije(w) != nil || !strings.Contains(w.Body.String(), "Previše krivih kodova") {
		t.Fatalf("peti krivi PIN: %d %q", w.Code, w.Body.String())
	}
	if c := kolacic(w, imeKolacicaPrijave); c == nil || c.MaxAge >= 0 {
		t.Errorf("peti krivi PIN briše kolačić prijave na čekanju: %+v", c)
	}
	if w := o.upisi(tunel, cek, pin, false); kolacicSesije(w) != nil {
		t.Error("nakon pet krivih ni točan PIN ne smije proći")
	}

	drugi := krozTunel("198.51.100.8")
	cek = naCekanju(t, o.prijava(drugi, "ana", "anina-lozinka"))
	pin = o.posta.pin(t)
	o.pomakni(service.TrajanjePrijaveNaCekanju + time.Second)
	if w := o.upisi(drugi, cek, pin, false); kolacicSesije(w) != nil || !strings.Contains(w.Body.String(), "istekla") {
		t.Errorf("istekla prijava na čekanju: %d %q", w.Code, w.Body.String())
	}
}

// Točna lozinka izvana ne briše brojač imena: to čini tek prošao PIN. Inače
// bi ukradena lozinka između pogađanja PIN-a brisala vlastiti brojač.
func TestTocnaLozinkaBezPINaNeBriseBrojacImena(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	napadac := krozTunel("203.0.113.66")
	for i := 0; i < loginMaxAttempts-1; i++ {
		o.prijava(napadac, "ana", fmt.Sprintf("pogadjam-%d", i))
	}
	cek := naCekanju(t, o.prijava(napadac, "ana", "anina-lozinka"))
	if w := o.upisi(napadac, cek, krivPIN(o.posta.pin(t)), false); kolacicSesije(w) != nil {
		t.Fatal("krivi PIN ne smije otvoriti sesiju")
	}
	if w := o.prijava(napadac, "ana", "anina-lozinka"); w.Code != http.StatusTooManyRequests {
		t.Errorf("četiri kriva lozinke i krivi PIN s iste adrese moraju čekati, dobiveno %d %q", w.Code, w.Body.String())
	}
}

// Blokiran ključ imena s adrese ili same adrese zaustavlja i točan PIN:
// 429, bez sesije i bez trošenja prijave na čekanju
func TestBlokiranKljucZaustavljaITocanPIN(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	o.osoba("ivo", "ivina-lozinka", "ivo.ivic@voda.hr", false)

	t.Run("ime s adrese", func(t *testing.T) {
		napadac := krozTunel("203.0.113.66")
		cek := naCekanju(t, o.prijava(napadac, "ana", "anina-lozinka"))
		pin := o.posta.pin(t)
		for i := 0; i < loginMaxAttempts-1; i++ {
			o.prijava(napadac, "ana", fmt.Sprintf("pogadjam-%d", i))
		}
		o.upisi(napadac, cek, krivPIN(pin), false)
		w := o.upisi(napadac, cek, pin, false)
		if w.Code != http.StatusTooManyRequests || kolacicSesije(w) != nil || !strings.Contains(w.Body.String(), "Previše neuspjelih") {
			t.Errorf("blokiran user+ip: točan PIN mora čekati: %d %q", w.Code, w.Body.String())
		}
		// s druge adrese ista prijava na čekanju prolazi: blokada je adrese
		if w := o.upisi(krozTunel("198.51.100.9"), cek, pin, false); kolacicSesije(w) == nil {
			t.Errorf("s druge adrese točan PIN prolazi: %d %q", w.Code, w.Body.String())
		}
	})

	t.Run("adresa", func(t *testing.T) {
		napadac := krozTunel("203.0.113.77")
		cek := naCekanju(t, o.prijava(napadac, "ivo", "ivina-lozinka"))
		pin := o.posta.pin(t)
		for i := 0; i < 20; i++ {
			o.prijava(napadac, fmt.Sprintf("netko-%d", i), "pogadjam")
		}
		w := o.upisi(napadac, cek, pin, false)
		if w.Code != http.StatusTooManyRequests || kolacicSesije(w) != nil {
			t.Errorf("blokiran ip: točan PIN mora čekati: %d %q", w.Code, w.Body.String())
		}
	})
}

// Zapamćeno računalo preskače samo PIN, i to samo osobi koja ga je
// zapamtila i samo dok se lozinka ne promijeni (ni na drugom čvoru); odjava
// ga ne zaboravlja
func TestZapamcenoRacunaloPreskaceSamoPIN(t *testing.T) {
	o := novaOkolinaPIN(t)
	ana := o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	o.osoba("ivo", "ivina-lozinka", "ivo.ivic@voda.hr", false)
	tunel := krozTunel("198.51.100.7")

	cek := naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	w := o.upisi(tunel, cek, o.posta.pin(t), true)
	rac := kolacic(w, imeKolacicaRacunala)
	if kolacicSesije(w) == nil || rac == nil {
		t.Fatalf("PIN s kvačicom: sesija %v, računalo %v", kolacicSesije(w), rac)
	}
	if !rac.HttpOnly || !rac.Secure || rac.SameSite != http.SameSiteLaxMode || rac.Path != "/" || rac.MaxAge != int(service.TrajanjeRacunala/time.Second) {
		t.Errorf("kolačić računala: %+v", rac)
	}

	w = o.zahtjev(tunel, http.MethodPost, "/logout", nil, kolacicSesije(w), rac)
	if kolacic(w, imeKolacicaRacunala) != nil {
		t.Error("odjava ne smije zaboraviti računalo")
	}

	poslano := o.posta.broj()
	w = o.prijavaS(tunel, "ana", "anina-lozinka", rac)
	if kolacicSesije(w) == nil || kolacic(w, imeKolacicaPrijave) != nil || o.posta.broj() != poslano {
		t.Errorf("zapamćeno računalo preskače PIN: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := o.prijavaS(tunel, "ana", "kriva-lozinka", rac); kolacicSesije(w) != nil || kolacic(w, imeKolacicaPrijave) != nil {
		t.Error("zapamćeno računalo ne preskače lozinku")
	}
	naCekanju(t, o.prijavaS(tunel, "ivo", "ivina-lozinka", rac))

	// lozinka promijenjena na drugom čvoru stiže razmjenom kao novi sažetak
	hash, err := o.auth.HashPassword("nova-anina-lozinka")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, ana.ID.String()); err != nil {
		t.Fatal(err)
	}
	naCekanju(t, o.prijavaS(tunel, "ana", "nova-anina-lozinka", rac))
}

// Kad PIN ne može otići (nema adrese, pošiljatelj odbijen), stranica nudi
// samo kodove: privremeni od administratora ili rezervni. Odbijena lozinka
// pošiljatelja zaustavlja daljnje slanje.
func TestSamoKodoviKadPINNeIde(t *testing.T) {
	o := novaOkolinaPIN(t)
	ctx := context.Background()
	bez := o.osoba("bez", "bezova-lozinka", "", false)
	tunel := krozTunel("198.51.100.7")
	poslano := o.posta.broj()

	w := o.prijava(tunel, "bez", "bezova-lozinka")
	cek := naCekanju(t, w)
	if loc := w.Header().Get("Location"); loc != "/login/pin?razlog=adresa" || o.posta.broj() != poslano {
		t.Errorf("bez adrese: %q, poslano %d", loc, o.posta.broj()-poslano)
	}
	w = o.zahtjev(tunel, http.MethodGet, "/login/pin?razlog=adresa", nil, cek)
	if b := w.Body.String(); !strings.Contains(b, "poslan=false") || !strings.Contains(b, "razlog=U profilu nema adrese") || !strings.Contains(b, "ponovno=false") {
		t.Errorf("stranica samo za kodove: %q", b)
	}
	kod, _, err := o.dk.IzdajPrivremeniKod(ctx, o.ovlasti(o.admin), bez.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if w := o.upisi(tunel, cek, strings.ToLower(kod), false); kolacicSesije(w) == nil {
		t.Errorf("privremeni kod od administratora: %d %q", w.Code, w.Body.String())
	}

	ana := o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	o.posta.greska = posta.ErrPrijava
	poslano = o.posta.broj()
	for i := 0; i < 2; i++ {
		w = o.prijava(tunel, "ana", "anina-lozinka")
		cek = naCekanju(t, w)
		if loc := w.Header().Get("Location"); loc != "/login/pin?razlog=neispravan" {
			t.Errorf("odbijen pošiljatelj, prijava %d: %q", i+1, loc)
		}
	}
	if o.posta.broj() != poslano+1 {
		t.Errorf("nakon odbijene lozinke pošiljatelja ne smije se slati dalje: pokušaja %d", o.posta.broj()-poslano)
	}
	w = o.zahtjev(tunel, http.MethodGet, "/login/pin?razlog=neispravan", nil, cek)
	if b := w.Body.String(); !strings.Contains(b, "poslan=false") || !strings.Contains(b, "razlog=Poslužitelj e-pošte odbio") {
		t.Errorf("stranica uz odbijenog pošiljatelja: %q", b)
	}

	kodovi, err := o.dk.NapraviRezervneKodove(ctx, ana, "anina-lozinka", false)
	if err != nil {
		t.Fatal(err)
	}
	if w := o.upisi(tunel, cek, kodovi[2], false); kolacicSesije(w) == nil {
		t.Fatalf("rezervni kod: %d %q", w.Code, w.Body.String())
	}
	cek = naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	if w := o.upisi(tunel, cek, kodovi[2], false); kolacicSesije(w) != nil || !strings.Contains(w.Body.String(), "preostalo=4") {
		t.Errorf("rezervni kod vrijedi jednom: %d %q", w.Code, w.Body.String())
	}
}

// Kad slanje PIN-a zastane, razlog i „Pošalji novi PIN” ostaju na
// stranici i nakon krivog ili neispravno upisanog koda (POST), ne samo na
// prvom prikazu s ?razlog= (GET)
func TestRazlogINoviPINOstajuNakonKrivogKoda(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	tunel := krozTunel("198.51.100.7")
	o.posta.greska = errors.New("dial tcp: i/o timeout")

	w := o.prijava(tunel, "ana", "anina-lozinka")
	cek := naCekanju(t, w)
	if loc := w.Header().Get("Location"); loc != "/login/pin?razlog=zastalo" {
		t.Fatalf("zastalo slanje: %q", loc)
	}
	ponudaNovog := func(ime string, w *httptest.ResponseRecorder) {
		t.Helper()
		if b := w.Body.String(); !strings.Contains(b, "poslan=false") || !strings.Contains(b, "razlog=Slanje PIN-a privremeno ne radi") || !strings.Contains(b, "ponovno=true") {
			t.Errorf("%s: stranica mora zadržati razlog i novi PIN: %d %q", ime, w.Code, b)
		}
	}
	ponudaNovog("GET bez oznake", o.zahtjev(tunel, http.MethodGet, "/login/pin", nil, cek))
	ponudaNovog("krivi rezervni kod", o.upisi(tunel, cek, "R1-AAAA-BBBB", false))
	ponudaNovog("neispravan oblik", o.upisi(tunel, cek, "abc", false))

	// lažna oznaka u adresi ne mijenja razlog koji pamti prijava
	ponudaNovog("tuđa oznaka", o.zahtjev(tunel, http.MethodGet, "/login/pin?razlog=adresa", nil, cek))
}

// Računalo se pamti samo kad preglednik može primiti kolačić __Host-
// (HTTPS ili posrednik): preko nešifriranog http-a s javne adrese ni
// kolačić ni redak u bazi
func TestZapamtiRacunaloSamoPrekoHTTPS(t *testing.T) {
	o := novaOkolinaPIN(t)
	ana := o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	cek := naCekanju(t, o.prijava(krozTunel("198.51.100.7"), "ana", "anina-lozinka"))
	javna := func(r *http.Request) *http.Request { r.RemoteAddr = "203.0.113.20:5000"; return r }
	w := o.upisi(javna, cek, o.posta.pin(t), true)
	if kolacicSesije(w) == nil {
		t.Fatalf("točan PIN: %d %q", w.Code, w.Body.String())
	}
	if c := kolacic(w, imeKolacicaRacunala); c != nil {
		t.Errorf("preko http-a računalo se ne pamti: %+v", c)
	}
	if r, err := o.dk.Racunala(context.Background(), ana.ID, ""); err != nil || len(r) != 0 {
		t.Errorf("preko http-a nema zapamćenog računala u bazi: %v %v", r, err)
	}
}

// Novi PIN najranije minutu nakon prethodnog; stari tada prestaje vrijediti
func TestPonovniPIN(t *testing.T) {
	o := novaOkolinaPIN(t)
	o.osoba("ana", "anina-lozinka", "ana.anic@voda.hr", false)
	tunel := krozTunel("198.51.100.7")

	cek := naCekanju(t, o.prijava(tunel, "ana", "anina-lozinka"))
	stari := o.posta.pin(t)
	if w := o.zahtjev(tunel, http.MethodPost, "/login/pin/ponovno", nil, cek); !strings.Contains(w.Body.String(), "najranije minutu") {
		t.Errorf("prerano: %d %q", w.Code, w.Body.String())
	}
	o.pomakni(service.RazmakPonovnogSlanja + time.Second)
	w := o.zahtjev(tunel, http.MethodPost, "/login/pin/ponovno", nil, cek)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login/pin?poslano=1" {
		t.Fatalf("ponovno slanje: %d %q", w.Code, w.Header().Get("Location"))
	}
	novi := o.posta.pin(t)
	if novi != stari {
		if w := o.upisi(tunel, cek, stari, false); kolacicSesije(w) != nil {
			t.Error("stari PIN nakon novog ne smije vrijediti")
		}
	}
	if w := o.upisi(tunel, cek, novi, false); kolacicSesije(w) == nil {
		t.Errorf("novi PIN: %d %q", w.Code, w.Body.String())
	}
}

// Prava stranica za upis PIN-a iscrtava se s PIN-om i bez njega
func TestStranicaPINaIscrtava(t *testing.T) {
	fsys, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := template.New("login_pin.html").Funcs(templateFuncs()).ParseFS(fsys, "login_pin.html")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := tp.ExecuteTemplate(&b, "login_pin.html", PINPageData{PINPoslan: true, Adresa: "a***@voda.hr", IstjeceTekst: "10:10", MozePonovno: true, Preostalo: 3}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"a***@voda.hr", `action="/login/pin"`, `name="kod"`, `name="zapamti"`, "Zapamti ovo računalo 30 dana", `action="/login/pin/ponovno"`, "Preostalo pokušaja: 3"} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("stranica s PIN-om nema %q", s)
		}
	}
	b.Reset()
	if err := tp.ExecuteTemplate(&b, "login_pin.html", PINPageData{Razlog: "U profilu nema adrese e-pošte", IstjeceTekst: "10:10", Preostalo: 5}); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); !strings.Contains(s, "PIN nije poslan") || !strings.Contains(s, "U profilu nema adrese e-pošte") || strings.Contains(s, "/login/pin/ponovno") {
		t.Errorf("stranica samo za kodove: %q", s)
	}
}
