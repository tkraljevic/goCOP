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

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Profil djelatnika ima tablicu zaduženja s gumbima Uredi i Opozovi;
// izmjena vrijedi odmah, a opozvano zaduženje ostaje u povijesti profila.
func TestZaduzenjaNaProfiluKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zad.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewUsersHandler(users, tmpl("users.html"))
	h.SetPageTemplates(tmpl("user_detail.html"), tmpl("user_form.html"), tmpl("duty_form.html"), tmpl("profile.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", h.ShowUser)
	mux.HandleFunc("GET /users/duties/{duty}/edit", h.ShowDutyEditForm)
	mux.HandleFunc("POST /users/duties/{duty}/update", h.HandleUpdateDuty)
	mux.HandleFunc("POST /users/duty/revoke", h.HandleRevokeDuty)

	// uprava organizacije uređuje; osoba s jednim zaduženjem na dionicama
	admin := &models.User{ID: uuid.New(), Username: "uprava", FullName: "Uprava Organizacije", IsGlobalAdmin: true, IsActive: true}
	if err := userRepo.CreateUser(admin, nil); err != nil {
		t.Fatal(err)
	}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *admin}
	bp := 34
	sektor := "B"
	osoba := &models.User{ID: uuid.New(), Username: "mmaric", FullName: "Mara Marić", IsActive: true}
	if err := userRepo.CreateUser(osoba, &models.Duty{Title: "Rukovoditelj dionica A.34.1", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &bp, SectionCodes: "A.34.1", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	zovi := func(metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if forma != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, admin)
		ctx = context.WithValue(ctx, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d\n%s", sto, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "template:") {
			t.Errorf("%s: predložak puknuo: %s", sto, w.Body.String()[strings.Index(w.Body.String(), "template:"):])
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}
	nema := func(w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
		t.Helper()
		for _, d := range dijelovi {
			if strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: ne bi smjelo biti %q", sto, d)
			}
		}
	}

	profil := "/users/" + osoba.ID.String()
	u, _ := userRepo.GetUserByID(osoba.ID)
	if len(u.Duties) != 1 {
		t.Fatalf("očekivano 1 zaduženje, ima %d", len(u.Duties))
	}
	zad := u.Duties[0]
	uredi := "/users/duties/" + zad.ID.String() + "/edit"

	// tablica s gumbima
	mora(zovi(http.MethodGet, profil, nil), "profil", "Rukovoditelj dionica A.34.1", "Dodaj zaduženje", uredi, "Opozovi", "A.34.1", "primarna")
	nema(zovi(http.MethodGet, profil, nil), "profil bez povijesti", "Prijašnja zaduženja")

	// obrazac za izmjenu je predispunjen
	mora(zovi(http.MethodGet, uredi, nil), "obrazac", `value="Rukovoditelj dionica A.34.1"`, `value="A.34.1"`, `value="34" data-sector="B" selected`, "Spremi izmjene", `action="/users/duties/`+zad.ID.String()+`/update"`)

	// izmjena: više dionica, novi naziv
	w := zovi(http.MethodPost, "/users/duties/"+zad.ID.String()+"/update", url.Values{"user_id": {osoba.ID.String()}, "title": {"Rukovoditelj dionica A.34.1 – A.34.3"}, "role": {string(models.RoleSectionLeader)}, "sector_id": {"B"}, "area_id": {"34"}, "section_codes": {"A.34.1, A.34.2, A.34.3"}, "is_primary": {"1"}})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), profil) {
		t.Fatalf("izmjena: %d %s\n%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	u, _ = userRepo.GetUserByID(osoba.ID)
	if len(u.Duties) != 1 || u.Duties[0].ID != zad.ID || u.Duties[0].SectionCodes != "A.34.1, A.34.2, A.34.3" || u.Duties[0].Title != "Rukovoditelj dionica A.34.1 – A.34.3" {
		t.Fatalf("izmjena nije spremljena u mjestu: %+v", u.Duties)
	}
	mora(zovi(http.MethodGet, profil, nil), "profil nakon izmjene", "A.34.1, A.34.2, A.34.3")

	// opoziv: nestaje iz tablice, ostaje u povijesti
	w = zovi(http.MethodPost, "/users/duty/revoke", url.Values{"duty_id": {zad.ID.String()}, "user_id": {osoba.ID.String()}})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "success") {
		t.Fatalf("opoziv: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = zovi(http.MethodGet, profil, nil)
	mora(w, "profil nakon opoziva", "Nema aktivnih zaduženja", "Prijašnja zaduženja (1)", "opozvano ", "A.34.1, A.34.2, A.34.3")
	nema(w, "profil nakon opoziva", uredi)

	// opozvano se više ne uređuje
	if w := zovi(http.MethodGet, uredi, nil); w.Code != http.StatusNotFound {
		t.Errorf("uređivanje opozvanog: očekivano 404, dobiveno %d", w.Code)
	}
}
