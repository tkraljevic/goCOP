package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// Dnevno izvješće rukovoditelja dionice kroz rute: popis, prazan obrazac s
// vodotokom iz dionice, spremanje nacrta, dokument, predaja, pa isti dan
// vodi na uređivanje umjesto na novi obrazac.
func TestDnevnaIzvjescaKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "izv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (16, 'B', 'Baranja', 'VGI Baranja', 'Osijek')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.16.3', 16, 'B', 'Dunav, lijeva obala', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	svc := service.NewIzvjescaService(repository.NewIzvjescaRepository(baza, rec), repository.NewSectionRepository(baza, rec),
		repository.NewStationRepository(baza, rec), repository.NewReadingRepository(baza, rec), repository.NewEpisodeRepository(baza, rec),
		repository.NewJournalRepository(baza, rec))

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewIzvjescaHandler(func() *service.IzvjescaService { return svc }, tmpl("izvjesca.html"), tmpl("izvjesce_form.html"), tmpl("izvjesce.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /izvjesca", h.ShowPopis)
	mux.HandleFunc("GET /izvjesca/novo", h.ShowNovo)
	mux.HandleFunc("POST /izvjesca", h.HandleSpremi)
	mux.HandleFunc("GET /izvjesca/{id}", h.ShowIzvjesce)
	mux.HandleFunc("GET /izvjesca/{id}/uredi", h.ShowUredi)
	mux.HandleFunc("POST /izvjesca/{id}", h.HandleSpremi)
	mux.HandleFunc("POST /izvjesca/{id}/predaj", h.HandlePredaj)
	mux.HandleFunc("POST /izvjesca/{id}/obrisi", h.HandleObrisi)

	rukovoditelj := &models.User{ID: uuid.New(), FullName: "Rukovoditelj Dionice"}
	prava := &models.UserPermissions{AllowedSections: map[string]bool{"B.16.3": true}}
	zovi := func(metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, rukovoditelj)
		ctx = context.WithValue(ctx, contextKeyPerms, prava)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, kod int, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != kod {
			t.Fatalf("%s: %d, a mora biti %d\n%s", sto, w.Code, kod, w.Body.String())
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

	mora(zovi(http.MethodGet, "/izvjesca", nil), http.StatusOK, "popis", "Nema izvješća", `href="/izvjesca/novo"`)
	mora(zovi(http.MethodGet, "/izvjesca/novo", nil), http.StatusOK, "obrazac",
		`name="dan"`, `<option value="B.16.3" selected>`, "Stadij obrane", "nagli porast", "Spremi i predaj")

	danas := time.Now().In(models.Zagreb).Format("2006-01-02")
	obrazac := url.Values{
		"dan": {danas}, "dionica": {"B.16.3"}, "stadij": {"REDOVNA"}, "vodotok": {"Dunav"}, "tendencija": {"PORAST"},
		"vodostaj_station": {"", ""}, "vodostaj_postaja": {"Dunav – Batina", ""}, "vodostaj_sat": {"07:00", ""},
		"vodostaj_vrijednost": {"551", ""}, "vodostaj_jedinica": {"cm", "cm"},
		"pregled": {"Nasip pregledan, bez oštećenja."}, "radnje": {"Nadvišenje nasipa 120 m."}, "vrece": {"1500"},
		"pravne_ljudi": {"12"}, "pravne_kamioni": {"2"}, "ostali_vatrogasci": {"8"},
		"popl_naselja": {"Batina"}, "popl_ljudi": {"0"}, "popl_sumske": {"12,5"},
	}
	w := zovi(http.MethodPost, "/izvjesca", obrazac)
	mora(w, http.StatusSeeOther, "spremanje")
	kamo := w.Header().Get("Location")
	if !strings.HasPrefix(kamo, "/izvjesca/") || strings.Contains(kamo, "error=") {
		t.Fatalf("spremanje vodi na %q", kamo)
	}
	id := strings.SplitN(strings.TrimPrefix(kamo, "/izvjesca/"), "?", 2)[0]
	mora(zovi(http.MethodGet, "/izvjesca/"+id, nil), http.StatusOK, "dokument",
		"Dunav – Batina", "551", "porast", "Nasip pregledan", "Batina", "12,5", "Rukovoditelj Dionice", "nacrt", `action="/izvjesca/`+id+`/predaj"`)

	// Isti dan i dionica: novi obrazac vodi na uređivanje postojećeg.
	w = zovi(http.MethodGet, "/izvjesca/novo?dionica=B.16.3&dan="+danas, nil)
	mora(w, http.StatusSeeOther, "ponovno novo")
	if w.Header().Get("Location") != "/izvjesca/"+id+"/uredi" {
		t.Errorf("ponovno novo vodi na %q", w.Header().Get("Location"))
	}
	if p := os.Getenv("GOCOP_DUMP"); p != "" {
		_ = os.WriteFile(p, zovi(http.MethodGet, "/izvjesca/"+id+"/uredi", nil).Body.Bytes(), 0o644)
	}
	mora(zovi(http.MethodGet, "/izvjesca/"+id+"/uredi", nil), http.StatusOK, "uređivanje", `value="Dunav"`, `value="1500"`, `value="12"`, `checked> porast`)

	mora(zovi(http.MethodPost, "/izvjesca/"+id+"/predaj", url.Values{}), http.StatusSeeOther, "predaja")
	mora(zovi(http.MethodGet, "/izvjesca/"+id, nil), http.StatusOK, "predano", "predano u podcentar")
	mora(zovi(http.MethodGet, "/izvjesca?dionica=B.16.3", nil), http.StatusOK, "popis poslije", "B.16.3", "Dunav", ">R<", "predano")

	// Predano rukovoditelj ne briše.
	w = zovi(http.MethodPost, "/izvjesca/"+id+"/obrisi", url.Values{})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("brisanje predanog: %d %s", w.Code, w.Header().Get("Location"))
	}
}
