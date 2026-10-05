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

// Privremeno imenovanje kroz obrazac: kvačica „ističe s obranom” stiže do
// dužnosti, „Vrijedi zaključno s 31. 12.” prestaje 1. 1. u 0 h po našem
// vremenu, profil kaže kad prestaje, a obrazac izmjene nudi zadani dan, ne
// raniji stvarni istek (kraj obrane)
func TestPrivremenoImenovanjeKrozObrazac(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "imenovanje.db"))
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
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES
			('B.34.2', 34, 'B', 'Dionica 2', datetime('now'), datetime('now'))`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	krajObrane := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	users.SetKrajObrane(func(models.Duty) *time.Time { return &krajObrane })

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
	mux.HandleFunc("GET /users/{id}/duties/new", h.ShowDutyForm)
	mux.HandleFunc("POST /users/duty/add", h.HandleAddDuty)
	mux.HandleFunc("GET /users/duties/{duty}/edit", h.ShowDutyEditForm)

	admin := &models.User{ID: uuid.New(), Username: "uprava", FullName: "Uprava Organizacije", IsGlobalAdmin: true, IsActive: true}
	if err := userRepo.CreateUser(admin, nil); err != nil {
		t.Fatal(err)
	}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *admin}
	osoba := &models.User{ID: uuid.New(), Username: "mmaric", FullName: "Mara Marić", IsActive: true}
	if err := userRepo.CreateUser(osoba, nil); err != nil {
		t.Fatal(err)
	}
	zovi := func(metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(metoda, putanja, nil)
		if forma != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		ctx := context.WithValue(context.WithValue(r.Context(), contextKeyUser, admin), contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d\n%s", sto, w.Code, w.Body.String())
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}

	// novi obrazac: privremeno imenovanje, kvačica obrane unaprijed uključena
	mora(zovi(http.MethodGet, "/users/"+osoba.ID.String()+"/duties/new", nil), "novi obrazac",
		"Privremena ispomoć (privremeno imenovanje)", `name="istece_s_obranom" value="1" checked`, "Vrijedi zaključno s (neobavezno)")

	w := zovi(http.MethodPost, "/users/duty/add", url.Values{"user_id": {osoba.ID.String()}, "role": {string(models.RoleSectionLeader)},
		"sector_id": {"B"}, "area_id": {"34"}, "section_codes": {"B.34.2"}, "is_temporary": {"1"}, "istece_s_obranom": {"1"},
		"reason": {"nedostaje osoblja"}, "expires_at": {"2026-12-31"}})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "success") {
		t.Fatalf("dodjela: %d %s", w.Code, w.Header().Get("Location"))
	}
	u, _ := userRepo.GetUserByID(osoba.ID)
	if len(u.Duties) != 1 {
		t.Fatalf("zaduženja: %+v", u.Duties)
	}
	d := u.Duties[0]
	prestanak := time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC) // 1. 1. 2027. u 0 h u Zagrebu
	if !d.IsTemporary || !d.IsticeSObranom || d.Rok == nil || !d.Rok.Equal(prestanak) || d.ExpiresAt == nil || !d.ExpiresAt.Equal(krajObrane) {
		t.Fatalf("imenovanje: privremena %v, s obranom %v, rok %v, istek %v", d.IsTemporary, d.IsticeSObranom, d.Rok, d.ExpiresAt)
	}

	// profil: prestaje s krajem obrane (raniji od datuma), u satu po našem vremenu
	mora(zovi(http.MethodGet, "/users/"+osoba.ID.String(), nil), "profil", "Privremeno · Rukovoditelj dionice", "prestaje 20. 10. 2026. u 10:00")

	// obrazac izmjene nudi zadani datum, a kvačica ostaje
	mora(zovi(http.MethodGet, "/users/duties/"+d.ID.String()+"/edit", nil), "obrazac izmjene",
		`value="2026-12-31"`, `name="istece_s_obranom" value="1" checked`, `name="is_temporary" value="1" checked`)
}
