package web

import (
	"context"
	"crypto/tls"
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Prijava izvana na profilu, kod djelatnika i u administraciji e-pošte:
// kodovi se pokazuju samo jednom i bez spremanja u pregledniku, tuđe oči
// ne prave kodove ni ne zaboravljaju računala, a PIN se ne uključuje prije
// uspješnog probnog PIN-a.

// lazniPostar prima prijavu i poruke bez poslužitelja
type lazniPostar struct {
	poslano []posta.Poruka
	greska  error
	prijava int // poziva Prijavi
}

func (l *lazniPostar) Prijavi(_ context.Context, _ posta.Postavke, r posta.Racun) (string, error) {
	l.prijava++
	return r.Korisnik, l.greska
}

func (l *lazniPostar) Posalji(_ context.Context, _ posta.Postavke, _ posta.Racun, poruke []posta.Poruka) ([]error, error) {
	if l.greska != nil {
		return nil, l.greska
	}
	l.poslano = append(l.poslano, poruke...)
	return make([]error, len(poruke)), nil
}

func (l *lazniPostar) Imenik(context.Context, posta.Postavke, posta.Racun, string) ([]posta.Kontakt, error) {
	return nil, nil
}

type okolinaIzvana struct {
	t      *testing.T
	baza   *sql.DB
	repo   *repository.UserRepository
	auth   *service.AuthService
	users  *service.UserService
	dk     *service.DrugiKorak
	postar *lazniPostar
	usersH *UsersHandler
	aktiH  *AktiHandler
	mux    *http.ServeMux
}

func novaOkolinaIzvana(t *testing.T) *okolinaIzvana {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "izvana.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	repo := repository.NewUserRepository(baza, rec)
	auth := service.NewAuthService(repo, repository.NewSessionRepository(baza))
	users := service.NewUserService(repo, auth, service.NewSSEBroker())
	dk := service.NewDrugiKorak(repository.NewDrugiKorakRepository(baza), repository.NewRacuniSustavaRepository(baza),
		repo, repository.NewAktiRepository(baza, rec))
	dk.SetKljuc([]byte("sjeme-cvora-za-test-drugog-koraka"))
	postar := &lazniPostar{}
	dk.SetPostar(postar)
	dk.SetPosta(func(context.Context) posta.Postavke {
		return posta.Postavke{Nacin: "ews", Posluzitelj: "owa.primjer.hr"}
	})
	auth.SetZastitaPrijave(dk)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	usersH := NewUsersHandler(users, tmpl("users.html"))
	usersH.SetPageTemplates(tmpl("user_detail.html"), tmpl("user_form.html"), tmpl("duty_form.html"), tmpl("profile.html"))
	usersH.SetDrugiKorak(func() *service.DrugiKorak { return dk })
	aktiH := NewAktiHandler(func() *service.AktService { return nil }, users, nil, nil, nil, nil, nil)
	aktiH.SetDrugiKorak(func() *service.DrugiKorak { return dk })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /profile", usersH.ShowProfile)
	mux.HandleFunc("POST /profile/update", usersH.HandleUpdateProfile)
	mux.HandleFunc("POST /profile/rezervni-kodovi", usersH.HandleRezervniKodovi)
	mux.HandleFunc("POST /profile/rezervni-kodovi.txt", usersH.HandleRezervniKodoviTxt)
	mux.HandleFunc("POST /profile/racunala/{id}/zaboravi", usersH.HandleZaboraviRacunalo)
	mux.HandleFunc("POST /profile/racunala/zaboravi-sva", usersH.HandleZaboraviSvaRacunala)
	mux.HandleFunc("POST /users/{id}/reset-password", usersH.HandleResetPassword)
	mux.HandleFunc("POST /users/{id}/kod-prijave", usersH.HandleKodPrijave)
	mux.HandleFunc("POST /users/update", usersH.HandleUpdateUser)
	mux.HandleFunc("POST /administracija/posta/pin", aktiH.HandlePINPosiljatelj)
	mux.HandleFunc("POST /administracija/posta/pin/proba", aktiH.HandlePINProba)
	mux.HandleFunc("POST /administracija/posta/pin/sklopka", aktiH.HandlePINSklopka)
	ponovnaLozinka = newLoginLimiter()
	t.Cleanup(func() { ponovnaLozinka = newLoginLimiter() })
	return &okolinaIzvana{t: t, baza: baza, repo: repo, auth: auth, users: users, dk: dk, postar: postar, usersH: usersH, aktiH: aktiH, mux: mux}
}

func (o *okolinaIzvana) racun(ime, lozinka, email string, admin bool) *models.User {
	o.t.Helper()
	hash, err := o.auth.HashPassword(lozinka)
	if err != nil {
		o.t.Fatal(err)
	}
	u := &models.User{Username: ime, FullName: strings.ToUpper(ime[:1]) + ime[1:], PasswordHash: hash, Email: email,
		IsActive: true, IsGlobalAdmin: admin, OrgType: models.OrgHrvatskeVode}
	if err := o.repo.CreateUser(u, nil); err != nil {
		o.t.Fatal(err)
	}
	return u
}

// zahtjev je zahtjev prijavljene osobe iz lokalne mreže; gleda li tuđim
// očima, vidi osobu vidi, a stvarno je prijavljena stvarni
type zahtjevIzvana struct {
	stvarni, vidi *models.User
	izvana        bool
	bezHTTPS      bool   // izvana kroz posrednika koji ne javlja HTTPS
	referer       string // stranica s koje je obrazac poslan
}

func (o *okolinaIzvana) posalji(z zahtjevIzvana, metoda, put string, polja url.Values) *httptest.ResponseRecorder {
	o.t.Helper()
	var tijelo *strings.Reader
	if polja != nil {
		tijelo = strings.NewReader(polja.Encode())
	} else {
		tijelo = strings.NewReader("")
	}
	r := httptest.NewRequest(metoda, put, tijelo)
	if polja != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if z.referer != "" {
		r.Header.Set("Referer", z.referer)
	}
	r.RemoteAddr = "192.168.1.50:40000"
	if z.izvana {
		// izvana preko HTTPS-a, izravno na javnu adresu
		r.RemoteAddr = "198.51.100.7:40000"
		r.TLS = &tls.ConnectionState{}
	}
	if z.bezHTTPS {
		r.RemoteAddr = "127.0.0.1:5555"
		r.Header.Set("CF-Connecting-IP", "198.51.100.7")
	}
	vidi := z.vidi
	if vidi == nil {
		vidi = z.stvarni
	}
	// svjež iz baze, kao authMiddleware
	if u, err := o.repo.GetUserByID(vidi.ID); err == nil && u != nil {
		vidi = u
	}
	perms := &models.UserPermissions{User: *vidi, IsGlobalAdmin: vidi.IsGlobalAdmin}
	c := context.WithValue(r.Context(), contextKeyUser, vidi)
	c = context.WithValue(c, contextKeyPerms, perms)
	c = context.WithValue(c, contextKeyRealUsr, z.stvarni)
	c = context.WithValue(c, contextKeyViewing, z.vidi != nil && z.vidi.ID != z.stvarni.ID)
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

// greskaPreusmjerenja vraća poruku greške iz preusmjerenja (prazno: nema je)
func greskaPreusmjerenja(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		return ""
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("error")
}

var uzorakRezervnog = regexp.MustCompile(`R\d{1,2}-[A-Z2-9]{4}-[A-Z2-9]{4}`)

// Rezervni kodovi traže lozinku, pokazuju se jednom i ne spremaju se u
// pregledniku; .txt ih pravi iznova, pa stari niz prestaje vrijediti.
func TestRezervniKodoviJednomIBezSpremanja(t *testing.T) {
	o := novaOkolinaIzvana(t)
	ana := o.racun("ana", "anina-lozinka", "ana@voda.hr", false)
	ctx := context.Background()
	o.usersH.SetCvor(func() string { return "cop-laptop" })

	// kodovi vrijede samo na čvoru na kojem su napravljeni: profil to kaže
	// i iz lokalne mreže upućuje na javni čvor
	if b := o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodGet, "/profile", nil).Body.String(); !strings.Contains(b, `Kodovi vrijede samo na čvoru <span class="mono">cop-laptop</span>`) || !strings.Contains(b, "napravite ih na javnom čvoru") {
		t.Error("profil iz lokalne mreže mora reći da kodovi vrijede samo na ovom čvoru i uputiti na javni")
	}
	if b := o.posalji(zahtjevIzvana{stvarni: ana, izvana: true}, http.MethodGet, "/profile", nil).Body.String(); !strings.Contains(b, "kodovi napravljeni ovdje vrijede za prijavu izvana") {
		t.Error("profil izvana mora reći da kodovi odavde vrijede izvana")
	}

	w := o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodPost, "/profile/rezervni-kodovi", url.Values{"lozinka": {"kriva"}})
	if !strings.Contains(greskaPreusmjerenja(t, w), "lozinka") {
		t.Fatalf("kriva lozinka: %d %s", w.Code, w.Header().Get("Location"))
	}
	if n, _, _ := o.dk.StanjeRezervnih(ctx, ana.ID); n != 0 {
		t.Fatalf("kriva lozinka napravila je %d kodova", n)
	}
	if b, _ := ponovnaLozinka.Blocked(kljucPonovneLozinke("", ana.ID.String())); b {
		t.Fatal("jedan krivi upis ne smije blokirati")
	}

	w = o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodPost, "/profile/rezervni-kodovi", url.Values{"lozinka": {"anina-lozinka"}})
	if w.Code != http.StatusOK {
		t.Fatalf("kodovi: %d %s", w.Code, w.Header().Get("Location"))
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("stranica s kodovima mora imati Cache-Control: no-store, ima %q", cc)
	}
	prvi := uzorakRezervnog.FindAllString(w.Body.String(), -1)
	if len(prvi) != service.BrojRezervnihKodova || !strings.HasPrefix(prvi[9], "R10-") {
		t.Fatalf("na stranici treba biti 10 kodova R1…R10: %v", prvi)
	}
	// adresa stranice s kodovima je POST: preglednik je odmah zamijeni
	// profilom, da osvježavanje ne napravi novi niz
	if !strings.Contains(w.Body.String(), `history.replaceState(null, '', '/profile#prijava-izvana')`) {
		t.Error("stranica s kodovima mora zamijeniti adresu profilom")
	}
	// obrasci profila poslani sa stranice kodova vraćaju se na profil, ne na
	// POST-adresu (405)
	sKodova := zahtjevIzvana{stvarni: ana, referer: "http://example.com/profile/rezervni-kodovi"}
	if loc := o.posalji(sKodova, http.MethodPost, "/profile/update", url.Values{"full_name": {"Ana"}, "email": {"ana@voda.hr"}}).Header().Get("Location"); !strings.HasPrefix(loc, "/profile?success=") {
		t.Errorf("kontakti sa stranice kodova: %q", loc)
	}
	r := httptest.NewRequest(http.MethodPost, "/profile/change-password", nil)
	r.Header.Set("Referer", "http://example.com/profile/rezervni-kodovi")
	if p := povratnaAdresaProfila(r, "/"); p != "/profile" {
		t.Errorf("lozinka sa stranice kodova vraća se na %q", p)
	}
	r.Header.Set("Referer", "http://example.com/profile")
	if p := povratnaAdresaProfila(r, "/"); p != "/profile" {
		t.Errorf("s profila na profil, ne %q", p)
	}
	if n, kad, _ := o.dk.StanjeRezervnih(ctx, ana.ID); n != 10 || kad == nil {
		t.Fatalf("stanje rezervnih: %d %v", n, kad)
	}

	// profil poslije ih više ne pokazuje, samo broj
	w = o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodGet, "/profile", nil)
	if uzorakRezervnog.MatchString(w.Body.String()) || !strings.Contains(w.Body.String(), "<strong>10</strong> od 10") {
		t.Fatalf("profil poslije kodova: kodovi se ne smiju ponoviti, broj mora stajati")
	}
	if w.Header().Get("Cache-Control") == "no-store" {
		t.Error("obični profil ne treba no-store")
	}

	w = o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodPost, "/profile/rezervni-kodovi.txt", url.Values{"lozinka": {"anina-lozinka"}})
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(w.Header().Get("Content-Disposition"), `attachment; filename="gocop-rezervni-kodovi-ana.txt"`) ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf(".txt: %d %v", w.Code, w.Header())
	}
	drugi := uzorakRezervnog.FindAllString(w.Body.String(), -1)
	if len(drugi) != 10 || drugi[0] == prvi[0] {
		t.Fatalf(".txt mora napraviti novi niz: %v (prije %v)", drugi, prvi)
	}
	if b := w.Body.String(); !strings.Contains(b, "Čvor: cop-laptop") || !strings.Contains(b, "samo na čvoru cop-laptop") || !strings.Contains(b, "na javnom čvoru") {
		t.Errorf(".txt mora imenovati čvor na kojem kodovi vrijede:\n%s", b)
	}
}

// Tuđim očima se ne prave kodovi, ne zaboravljaju računala i ne izdaje
// privremeni kod, ni kad je upis tuđim očima uključen; profil tada ne
// pokazuje tuđa računala.
func TestTudjimOcimaNemaKodovaNiRacunala(t *testing.T) {
	o := novaOkolinaIzvana(t)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ana := o.racun("ana", "anina-lozinka", "ana@voda.hr", false)
	ivo := o.racun("ivo", "ivina-lozinka", "ivo@voda.hr", false)
	ctx := context.Background()
	anaIzBaze, _ := o.repo.GetUserByID(ana.ID)
	if _, err := o.dk.ZapamtiRacunalo(ctx, anaIzBaze, "", "198.51.100.7", "Firefox na Windowsu"); err != nil {
		t.Fatal(err)
	}
	tudje := zahtjevIzvana{stvarni: admin, vidi: ana}

	for _, slucaj := range []struct {
		put   string
		polja url.Values
	}{
		{"/profile/rezervni-kodovi", url.Values{"lozinka": {"upravina-lozinka"}}},
		{"/profile/rezervni-kodovi", url.Values{"lozinka": {"anina-lozinka"}}},
		{"/profile/rezervni-kodovi.txt", url.Values{"lozinka": {"upravina-lozinka"}}},
		{"/profile/racunala/zaboravi-sva", url.Values{}},
		{"/users/" + ivo.ID.String() + "/kod-prijave", url.Values{}},
	} {
		w := o.posalji(tudje, http.MethodPost, slucaj.put, slucaj.polja)
		if g := greskaPreusmjerenja(t, w); !strings.Contains(g, "tuđim očima") {
			t.Errorf("%s tuđim očima: %d %q", slucaj.put, w.Code, g)
		}
	}
	for _, u := range []*models.User{admin, ana} {
		if n, _, _ := o.dk.StanjeRezervnih(ctx, u.ID); n != 0 {
			t.Errorf("%s ima %d rezervnih kodova napravljenih tuđim očima", u.Username, n)
		}
	}
	if r, _ := o.dk.Racunala(ctx, ana.ID, ""); len(r) != 1 {
		t.Errorf("tuđe oči zaboravile su Anino računalo: %v", r)
	}

	w := o.posalji(tudje, http.MethodGet, "/profile", nil)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Firefox na Windowsu") || !strings.Contains(w.Body.String(), "tuđim očima") {
		t.Fatalf("profil tuđim očima ne smije pokazati računala: %d", w.Code)
	}
}

// Zapamćeno računalo zaboravlja samo njegova osoba; tuđe se ne da ni
// pogađanjem oznake.
func TestZaboraviRacunaloSamoSvoje(t *testing.T) {
	o := novaOkolinaIzvana(t)
	ana := o.racun("ana", "anina-lozinka", "ana@voda.hr", false)
	ivo := o.racun("ivo", "ivina-lozinka", "ivo@voda.hr", false)
	ctx := context.Background()
	for _, u := range []*models.User{ana, ivo} {
		x, _ := o.repo.GetUserByID(u.ID)
		if _, err := o.dk.ZapamtiRacunalo(ctx, x, "", "198.51.100.7", "Safari na iPhoneu"); err != nil {
			t.Fatal(err)
		}
	}
	ivina, _ := o.dk.Racunala(ctx, ivo.ID, "")
	w := o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodPost, "/profile/racunala/"+ivina[0].ID+"/zaboravi", url.Values{})
	if greskaPreusmjerenja(t, w) == "" {
		t.Fatal("Ana je zaboravila Ivino računalo")
	}
	if r, _ := o.dk.Racunala(ctx, ivo.ID, ""); len(r) != 1 {
		t.Fatal("Ivino računalo je nestalo")
	}

	w = o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodGet, "/profile", nil)
	if !strings.Contains(w.Body.String(), "Safari na iPhoneu") || !strings.Contains(w.Body.String(), "a***@voda.hr") {
		t.Fatal("profil mora pokazati zapamćeno računalo i maskiranu adresu za PIN")
	}
	anina, _ := o.dk.Racunala(ctx, ana.ID, "")
	w = o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodPost, "/profile/racunala/"+anina[0].ID+"/zaboravi", url.Values{})
	if g := greskaPreusmjerenja(t, w); g != "" || w.Code != http.StatusSeeOther {
		t.Fatalf("zaboravi svoje: %d %q", w.Code, g)
	}
	if r, _ := o.dk.Racunala(ctx, ana.ID, ""); len(r) != 0 {
		t.Fatal("Anino računalo nije zaboravljeno")
	}
}

// Privremeni kod izdaje tko smije poništiti lozinku, nikad sebi; stranica
// s kodom i stranica s privremenom lozinkom ne spremaju se u pregledniku.
// Uz kvačicu „i kod” kod se izdaje poslije poništavanja, pa vrijedi.
func TestPrivremeniKodOvlastiIBezSpremanja(t *testing.T) {
	o := novaOkolinaIzvana(t)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ana := o.racun("ana", "anina-lozinka", "", false)
	ivo := o.racun("ivo", "ivina-lozinka", "ivo@voda.hr", false)
	ctx := context.Background()
	uzorakKoda := regexp.MustCompile(`P-[A-Z2-9]{4}-[A-Z2-9]{4}`)

	if g := greskaPreusmjerenja(t, o.posalji(zahtjevIzvana{stvarni: ivo}, http.MethodPost, "/users/"+ana.ID.String()+"/kod-prijave", url.Values{})); g == "" {
		t.Fatal("obični djelatnik izdao je kod drugome")
	}
	if g := greskaPreusmjerenja(t, o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/"+admin.ID.String()+"/kod-prijave", url.Values{})); g == "" {
		t.Fatal("administrator je izdao kod sebi")
	}

	o.usersH.SetCvor(func() string { return "cop-laptop" })
	w := o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/"+ana.ID.String()+"/kod-prijave", url.Values{})
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !uzorakKoda.MatchString(w.Body.String()) {
		t.Fatalf("kod za Anu: %d %q %v", w.Code, w.Header().Get("Cache-Control"), uzorakKoda.MatchString(w.Body.String()))
	}
	// kod vrijedi samo na ovom čvoru: stranica ga imenuje i upućuje na javni
	if b := w.Body.String(); !strings.Contains(b, `<span class="mono">cop-laptop</span>`) || !strings.Contains(b, "na javnom čvoru") {
		t.Error("uz privremeni kod mora stajati čvor na kojem vrijedi")
	}

	w = o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/"+ivo.ID.String()+"/reset-password", url.Values{})
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || uzorakKoda.MatchString(w.Body.String()) {
		t.Fatalf("privremena lozinka bez koda: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}

	w = o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/"+ana.ID.String()+"/reset-password", url.Values{"i_kod": {"1"}})
	kod := uzorakKoda.FindString(w.Body.String())
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || kod == "" || !strings.Contains(w.Body.String(), "Privremena lozinka za") {
		t.Fatalf("poništavanje s kodom: %d %q kod=%q", w.Code, w.Header().Get("Cache-Control"), kod)
	}
	// kod izdan uz poništavanje vrijedi za prijavu izvana
	anaIzBaze, _ := o.repo.GetUserByID(ana.ID)
	p, err := o.dk.ZapocniPrijavu(ctx, anaIzBaze, "198.51.100.7", "test")
	if err != nil {
		t.Fatal(err)
	}
	ishod, err := o.dk.ProvjeriKod(ctx, p.Token, kod)
	if err != nil || ishod.Vrsta != service.VrstaPrivremeni {
		t.Fatalf("kod izdan uz poništavanje ne vrijedi: %v %+v", err, ishod)
	}
}

// Sklopka: PIN se ne uključuje bez pošiljatelja ni prije probnog PIN-a;
// nakon probe se uključuje, a isključuje uvijek. Tuđim očima ništa.
func TestSklopkaPINaTekNakonProbe(t *testing.T) {
	o := novaOkolinaIzvana(t)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ana := o.racun("ana", "anina-lozinka", "ana@voda.hr", true)
	ctx := context.Background()
	// sklopka se uključuje sa čvora koji prima prijave izvana
	ja := zahtjevIzvana{stvarni: admin, izvana: true}
	ukljuci := url.Values{"ukljuci": {"1"}, "domena": {"voda.hr"}}

	if g := greskaPreusmjerenja(t, o.posalji(ja, http.MethodPost, "/administracija/posta/pin/sklopka", ukljuci)); !strings.Contains(g, service.ErrNemaPosiljatelja.Error()) {
		t.Fatalf("bez pošiljatelja: %q", g)
	}
	w := o.posalji(ja, http.MethodPost, "/administracija/posta/pin", url.Values{"radnja": {"spremi"}, "korisnik": {"pinpost"}, "lozinka": {"domena-1"}, "adresa": {"pin.post@voda.hr"}})
	if g := greskaPreusmjerenja(t, w); g != "" {
		t.Fatalf("spremanje pošiljatelja: %q", g)
	}
	if g := greskaPreusmjerenja(t, o.posalji(ja, http.MethodPost, "/administracija/posta/pin/sklopka", ukljuci)); !strings.Contains(g, service.ErrProbaPotrebna.Error()) {
		t.Fatalf("prije probe: %q", g)
	}
	if o.dk.Ukljuceno(ctx) {
		t.Fatal("PIN je uključen prije probe")
	}

	// tuđim očima (drugi administrator) ni proba ni sklopka
	tudje := zahtjevIzvana{stvarni: admin, vidi: ana}
	for _, put := range []string{"/administracija/posta/pin/proba", "/administracija/posta/pin/sklopka", "/administracija/posta/pin"} {
		if g := greskaPreusmjerenja(t, o.posalji(tudje, http.MethodPost, put, ukljuci)); !strings.Contains(g, "tuđim očima") {
			t.Errorf("%s tuđim očima: %q", put, g)
		}
	}
	if len(o.postar.poslano) != 0 {
		t.Fatalf("tuđim očima poslano: %d", len(o.postar.poslano))
	}

	if g := greskaPreusmjerenja(t, o.posalji(ja, http.MethodPost, "/administracija/posta/pin/proba", url.Values{})); g != "" {
		t.Fatalf("proba: %q", g)
	}
	if len(o.postar.poslano) != 1 || o.postar.poslano[0].Za.Address != "uprava@voda.hr" || !o.postar.poslano[0].BezKopije {
		t.Fatalf("probni PIN: %+v", o.postar.poslano)
	}
	if g := greskaPreusmjerenja(t, o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/administracija/posta/pin/sklopka", ukljuci)); !strings.Contains(g, service.ErrUkljuciIzvana.Error()) || o.dk.Ukljuceno(ctx) {
		t.Fatalf("iz lokalne mreže: %q", g)
	}
	if g := greskaPreusmjerenja(t, o.posalji(ja, http.MethodPost, "/administracija/posta/pin/sklopka", ukljuci)); g != "" || !o.dk.Ukljuceno(ctx) {
		t.Fatalf("nakon probe: %q, uključeno %v", g, o.dk.Ukljuceno(ctx))
	}
	if g := greskaPreusmjerenja(t, o.posalji(ja, http.MethodPost, "/administracija/posta/pin/sklopka", url.Values{"ukljuci": {"0"}})); g != "" || o.dk.Ukljuceno(ctx) {
		t.Fatalf("isključivanje: %q", g)
	}

	// samo globalni administrator, i kad bi ograda rute popustila
	ivo := o.racun("ivo", "ivina-lozinka", "ivo@voda.hr", false)
	if g := greskaPreusmjerenja(t, o.posalji(zahtjevIzvana{stvarni: ivo}, http.MethodPost, "/administracija/posta/pin/sklopka", url.Values{"ukljuci": {"0"}})); g == "" {
		t.Fatal("obični djelatnik dirao je sklopku")
	}
}

// Vlastita adresa e-pošte (kamo ide PIN): izvana se ne mijenja, inače samo
// uz trenutnu lozinku (kriva se broji) i na dopuštenu domenu; profil izvana
// polje zaključa i kaže zašto.
func TestPromjenaVlastiteAdreseNaProfilu(t *testing.T) {
	o := novaOkolinaIzvana(t)
	ana := o.racun("ana", "anina-lozinka", "ana@voda.hr", false)
	adresa := func() string {
		u, _ := o.repo.GetUserByID(ana.ID)
		return u.Email
	}
	polja := func(email, lozinka string) url.Values {
		return url.Values{"full_name": {"Ana"}, "email": {email}, "trenutna_lozinka": {lozinka}}
	}

	w := o.posalji(zahtjevIzvana{stvarni: ana, izvana: true}, http.MethodPost, "/profile/update", polja("napadac@voda.hr", "anina-lozinka"))
	if !strings.Contains(w.Header().Get("Location"), url.QueryEscape(service.ErrPromjenaAdreseIzvana.Error())) || adresa() != "ana@voda.hr" {
		t.Fatalf("izvana: %s, adresa %s", w.Header().Get("Location"), adresa())
	}
	w = o.posalji(zahtjevIzvana{stvarni: ana, izvana: true}, http.MethodGet, "/profile", nil)
	if !strings.Contains(w.Body.String(), "ne možete mijenjati izvana") || !regexp.MustCompile(`id="email"[^>]*readonly`).MatchString(w.Body.String()) {
		t.Fatal("profil izvana mora zaključati adresu i reći zašto")
	}
	// spremanje ostalih kontakata izvana i dalje radi
	if w := o.posalji(zahtjevIzvana{stvarni: ana, izvana: true}, http.MethodPost, "/profile/update", polja("ana@voda.hr", "")); strings.Contains(w.Header().Get("Location"), "error=") {
		t.Fatalf("kontakti izvana bez promjene adrese: %s", w.Header().Get("Location"))
	}

	lokalno := zahtjevIzvana{stvarni: ana}
	if w := o.posalji(lokalno, http.MethodPost, "/profile/update", polja("ana.nova@voda.hr", "")); !strings.Contains(w.Header().Get("Location"), "error=") || adresa() != "ana@voda.hr" {
		t.Fatalf("bez lozinke: %s", w.Header().Get("Location"))
	}
	if w := o.posalji(lokalno, http.MethodPost, "/profile/update", polja("ana.nova@voda.hr", "kriva")); !strings.Contains(w.Header().Get("Location"), "error=") || adresa() != "ana@voda.hr" {
		t.Fatalf("kriva lozinka: %s", w.Header().Get("Location"))
	}
	if w := o.posalji(lokalno, http.MethodPost, "/profile/update", polja("ana@gmail.com", "anina-lozinka")); !strings.Contains(w.Header().Get("Location"), "error=") || adresa() != "ana@voda.hr" {
		t.Fatalf("tuđa domena: %s", w.Header().Get("Location"))
	}
	// kriva lozinka broji se kao pri promjeni lozinke: još dvije i blok
	for i := 0; i < 2; i++ {
		o.posalji(lokalno, http.MethodPost, "/profile/update", polja("ana.nova@voda.hr", "kriva"))
	}
	if w := o.posalji(lokalno, http.MethodPost, "/profile/update", polja("ana.nova@voda.hr", "anina-lozinka")); !strings.Contains(w.Header().Get("Location"), "error=") || adresa() != "ana@voda.hr" {
		t.Fatalf("nakon tri kriva upisa mora biti blokirano: %s", w.Header().Get("Location"))
	}
	ponovnaLozinka.Reset(kljucPonovneLozinke("", ana.ID.String()))
	if w := o.posalji(lokalno, http.MethodPost, "/profile/update", polja("ana.nova@voda.hr", "anina-lozinka")); strings.Contains(w.Header().Get("Location"), "error=") || adresa() != "ana.nova@voda.hr" {
		t.Fatalf("točna lozinka iz ureda: %s, adresa %s", w.Header().Get("Location"), adresa())
	}
}

// Obrazac djelatnika: nova lozinka za drugoga opoziva njegova zapamćena
// računala; svoju lozinku administrator tamo ne mijenja (mijenja je na
// profilu, uz trenutnu).
func TestObrazacDjelatnikaILozinka(t *testing.T) {
	o := novaOkolinaIzvana(t)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ana := o.racun("ana", "anina-lozinka", "ana@voda.hr", false)
	ctx := context.Background()
	anaIzBaze, _ := o.repo.GetUserByID(ana.ID)
	if _, err := o.dk.ZapamtiRacunalo(ctx, anaIzBaze, "", "198.51.100.7", "Chrome"); err != nil {
		t.Fatal(err)
	}
	obrazac := func(u *models.User, lozinka string) url.Values {
		return url.Values{"id": {u.ID.String()}, "username": {u.Username}, "full_name": {u.FullName}, "email": {u.Email},
			"org_type": {string(models.OrgHrvatskeVode)}, "is_active": {"1"}, "password": {lozinka}}
	}
	w := o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/update", obrazac(ana, "nova-anina-1"))
	if g := greskaPreusmjerenja(t, w); g != "" {
		t.Fatalf("nova lozinka za Anu: %q", g)
	}
	if r, _ := o.dk.Racunala(ctx, ana.ID, ""); len(r) != 0 {
		t.Fatalf("nova lozinka iz obrasca nije zaboravila Anina računala: %v", r)
	}

	stari, _ := o.repo.GetUserByID(admin.ID)
	w = o.posalji(zahtjevIzvana{stvarni: admin}, http.MethodPost, "/users/update", obrazac(admin, "nova-upravina-1"))
	if !strings.Contains(greskaPreusmjerenja(t, w), "na profilu") {
		t.Fatalf("svoja lozinka kroz obrazac: %s", w.Header().Get("Location"))
	}
	if novi, _ := o.repo.GetUserByID(admin.ID); novi.PasswordHash != stari.PasswordHash {
		t.Fatal("administrator je kroz obrazac promijenio svoju lozinku")
	}
}

// Filtar „bez e-pošte” nalazi aktivne račune kojima PIN nema kamo ići.
func TestFiltarBezEposte(t *testing.T) {
	o := novaOkolinaIzvana(t)
	o.racun("ana", "anina-lozinka", "ana@voda.hr", false)
	bez := o.racun("ivo", "ivina-lozinka", "", false)
	praznine := o.racun("eva", "evina-lozinka", "  ", false)
	ugasen := o.racun("ugo", "ugova-lozinka", "", false)
	ugasen.IsActive = false
	if err := o.repo.UpdateUser(ugasen); err != nil {
		t.Fatal(err)
	}
	popis, err := o.users.ListUsers("", 0, "", "", string(repository.StanjeBezEposte))
	if err != nil {
		t.Fatal(err)
	}
	nadjeni := map[string]bool{}
	for _, u := range popis {
		nadjeni[u.Username] = true
	}
	if len(popis) != 2 || !nadjeni[bez.Username] || !nadjeni[praznine.Username] {
		t.Fatalf("bez e-pošte: %v", nadjeni)
	}
}

// Stranica e-pošte: gumb za uključivanje PIN-a stoji onemogućen dok probni
// PIN nije prošao, a odbijena lozinka pošiljatelja vidi se i zaustavlja ga.
func TestStranicaPosteSklopkaIStanjePosiljatelja(t *testing.T) {
	o := novaOkolinaIzvana(t)
	admin := o.racun("uprava", "upravina-lozinka", "uprava@voda.hr", true)
	ctx := context.Background()
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("administracija_posta.html")...)
	if err != nil {
		t.Fatal(err)
	}
	stranicaZ := func(z zahtjevIzvana) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/administracija/posta", nil)
		r.RemoteAddr = "192.168.1.50:40000"
		if z.izvana {
			r.RemoteAddr = "198.51.100.7:40000"
			r.TLS = &tls.ConnectionState{}
		}
		if z.bezHTTPS {
			r.RemoteAddr = "127.0.0.1:5555"
			r.Header.Set("CF-Connecting-IP", "198.51.100.7")
		}
		p, err := o.aktiH.pinStanje(r)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		d := AdminPostaData{CurrentUser: admin, Permissions: &models.UserPermissions{User: *admin, IsGlobalAdmin: true}, PIN: p}
		if err := tp.ExecuteTemplate(&b, "administracija_posta.html", d); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	stranicaS := func(izvana bool) string { t.Helper(); return stranicaZ(zahtjevIzvana{izvana: izvana}) }
	stranica := func() string { t.Helper(); return stranicaS(true) }
	gumb := regexp.MustCompile(`<button[^>]*name="ukljuci" value="1"[^>]*>`)
	ja := zahtjevIzvana{stvarni: admin, izvana: true}

	if g := gumb.FindString(stranica()); !strings.Contains(g, "disabled") {
		t.Fatalf("bez pošiljatelja gumb mora biti onemogućen: %s", g)
	}
	o.posalji(ja, http.MethodPost, "/administracija/posta/pin", url.Values{"radnja": {"spremi"}, "korisnik": {"pinpost"}, "lozinka": {"domena-1"}, "adresa": {"pin.post@voda.hr"}})
	if s := stranica(); !strings.Contains(gumb.FindString(s), "disabled") || !strings.Contains(s, "pin.post@voda.hr") {
		t.Fatal("prije probe gumb mora biti onemogućen, a pošiljatelj vidljiv")
	}
	o.posalji(ja, http.MethodPost, "/administracija/posta/pin/proba", url.Values{})
	if g := gumb.FindString(stranica()); g == "" || strings.Contains(g, "disabled") {
		t.Fatalf("nakon probe gumb mora raditi: %s", g)
	}
	// kroz posrednika koji ne javlja HTTPS PIN se ne uključuje: svaka bi se
	// prijava izvana odbila, a stranica kaže zašto
	bez := zahtjevIzvana{stvarni: admin, bezHTTPS: true}
	if s := stranicaZ(bez); !strings.Contains(gumb.FindString(s), "disabled") || !strings.Contains(s, `id="pin-bez-https"`) {
		t.Fatalf("posrednik bez HTTPS-a: gumb %s", gumb.FindString(s))
	}
	w := o.posalji(bez, http.MethodPost, "/administracija/posta/pin/sklopka", url.Values{"ukljuci": {"1"}})
	if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "nije stigla preko HTTPS-a") || o.dk.Ukljuceno(ctx) {
		t.Fatalf("uključivanje kroz posrednika bez HTTPS-a: %s", loc)
	}
	// iz lokalne mreže gumb ne radi ni nakon probe, a stranica kaže zašto
	if s := stranicaS(false); !strings.Contains(gumb.FindString(s), "disabled") || !strings.Contains(s, "otvorena je iz lokalne mreže") || !strings.Contains(s, "Uključuje se na javnom čvoru") {
		t.Fatalf("iz lokalne mreže PIN se ne uključuje: %s", gumb.FindString(s))
	}
	if s := stranica(); strings.Contains(s, "pin-bez-posiljatelja") {
		t.Fatal("dok je PIN isključen nema uzbune o pošiljatelju")
	}
	if err := o.dk.PostaviUkljuceno(ctx, &models.UserPermissions{User: *admin, IsGlobalAdmin: true}, true, true); err != nil {
		t.Fatal(err)
	}

	// poslužitelj odbije lozinku: račun se označi, slanje stane, stranica to kaže
	o.postar.greska = posta.ErrPrijava
	o.posalji(ja, http.MethodPost, "/administracija/posta/pin/proba", url.Values{})
	s := stranica()
	if !strings.Contains(s, "odbio je lozinku") {
		t.Fatal("odbijena lozinka pošiljatelja mora se vidjeti")
	}
	// čvor koji nije primio prijavu izvana ne diže uzbunu, nego kaže da mu
	// pošiljatelj treba tek kad se kroz njega ulazi izvana
	if strings.Contains(s, `id="pin-bez-posiljatelja"`) || !strings.Contains(s, `id="pin-bez-prijava-izvana"`) {
		t.Fatal("uzbuna o pošiljatelju na čvoru bez prijava izvana")
	}
	// PIN je uključen, čvor prima prijave izvana, a nema ih čime slati:
	// uzbuna na vrhu odjeljka i na ulaznoj stranici administracije
	o.dk.TrebaDrugiKorak(ctx, true)
	s = stranica()
	if strings.Contains(s, `id="pin-bez-prijava-izvana"`) {
		t.Fatal("napomena o čvoru bez prijava izvana uz uzbunu")
	}
	if !strings.Contains(s, `id="pin-bez-posiljatelja"`) || !strings.Contains(s, "nema ispravnog računa za slanje PIN-a") {
		t.Fatal("uključen PIN bez ispravnog pošiljatelja mora dići uzbunu na stranici e-pošte")
	}
	adminH := &AdminHandler{}
	adminH.SetDrugiKorak(func() *service.DrugiKorak { return o.dk })
	tpA, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("administracija.html")...)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	d := AdminPageData{CurrentUser: admin, Permissions: &models.UserPermissions{User: *admin, IsGlobalAdmin: true}, Sectors: 1,
		PINBezPosiljatelja: adminH.drugiKorak().BezPosiljatelja(ctx)}
	if err := tpA.ExecuteTemplate(&b, "administracija.html", d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `id="pin-bez-posiljatelja"`) {
		t.Fatal("uzbuna o pošiljatelju mora stajati i na ulaznoj stranici administracije")
	}
}
