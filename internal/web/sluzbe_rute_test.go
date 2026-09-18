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

// Stranica županije ima službe: dodavanje, izmjena i brisanje, a grad ih
// pokazuje uz svoje naselje
func TestSluzbeUzZupanijuKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "sl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO counties (id, code, name, seat) VALUES (14, 'OB', 'Osječko-baranjska županija', 'Osijek')`,
		`INSERT INTO municipalities (id, county_id, name, type) VALUES (302, 14, 'Beli Manastir', 'GRAD')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	ter := service.NewTerritoryService(repository.NewTerritoryRepository(baza, rec), sections)
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
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
	h.SetCountyTemplate(tmpl("county_detail.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /territories", h.ShowTerritories)
	mux.HandleFunc("GET /territories/counties/{id}", h.ShowCounty)
	mux.HandleFunc("POST /territories/counties/{id}/sluzbe", h.HandleSaveSluzba)
	mux.HandleFunc("POST /territories/counties/{id}/sluzbe/{sluzba}/obrisi", h.HandleDeleteSluzba)
	mux.HandleFunc("GET /territories/municipalities/{id}", h.ShowMunicipality)
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
	w := zovi(http.MethodPost, "/territories/counties/14/sluzbe", url.Values{"vrsta": {models.SluzbaLuckaKapetanija}, "naziv": {"Lučka kapetanija Osijek"}, "email": {"kapetanija-osijek@mmpi.hr"}})
	if !strings.Contains(w.Header().Get("Location"), "success") {
		t.Fatalf("dodavanje: %s", w.Header().Get("Location"))
	}
	zovi(http.MethodPost, "/territories/counties/14/sluzbe", url.Values{"vrsta": {models.SluzbaPolicijskaPost}, "naziv": {"PP Beli Manastir"}, "municipality_id": {"302"}})
	w = zovi(http.MethodGet, "/territories/counties/14", nil)
	for _, zeli := range []string{"Lučka kapetanija Osijek", "kapetanija-osijek@mmpi.hr", "PP Beli Manastir", "Beli Manastir", "Nova služba", "Županijski centar 112"} {
		if !strings.Contains(w.Body.String(), zeli) {
			t.Errorf("stranica županije nema %q", zeli)
		}
	}
	if w := zovi(http.MethodGet, "/territories", nil); !strings.Contains(w.Body.String(), "Službe (2)") {
		t.Error("kartica županije ne pokazuje broj službi")
	}
	if w := zovi(http.MethodGet, "/territories/municipalities/302", nil); !strings.Contains(w.Body.String(), "PP Beli Manastir") || !strings.Contains(w.Body.String(), "Lučka kapetanija Osijek") {
		t.Error("grad ne pokazuje službe")
	}
	sve, _ := ter.Sluzbe(context.Background(), 14)
	if len(sve) != 2 {
		t.Fatalf("službi %d", len(sve))
	}
	zovi(http.MethodPost, "/territories/counties/14/sluzbe/"+sve[0].ID+"/obrisi", url.Values{})
	if sve, _ = ter.Sluzbe(context.Background(), 14); len(sve) != 1 {
		t.Errorf("brisanje: ostalo %d", len(sve))
	}
}
