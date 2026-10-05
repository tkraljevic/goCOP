package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Obrazac nove dionice predlaže sljedeću šifru područja; kad se dionice ne
// daju pročitati, obrazac to kaže i ne predlaže prvi broj, jer takva dionica
// možda već postoji.
func TestObrazacDionicePredlazeSifruIliJavljaGresku(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "obrazac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('F', 'Sektor F', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (41, 'F', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	perms := &models.UserPermissions{IsGlobalAdmin: true}
	sec := &models.Section{Code: "F.41.1", AreaID: 41, Parts: []models.SectionPart{{Description: "rijeka Primjerica"}}}
	if err := sections.SaveSection(context.Background(), perms, sec, true); err != nil {
		t.Fatal(err)
	}

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("section_form.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSectionsHandler(sections, users, nil)
	h.SetPageTemplates(nil, tmpl, nil, nil)
	otvori := func() string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/sections/new?area=41", nil)
		c := context.WithValue(r.Context(), contextKeyUser, &models.User{Username: "pperic", FullName: "Pero Perić"})
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		h.ShowSectionForm(w, r.WithContext(c))
		if w.Code != http.StatusOK {
			t.Fatalf("obrazac: %d %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if html := otvori(); !strings.Contains(html, `value="F.41.2"`) || strings.Contains(html, "ne može se predložiti") {
		t.Error("obrazac ne predlaže F.41.2")
	}
	if _, err := baza.Exec(`ALTER TABLE sections RENAME TO nema_sections`); err != nil {
		t.Fatal(err)
	}
	html := otvori()
	if !strings.Contains(html, "Šifra sljedeće dionice ne može se predložiti") || strings.Contains(html, `value="F.41.1"`) {
		t.Error("obrazac ne javlja grešku čitanja ili predlaže prvi broj")
	}
}
