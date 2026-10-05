package web

import (
	"context"
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

// Tko ne smije čitati vodočuvarski dnevnik, ne smije ga ni parafirati,
// upisivati u njega ni zadavati zadatke vodočuvaru, ni izravnim POST-om:
// gost i preglednik s dužnošću na području dobiju zabranu, a list ostane
// bez parafe, upisa i zadatka. Rukovoditelj područja sve to smije.
func TestParafaUpisIZadatakSamoSDnevnikom(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "parafa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica', 'Primjerska 1', '01/000-000', 'cop@primjer.hr')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Primjerica', 'VGI Primjerica', 'Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'd.o. r. Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop-primjer")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	ctx := context.Background()
	p, bp := "P", 1
	osoba := func(korisnik, ime string, d models.Duty) *models.User {
		t.Helper()
		u := &models.User{ID: uuid.New(), Username: korisnik, FullName: ime, IsActive: true}
		d.SectorID, d.IsPrimary = &p, true
		if err := userRepo.CreateUser(u, &d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	vodocuvar := osoba("pperic", "Pero Perić", models.Duty{Title: "Vodočuvar Primjerica", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	rukovoditelj := osoba("iivic", "Ivo Ivić", models.Duty{Title: "Rukovoditelj BP 1", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &bp})
	gost := osoba("aanic", "Ana Anić", models.Duty{Title: "Gost", Role: models.RoleGuest, ScopeType: models.ScopeArea, AreaID: &bp})
	preglednik := osoba("mmaric", "Marko Marić", models.Duty{Title: "Preglednik", Role: models.RoleViewer, ScopeType: models.ScopeArea, AreaID: &bp})

	orgRepo := repository.NewOrgRepository(baza, rec)
	vodRepo := repository.NewVodocuvarRepository(baza, rec)
	vod := service.NewVodocuvarService(vodRepo, users, "cop-primjer")
	vod.SetOrg(orgRepo)
	sad := time.Now()
	n := sad.In(models.Zagreb)
	list := &models.VodocuvarskiList{UserID: vodocuvar.ID.String(), Ime: vodocuvar.FullName, Sektor: p, AreaID: bp,
		Datum: time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb), Broj: 1, PredanoAt: &sad}
	if err := vodRepo.Save(ctx, list); err != nil {
		t.Fatal(err)
	}

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewVodocuvarHandler(func() *service.VodocuvarService { return vod }, users, func() *repository.OrgRepository { return orgRepo }, tmpl("vodocuvar.html"), tmpl("vodocuvar_list.html"))
	h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{} })
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vodocuvar/zadatak", h.HandleZadatak)
	mux.HandleFunc("POST /vodocuvar/upis", h.HandleUpis)
	mux.HandleFunc("POST /vodocuvar/{id}/radnja", h.HandleRadnja)
	// kao vraća osobu s dužnostima iz baze, kako je vidi prijava
	kao := func(u *models.User) (*models.User, *models.UserPermissions) {
		cijeli, err := users.GetUserByID(u.ID)
		if err != nil || cijeli == nil || len(cijeli.Duties) == 0 {
			t.Fatalf("%s bez dužnosti: %v", u.FullName, err)
		}
		return cijeli, models.NewUserPermissions(*cijeli)
	}
	posalji := func(u *models.User, putanja string, forma url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader(forma.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		cijeli, perms := kao(u)
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	danas := n.Format("2006-01-02")
	parafa := url.Values{"radnja": {"parafiraj"}}
	upis := url.Values{"vodocuvar": {vodocuvar.ID.String()}, "datum": {danas}, "tekst": {"Obilazak nasipa Primjerica"}}
	zadatak := url.Values{"vodocuvar": {vodocuvar.ID.String()}, "tekst": {"Deponija pijeska Primjerica"}}

	for _, u := range []*models.User{gost, preglednik} {
		if w := posalji(u, "/vodocuvar/"+list.ID+"/radnja", parafa); w.Code != http.StatusForbidden {
			t.Errorf("%s parafira: %d %s", u.FullName, w.Code, w.Header().Get("Location"))
		}
		if w := posalji(u, "/vodocuvar/upis", upis); w.Code != http.StatusForbidden {
			t.Errorf("%s upisuje: %d %s", u.FullName, w.Code, w.Header().Get("Location"))
		}
		if w := posalji(u, "/vodocuvar/zadatak", zadatak); w.Code != http.StatusForbidden {
			t.Errorf("%s zadaje zadatak: %d %s", u.FullName, w.Code, w.Header().Get("Location"))
		}
		// servis ih odbija i bez rute
		cijeli, perms := kao(u)
		if _, err := vod.Parafiraj(ctx, perms, cijeli, list.ID); err == nil {
			t.Errorf("%s parafira kroz servis", u.FullName)
		}
		if _, err := vod.Upisi(ctx, perms, cijeli, vodocuvar.ID.String(), n, "kroz servis"); err == nil {
			t.Errorf("%s upisuje kroz servis", u.FullName)
		}
		if _, err := vod.ZadajZadatak(ctx, perms, cijeli, vodocuvar.ID.String(), "kroz servis", time.Time{}); err == nil {
			t.Errorf("%s zadaje zadatak kroz servis", u.FullName)
		}
		if len(vod.Vodocuvari(ctx, perms)) != 0 {
			t.Errorf("%s dobiva popis vodočuvara za zadatke", u.FullName)
		}
	}
	l, _ := vodRepo.Get(ctx, list.ID)
	if len(l.Parafe) != 0 || len(l.Upisi) != 0 || len(vod.Zadaci(ctx, vodocuvar.ID.String())) != 0 {
		t.Fatalf("list nakon pokušaja gosta i preglednika: parafe %d, upisi %d", len(l.Parafe), len(l.Upisi))
	}

	// rukovoditelj područja parafira, upisuje i zadaje
	if l := mustUnescape(posalji(rukovoditelj, "/vodocuvar/"+list.ID+"/radnja", parafa).Header().Get("Location")); !strings.Contains(l, "parafiran") {
		t.Errorf("parafa rukovoditelja: %s", l)
	}
	if l := mustUnescape(posalji(rukovoditelj, "/vodocuvar/upis", upis).Header().Get("Location")); !strings.Contains(l, "success") {
		t.Errorf("upis rukovoditelja: %s", l)
	}
	if l := mustUnescape(posalji(rukovoditelj, "/vodocuvar/zadatak", zadatak).Header().Get("Location")); !strings.Contains(l, "success") {
		t.Errorf("zadatak rukovoditelja: %s", l)
	}
	if _, perms := kao(rukovoditelj); len(vod.Vodocuvari(ctx, perms)) != 1 {
		t.Error("rukovoditelj ne dobiva vodočuvara za zadatke")
	}
	l, _ = vodRepo.Get(ctx, list.ID)
	if len(l.Parafe) != 1 || len(l.Upisi) != 1 || len(vod.Zadaci(ctx, vodocuvar.ID.String())) != 1 {
		t.Fatalf("list nakon rukovoditelja: parafe %d, upisi %d", len(l.Parafe), len(l.Upisi))
	}
}
