package web

// Računi izvana (Odluke (30. 10. 2026.), 12): novi globalni administrator,
// davanje i oduzimanje tog prava, tuđa lozinka globalnog administratora i
// kod za prijavu izvana traže izvana lozinku vlastitog računa, a s ovog
// računala i iz lokalne mreže ne. Odbijeni zahtjev ide u dnevnik programa.

import (
	"bytes"
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// okolinaRacuna je registar djelatnika s globalnim administratorom Anom
// (lozinka "anina-lozinka") i rukovateljem obrazaca
type okolinaRacuna struct {
	t     *testing.T
	baza  *sql.DB
	repo  *repository.UserRepository
	ana   *models.User
	perms *models.UserPermissions
	srv   http.Handler
}

func novaOkolinaRacuna(t *testing.T) *okolinaRacuna {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "racuni.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := baza.Close(); err != nil {
			t.Error(err)
		}
	})
	repo := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	auth := service.NewAuthService(repo, repository.NewSessionRepository(baza))
	users := service.NewUserService(repo, auth, service.NewSSEBroker())
	hash, err := auth.HashPassword("anina-lozinka")
	if err != nil {
		t.Fatal(err)
	}
	ana := &models.User{ID: uuid.New(), Username: "aivic", FullName: "Ana Ivić", PasswordHash: hash, IsGlobalAdmin: true, IsActive: true}
	if err := repo.CreateUser(ana, nil); err != nil {
		t.Fatal(err)
	}
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	detalj, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("user_detail.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewUsersHandler(users, nil)
	h.SetPageTemplates(detalj, nil, nil, nil)
	h.SetLozinkaRacuna(auth.ProvjeriLozinku)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /users/create", h.HandleCreateUser)
	mux.HandleFunc("POST /users/update", h.HandleUpdateUser)
	mux.HandleFunc("POST /users/{id}/reset-password", h.HandleResetPassword)
	mux.HandleFunc("POST /users/{id}/kod-prijave", h.HandleKodPrijave)
	mux.HandleFunc("POST /users/duty/add", h.HandleAddDuty)
	mux.HandleFunc("POST /users/duties/{duty}/update", h.HandleUpdateDuty)
	mux.HandleFunc("POST /users/duty/revoke", h.HandleRevokeDuty)
	mux.HandleFunc("POST /users/delete", h.HandleDeleteUser)
	ponovnaLozinka = newLoginLimiter()
	t.Cleanup(func() { ponovnaLozinka = newLoginLimiter() })
	return &okolinaRacuna{t: t, baza: baza, repo: repo, ana: ana, perms: &models.UserPermissions{IsGlobalAdmin: true, User: *ana},
		srv: (&Server{}).klijentSloj(mux)}
}

// osoba upiše račun; admin mu daje zastavicu globalnog administratora
func (o *okolinaRacuna) osoba(ime string, admin bool) *models.User {
	o.t.Helper()
	u := &models.User{ID: uuid.New(), Username: ime, FullName: ime, PasswordHash: "sazetak", OrgType: models.OrgHrvatskeVode, IsActive: true, IsGlobalAdmin: admin}
	if err := o.repo.CreateUser(u, nil); err != nil {
		o.t.Fatal(err)
	}
	return u
}

// salji šalje obrazac kao Ana i vraća odgovor
func (o *okolinaRacuna) salji(porijeklo func(*http.Request) *http.Request, putanja string, forma url.Values) *httptest.ResponseRecorder {
	o.t.Helper()
	r := porijeklo(httptest.NewRequest(http.MethodPost, putanja, strings.NewReader(forma.Encode())))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(r.Context(), contextKeyUser, o.ana)
	ctx = context.WithValue(ctx, contextKeyPerms, o.perms)
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, r.WithContext(ctx))
	return w
}

// greska je poruka greške iz preusmjeravanja, prazna kad je nema
func greska(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("error")
}

// izmjena je obrazac izmjene računa u
func izmjena(u *models.User, admin bool, lozinka, lozinkaRacuna string) url.Values {
	f := url.Values{"id": {u.ID.String()}, "username": {u.Username}, "full_name": {u.FullName},
		"org_type": {string(models.OrgHrvatskeVode)}, "is_active": {"1"}, "password": {lozinka}, "lozinka_racuna": {lozinkaRacuna}}
	if admin {
		f.Set("is_global_admin", "1")
	}
	return f
}

func TestGlobalniAdministratorIzvanaUzLozinku(t *testing.T) {
	dnevnik := dnevnikPrograma(t)
	o := novaOkolinaRacuna(t)
	tunel := krozTunel("198.51.100.7")
	novi := func(ime string, admin bool, lozinkaRacuna string) url.Values {
		f := url.Values{"username": {ime}, "password": {"pocetna-lozinka"}, "full_name": {"Novi Novak"},
			"org_type": {string(models.OrgHrvatskeVode)}, "lozinka_racuna": {lozinkaRacuna}}
		if admin {
			f.Set("is_global_admin", "1")
		}
		return f
	}

	// novi globalni administrator
	if g := greska(t, o.salji(tunel, "/users/create", novi("nnovak", true, ""))); !strings.Contains(g, "uz lozinku vašeg računa") {
		t.Errorf("novi administrator izvana bez lozinke: %q", g)
	}
	if g := greska(t, o.salji(tunel, "/users/create", novi("nnovak", true, "kriva"))); !strings.Contains(g, "lozinka nije točna") {
		t.Errorf("novi administrator izvana s krivom lozinkom: %q", g)
	}
	for ime, c := range map[string]struct {
		porijeklo func(*http.Request) *http.Request
		forma     url.Values
	}{
		"izvana s lozinkom":    {javnoIzravno, novi("ppetric", true, "anina-lozinka")},
		"iz lokalne mreže":     {izravno, novi("iivancic", true, "")},
		"izvana bez zastavice": {tunel, novi("mmatic", false, "")},
	} {
		w := o.salji(c.porijeklo, "/users/create", c.forma)
		if g := greska(t, w); g != "" || !strings.Contains(w.Header().Get("Location"), "success=") {
			t.Errorf("novi račun %s: %q %q", ime, w.Header().Get("Location"), g)
		}
	}

	// davanje i oduzimanje zastavice
	mara := o.osoba("mmaric", false)
	if g := greska(t, o.salji(tunel, "/users/update", izmjena(mara, true, "", ""))); !strings.Contains(g, "uz lozinku vašeg računa") {
		t.Errorf("davanje prava izvana bez lozinke: %q", g)
	}
	if g := greska(t, o.salji(tunel, "/users/update", izmjena(mara, true, "", "anina-lozinka"))); g != "" {
		t.Errorf("davanje prava izvana s lozinkom: %q", g)
	}
	if g := greska(t, o.salji(tunel, "/users/update", izmjena(mara, false, "", ""))); !strings.Contains(g, "uz lozinku vašeg računa") {
		t.Errorf("oduzimanje prava izvana bez lozinke: %q", g)
	}
	// lozinka računa koji jest globalni administrator
	if g := greska(t, o.salji(tunel, "/users/update", izmjena(mara, true, "nova-lozinka-12", ""))); !strings.Contains(g, "uz lozinku vašeg računa") {
		t.Errorf("lozinka administratora izvana bez lozinke: %q", g)
	}
	if g := greska(t, o.salji(izravno, "/users/update", izmjena(mara, false, "nova-lozinka-12", ""))); g != "" {
		t.Errorf("oduzimanje prava i lozinka iz lokalne mreže: %q", g)
	}
	// izvana bez promjene prava i lozinka računa koji nije administrator
	if g := greska(t, o.salji(tunel, "/users/update", izmjena(mara, false, "druga-lozinka-12", ""))); g != "" {
		t.Errorf("lozinka djelatnika izvana: %q", g)
	}
	// svoju lozinku administrator mijenja na profilu, i s lozinkom računa
	if g := greska(t, o.salji(tunel, "/users/update", izmjena(o.ana, true, "nova-lozinka-12", "anina-lozinka"))); !strings.Contains(g, "na profilu") {
		t.Errorf("vlastita lozinka na obrascu djelatnika: %q", g)
	}
	if sad, err := o.repo.GetUserByID(mara.ID); err != nil || sad.IsGlobalAdmin {
		t.Errorf("Mara nakon izmjena: %+v %v", sad, err)
	}

	if !strings.Contains(dnevnik.String(), `izvana bez točne lozinke računa odbijeno: "Ana Ivić (aivic)", POST "/users/create", s adrese 198.51.100.7`) ||
		!strings.Contains(dnevnik.String(), `POST "/users/update"`) {
		t.Errorf("dnevnik programa nema odbijene zahtjeve:\n%s", dnevnik.String())
	}
	if strings.Contains(dnevnik.String(), "anina-lozinka") {
		t.Error("lozinka je u dnevniku programa")
	}
}

func TestPonistenjeIKodIzvanaUzLozinku(t *testing.T) {
	o := novaOkolinaRacuna(t)
	tunel := krozTunel("198.51.100.7")
	sef := o.osoba("sef", true)
	mara := o.osoba("mmaric", false)
	ponisti := func(u *models.User) string { return "/users/" + u.ID.String() + "/reset-password" }
	kod := "/users/" + mara.ID.String() + "/kod-prijave"

	for ime, c := range map[string]struct {
		porijeklo func(*http.Request) *http.Request
		putanja   string
		forma     url.Values
		trazi     bool
	}{
		"lozinka administratora izvana":     {tunel, ponisti(sef), url.Values{}, true},
		"lozinka i kod izvana":              {tunel, ponisti(mara), url.Values{"i_kod": {"1"}}, true},
		"kod izvana":                        {tunel, kod, url.Values{}, true},
		"lozinka administratora s lozinkom": {tunel, ponisti(sef), url.Values{"lozinka_racuna": {"anina-lozinka"}}, false},
		"lozinka djelatnika izvana":         {tunel, ponisti(mara), url.Values{}, false},
		"lozinka administratora lokalno":    {izravno, ponisti(sef), url.Values{}, false},
		"kod izvana s lozinkom":             {tunel, kod, url.Values{"lozinka_racuna": {"anina-lozinka"}}, false},
		"kod iz lokalne mreže":              {izravno, kod, url.Values{}, false},
	} {
		w := o.salji(c.porijeklo, c.putanja, c.forma)
		g := greska(t, w)
		odbijeno := strings.Contains(g, "lozinku vašeg računa") || strings.Contains(g, "lozinka nije točna")
		if odbijeno != c.trazi {
			t.Errorf("%s: %d %q", ime, w.Code, g)
		}
	}
	// bez drugog koraka kod se ne izdaje, ali tek iza provjere lozinke
	if g := greska(t, o.salji(izravno, kod, url.Values{})); !strings.Contains(g, "drugi korak") {
		t.Errorf("kod bez drugog koraka: %q", g)
	}
	if sad, err := o.repo.GetUserByID(sef.ID); err != nil || !sad.MustChangePassword {
		t.Errorf("lozinka administratora nije poništena: %+v %v", sad, err)
	}

	// krivi upisi broje se kao pri promjeni lozinke
	for range 3 {
		o.salji(tunel, kod, url.Values{"lozinka_racuna": {"kriva"}})
	}
	if g := greska(t, o.salji(tunel, kod, url.Values{"lozinka_racuna": {"kriva"}})); !strings.Contains(g, "lozinka nije točna") && !strings.Contains(g, "previše krivih lozinki") {
		t.Errorf("kod izvana s krivom lozinkom: %q", g)
	}
	if g := greska(t, o.salji(tunel, kod, url.Values{"lozinka_racuna": {"anina-lozinka"}})); !strings.Contains(g, "previše krivih lozinki") {
		t.Errorf("točna lozinka nakon tri krive: %q", g)
	}
	if g := greska(t, o.salji(izravno, kod, url.Values{})); strings.Contains(g, "lozink") {
		t.Errorf("iz lokalne mreže nakon krivih upisa: %q", g)
	}
}

func TestDiraAdministratora(t *testing.T) {
	admin, djelatnik := &models.User{IsGlobalAdmin: true, IsActive: true}, &models.User{IsActive: true}
	zamjenik := &models.User{IsActive: true, Duties: []models.Duty{{Role: models.RoleMainCenterDeputy, IsActive: true, IsTemporary: true}}}
	for _, c := range []struct {
		ime    string
		target *models.User
		iz     izmjenaRacuna
		dira   bool
	}{
		{"novo pravo", djelatnik, izmjenaRacuna{zastavica: true, aktivan: true}, true},
		{"oduzeto pravo", admin, izmjenaRacuna{aktivan: true}, true},
		{"lozinka administratora", admin, izmjenaRacuna{zastavica: true, aktivan: true, lozinka: "nova"}, true},
		{"isključen administrator", admin, izmjenaRacuna{zastavica: true}, true},
		{"ništa", admin, izmjenaRacuna{zastavica: true, aktivan: true}, false},
		{"lozinka zamjenika po dužnosti", zamjenik, izmjenaRacuna{aktivan: true, lozinka: "nova"}, true},
		{"isključen zamjenik po dužnosti", zamjenik, izmjenaRacuna{}, true},
		{"zamjenik bez promjene", zamjenik, izmjenaRacuna{aktivan: true}, false},
		{"lozinka djelatnika", djelatnik, izmjenaRacuna{aktivan: true, lozinka: "nova"}, false},
		{"isključen djelatnik", djelatnik, izmjenaRacuna{}, false},
		{"nepoznat račun", nil, izmjenaRacuna{lozinka: "nova"}, false},
		{"nepoznat račun sa zastavicom", nil, izmjenaRacuna{zastavica: true}, true},
	} {
		if got := diraAdministratora(c.target, c.iz); got != c.dira {
			t.Errorf("%s: %v", c.ime, got)
		}
	}
	if !trazeLozinkuPonistenja(nil, true) || trazeLozinkuPonistenja(nil, false) || !trazeLozinkuPonistenja(admin, false) ||
		!trazeLozinkuPonistenja(zamjenik, false) || trazeLozinkuPonistenja(djelatnik, false) {
		t.Error("poništenje lozinke")
	}
	istekla := time.Now().Add(-time.Hour)
	bivsi := &models.User{Duties: []models.Duty{{Role: models.RoleGlobalAdmin, IsActive: true, ExpiresAt: &istekla}}}
	if pravoAdministratora(bivsi) || pravoAdministratora(nil) || !pravoAdministratora(zamjenik) {
		t.Error("pravo globalnog administratora po dužnosti")
	}
	if !trazeLozinkuNovog(false, models.RoleGlobalAdmin, false) || !trazeLozinkuNovog(true, "", false) ||
		!trazeLozinkuNovog(false, models.RoleAreaLeader, true) || trazeLozinkuNovog(false, models.RoleAreaLeader, false) {
		t.Error("novi račun")
	}
}

func TestNovaPotvrdaAdrese(t *testing.T) {
	potvrden := &models.User{Email: "vanjski@izvodjac.hr", PINAdresaPotvrdena: "vanjski@izvodjac.hr"}
	for _, c := range []struct {
		ime    string
		target *models.User
		iz     izmjenaRacuna
		nova   bool
	}{
		{"bez potvrde", potvrden, izmjenaRacuna{email: "drugi@izvodjac.hr"}, false},
		{"potvrda iste adrese", potvrden, izmjenaRacuna{potvrda: true, email: " Vanjski@izvodjac.hr"}, false},
		{"potvrda druge adrese", potvrden, izmjenaRacuna{potvrda: true, email: "drugi@izvodjac.hr"}, true},
		{"prva potvrda", &models.User{Email: "drugi@izvodjac.hr"}, izmjenaRacuna{potvrda: true, email: "drugi@izvodjac.hr"}, true},
		{"nepoznat račun", nil, izmjenaRacuna{potvrda: true}, true},
	} {
		if got := novaPotvrdaAdrese(c.target, c.iz); got != c.nova {
			t.Errorf("%s: %v", c.ime, got)
		}
	}
}

// Obrasci djelatnika izvana nude polje za lozinku računa, a iz lokalne
// mreže ne
func TestObrasciDjelatnikaIzvanaPoljeLozinke(t *testing.T) {
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	sef := &models.User{ID: uuid.New(), FullName: "Šef", IsGlobalAdmin: true}
	for _, izvana := range []bool{true, false} {
		for _, p := range []struct {
			stranica string
			data     UserPageData
			polja    int
		}{
			{"user_form.html", UserPageData{CurrentUser: adminIvic, Permissions: admin, User: sef, IsEdit: true, Izvana: izvana}, 1},
			{"user_detail.html", UserPageData{CurrentUser: adminIvic, Permissions: admin, User: sef, CanManage: true, DrugiKorak: true, Izvana: izvana}, 2},
		} {
			var b bytes.Buffer
			if err := predlozakStranice(t, p.stranica).ExecuteTemplate(&b, p.stranica, p.data); err != nil {
				t.Fatal(err)
			}
			n := strings.Count(b.String(), `name="lozinka_racuna"`)
			if (izvana && n != p.polja) || (!izvana && n != 0) {
				t.Errorf("%s izvana=%v: %d polja", p.stranica, izvana, n)
			}
		}
	}
}
