package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
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

// Upisanom naselju admin može promijeniti broj stanovnika: polje je vidljivo
// u retku za izmjenu, a spremanje upisuje novi broj
func TestNaseljuSeMijenjaBrojStanovnika(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "ns.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	for _, q := range []string{
		`INSERT INTO counties (id, code, name, seat) VALUES (14, 'OB', 'Osječko-baranjska županija', 'Osijek')`,
		`INSERT INTO municipalities (id, county_id, name, type) VALUES (302, 14, 'Beli Manastir', 'GRAD')`,
		`INSERT INTO settlements (id, municipality_id, county_id, name, postal_code, population) VALUES (4801, 302, 14, 'Branjin Vrh', '31300', 0)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	ter := service.NewTerritoryService(repository.NewTerritoryRepository(baza, rec), sections)
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewTerritoriesHandler(ter, tmpl("territories.html"))
	h.SetPageTemplates(tmpl("county_form.html"), tmpl("municipality_form.html"), tmpl("municipality_detail.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /territories/municipalities/{id}", h.ShowMunicipality)
	mux.HandleFunc("POST /api/settlements/update", h.HandleUpdateSettlementAPI)
	u := &models.User{ID: uuid.New(), FullName: "Uprava"}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *u}
	zovi := func(metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		var r *http.Request
		if forma != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		c := context.WithValue(r.Context(), contextKeyUser, u)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	obrazacIzmjene := regexp.MustCompile(`(?s)<form method="POST" action="/api/settlements/update".*?</form>`)
	poljeStanovnika := func() string {
		t.Helper()
		obrazac := obrazacIzmjene.FindString(zovi(http.MethodGet, "/territories/municipalities/302", nil).Body.String())
		if obrazac == "" {
			t.Fatal("stranica grada nema obrazac za izmjenu naselja")
		}
		polje := regexp.MustCompile(`<input[^>]*name="population"[^>]*>`).FindString(obrazac)
		if polje == "" || strings.Contains(polje, `type="hidden"`) || !strings.Contains(polje, `aria-label="Broj stanovnika"`) {
			t.Fatalf("broj stanovnika nije vidljivo polje: %q", polje)
		}
		return polje
	}
	if polje := poljeStanovnika(); !strings.Contains(polje, `value=""`) {
		t.Errorf("polje bez upisanog broja nije prazno: %q", polje)
	}

	w := zovi(http.MethodPost, "/api/settlements/update", url.Values{"id": {"4801"}, "municipality_id": {"302"}, "name": {"Branjin Vrh"}, "postal_code": {"31300"}, "population": {"812"}})
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/territories/municipalities/302") || !strings.Contains(loc, "success") {
		t.Fatalf("spremanje: %d %s", w.Code, loc)
	}
	var stanovnika int
	if err := baza.QueryRow(`SELECT population FROM settlements WHERE id = 4801`).Scan(&stanovnika); err != nil || stanovnika != 812 {
		t.Fatalf("u bazi %d stanovnika (%v), treba 812", stanovnika, err)
	}
	if polje := poljeStanovnika(); !strings.Contains(polje, `value="812"`) {
		t.Errorf("polje ne pokazuje novi broj: %q", polje)
	}
}
