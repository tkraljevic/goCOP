package web

import (
	"bytes"
	"context"
	"html/template"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// Odluke (30. 10. 2026.), 9, kroz rute: ugovor s vodama kojih nema u
// registru upisuje uprava branjenog područja. Tko nije uprava, u izvješću
// odmah vidi upozorenje, a kad ipak klikne Upiši, ništa se ne upiše,
// izvješće se pokaže ponovno i učitana datoteka čeka da vode veže. Uprava
// isti ugovor upiše. Izmišljeni sektor P.
func TestUvozUgovoraSNovimVodamaKrozRute(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "odrzavanje.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	waters, structures := repository.NewWatercourseRepository(baza, rec), repository.NewStructureRepository(baza, rec)
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("odrzavanje.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewMaintenanceHandler(service.NewMaintenanceService(repository.NewMaintenanceRepository(baza, rec), waters, structures),
		service.NewUserService(userRepo, nil, nil), service.NewWatercourseService(waters), service.NewStructureService(structures), tmpl)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /odrzavanje/uvoz", h.HandleImportUpload)
	mux.HandleFunc("POST /odrzavanje/uvoz/upisi", h.HandleImportWrite)
	ugovor, err := os.ReadFile(filepath.Join("..", "..", "docs", "predlosci", "odrzavanje", "ugovor-a02.xlsx"))
	if err != nil {
		t.Fatal(err)
	}

	zovi := func(p *models.UserPermissions, r *http.Request) *httptest.ResponseRecorder {
		t.Helper()
		ctx := context.WithValue(context.WithValue(r.Context(), contextKeyUser, &models.User{ID: uuid.New(), FullName: "Pero Perić"}), contextKeyPerms, p)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	ucitaj := func(p *models.UserPermissions) (string, string) {
		t.Helper()
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		if err := mw.WriteField("area", "1"); err != nil {
			t.Fatal(err)
		}
		fw, err := mw.CreateFormFile("file", "ugovor-a02.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(ugovor); err != nil || mw.Close() != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/odrzavanje/uvoz", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := zovi(p, r)
		m := regexp.MustCompile(`name="token" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
		if w.Code != http.StatusOK || m == nil {
			t.Fatalf("izvješće uvoza: %d", w.Code)
		}
		return m[1], w.Body.String()
	}
	upisi := func(p *models.UserPermissions, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/odrzavanje/uvoz/upisi", strings.NewReader(url.Values{"area": {"1"}, "token": {token}, "filename": {"ugovor-a02.xlsx"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return zovi(p, r)
	}
	upozorenje := "vode iz ugovora nisu u registru vodnih tijela"

	nijeUprava := &models.UserPermissions{AllowedAreas: map[int]bool{1: true}}
	token, izvjesce := ucitaj(nijeUprava)
	if !strings.Contains(izvjesce, upozorenje) {
		t.Error("izvješće ne kaže unaprijed da ugovor s novim vodama upisuje uprava")
	}
	w := upisi(nijeUprava, token)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "nove vode dodaje uprava branjenog područja") || !strings.Contains(w.Body.String(), `value="`+token+`"`) {
		t.Errorf("odbijen upis: %d, izvješće s porukom se ne vidi", w.Code)
	}
	if _, err := os.Stat(filepath.Join(importDir(), token+".xlsx")); err != nil {
		t.Errorf("odbijen upis obrisao je učitanu datoteku: %v", err)
	}
	var n int
	if err := baza.QueryRow(`SELECT count(*) FROM maintained_waters`).Scan(&n); err != nil || n != 0 {
		t.Errorf("odbijen upis upisao je %d lokacija (%v)", n, err)
	}

	uprava := pravaDuznosti(models.RoleAreaLeader, models.ScopeArea, "P", 1, "")
	token, izvjesce = ucitaj(uprava)
	if strings.Contains(izvjesce, upozorenje) {
		t.Error("upravi izvješće javlja da ugovor upisuje uprava")
	}
	if l := upisi(uprava, token).Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Errorf("uprava ne upisuje ugovor: %q", l)
	}
	if _, err := os.Stat(filepath.Join(importDir(), token+".xlsx")); !os.IsNotExist(err) {
		t.Errorf("upisana datoteka nije maknuta: %v", err)
	}

	// kad se popis lokacija ne da pročitati, izvješće se ne prikazuje
	if _, err := baza.Exec(`DROP TABLE maintained_waters`); err != nil {
		t.Fatal(err)
	}
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	if err := mw.WriteField("area", "1"); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "ugovor-a02.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(ugovor); err != nil || mw.Close() != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/odrzavanje/uvoz", &tijelo)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if w := zovi(nijeUprava, r); w.Code != http.StatusInternalServerError && w.Code != http.StatusSeeOther {
		t.Errorf("izvješće bez popisa lokacija: %d", w.Code)
	}
}
