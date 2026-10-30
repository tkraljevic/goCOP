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

// Obrasci djelatnika tuđim očima ne poništavaju lozinku, ne isključuju
// račun i ne dodjeljuju, mijenjaju ni opozivaju upravne dužnosti; svojim
// očima isti zahtjevi prolaze
func TestObrasciDjelatnikaTudjimOcima(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "oci.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
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
	mux := http.NewServeMux()
	mux.HandleFunc("POST /users/{id}/reset-password", h.HandleResetPassword)
	mux.HandleFunc("POST /users/update", h.HandleUpdateUser)
	mux.HandleFunc("POST /users/duty/add", h.HandleAddDuty)
	mux.HandleFunc("POST /users/duties/{duty}/update", h.HandleUpdateDuty)
	mux.HandleFunc("POST /users/duty/revoke", h.HandleRevokeDuty)

	admin := &models.User{ID: uuid.New(), Username: "uprava", FullName: "Uprava Organizacije", IsGlobalAdmin: true, IsActive: true}
	if err := userRepo.CreateUser(admin, nil); err != nil {
		t.Fatal(err)
	}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *admin}
	sektor := "B"
	osoba := &models.User{ID: uuid.New(), Username: "mmaric", FullName: "Mara Marić", PasswordHash: "sazetak", OrgType: models.OrgHrvatskeVode, IsActive: true}
	if err := userRepo.CreateUser(osoba, &models.Duty{Title: "Rukovoditelj sektora", Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &sektor, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	procitaj := func() *models.User {
		t.Helper()
		u, err := userRepo.GetUserByID(osoba.ID)
		if err != nil || u == nil || len(u.Duties) == 0 {
			t.Fatalf("račun s dužnošću: %+v %v", u, err)
		}
		return u
	}
	duznost := procitaj().Duties[0].ID.String()

	zovi := func(gleda bool, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader(forma.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(r.Context(), contextKeyUser, admin)
		ctx = context.WithValue(ctx, contextKeyPerms, perms)
		ctx = context.WithValue(ctx, contextKeyViewing, gleda)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	racun := url.Values{"id": {osoba.ID.String()}, "username": {"mmaric"}, "full_name": {"Mara Marić"}, "org_type": {string(models.OrgHrvatskeVode)}}
	zahtjevi := []struct {
		naziv, putanja string
		forma          url.Values
	}{
		{"poništenje lozinke", "/users/" + osoba.ID.String() + "/reset-password", url.Values{}},
		{"isključenje računa", "/users/update", racun},
		{"dužnost uprave sektora", "/users/duty/add", url.Values{"user_id": {osoba.ID.String()}, "role": {string(models.RoleSectorDeputy)}, "sector_id": {"B"}}},
		{"izmjena upravne dužnosti", "/users/duties/" + duznost + "/update", url.Values{"user_id": {osoba.ID.String()}, "role": {string(models.RoleSectorLeader)}, "sector_id": {"B"}, "title": {"Rukovoditelj za led"}, "is_primary": {"1"}}},
		{"opoziv upravne dužnosti", "/users/duty/revoke", url.Values{"duty_id": {duznost}, "user_id": {osoba.ID.String()}}},
	}
	for _, z := range zahtjevi {
		w := zovi(true, z.putanja, z.forma)
		if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "error=") {
			t.Errorf("tuđim očima, %s: %d %s", z.naziv, w.Code, w.Header().Get("Location"))
		}
	}
	if sad := procitaj(); !sad.IsActive || sad.MustChangePassword || len(sad.Duties) != 1 || sad.Duties[0].Title != "Rukovoditelj sektora" {
		t.Fatalf("račun se tuđim očima ipak promijenio: %+v", sad)
	}

	// svojim očima isti zahtjevi prolaze
	for _, z := range zahtjevi {
		w := zovi(false, z.putanja, z.forma)
		if strings.Contains(w.Header().Get("Location"), "error=") || (w.Code != http.StatusOK && w.Code != http.StatusSeeOther) {
			t.Errorf("svojim očima, %s: %d %s", z.naziv, w.Code, w.Header().Get("Location"))
		}
	}
}
