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

// Odluke (30. 10. 2026.), 3, kroz rute: dnevno izvješće dionice otvara za
// pisanje, sprema i predaje samo rukovoditelj dionice ili zamjenik. Uprava
// područja i globalni administrator izvješće čitaju i brišu, ali obrazac
// dobivaju s 403, a spremanje i predaju s porukom. Izmišljeni sektor P.
func TestDnevnoIzvjescePiseRukovoditeljKrozRute(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "izv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	korisnici := repository.NewUserRepository(baza, rec)
	svc := service.NewIzvjescaService(repository.NewIzvjescaRepository(baza, rec), repository.NewSectionRepository(baza, rec),
		repository.NewStationRepository(baza, rec), repository.NewReadingRepository(baza, rec), repository.NewEpisodeRepository(baza, rec),
		repository.NewJournalRepository(baza, rec), korisnici)
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
	h := NewIzvjescaHandler(func() *service.IzvjescaService { return svc }, tmpl("izvjesca.html"), tmpl("izvjesce_form.html"), tmpl("izvjesce.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /izvjesca/novo", h.ShowNovo)
	mux.HandleFunc("POST /izvjesca", h.HandleSpremi)
	mux.HandleFunc("GET /izvjesca/{id}", h.ShowIzvjesce)
	mux.HandleFunc("GET /izvjesca/{id}/uredi", h.ShowUredi)
	mux.HandleFunc("POST /izvjesca/{id}/predaj", h.HandlePredaj)
	mux.HandleFunc("POST /izvjesca/{id}/obrisi", h.HandleObrisi)

	// Pero je rukovoditelj dionice P.1.1 i u bazi: o njegovoj dužnosti ovisi
	// tko mijenja izvješće koje je predao (Odluke (30. 10. 2026.), 10)
	pero := &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", IsActive: true}
	sektorP, podrucje1 := "P", 1
	perina := &models.Duty{Title: "Rukovoditelj dionice", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection,
		SectorID: &sektorP, AreaID: &podrucje1, SectionCodes: "P.1.1"}
	if err := korisnici.CreateUser(pero, perina); err != nil {
		t.Fatal(err)
	}
	osoba := pero
	zovi := func(p *models.UserPermissions, metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(context.WithValue(r.Context(), contextKeyUser, osoba), contextKeyPerms, p)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	greska := func(w *httptest.ResponseRecorder, sto string) {
		t.Helper()
		if l, err := url.QueryUnescape(w.Header().Get("Location")); err != nil || w.Code != http.StatusSeeOther || !strings.Contains(l, "error=dnevno izvješće dionice") {
			t.Errorf("%s: %d %q", sto, w.Code, l)
		}
	}

	rukovoditelj := pravaDuznosti(models.RoleSectionLeader, models.ScopeSection, "P", 1, "P.1.1")
	zamjenik := pravaDuznosti(models.RoleSectionDeputy, models.ScopeSection, "P", 1, "P.1.1")
	upravaPodrucja := pravaDuznosti(models.RoleAreaLeader, models.ScopeArea, "P", 1, "")
	admin := models.NewUserPermissions(models.User{ID: uuid.New(), IsGlobalAdmin: true})
	danas := time.Now().In(models.Zagreb).Format("2006-01-02")
	obrazac := url.Values{"dan": {danas}, "dionica": {"P.1.1"}, "stadij": {"REDOVNA"}, "pregled": {"Nasip pregledan."}}

	for _, s := range []struct {
		ime string
		p   *models.UserPermissions
	}{{"uprava područja", upravaPodrucja}, {"globalni administrator", admin}} {
		if w := zovi(s.p, http.MethodGet, "/izvjesca/novo?dionica=P.1.1", nil); w.Code != http.StatusForbidden {
			t.Errorf("%s otvara obrazac: %d", s.ime, w.Code)
		}
		greska(zovi(s.p, http.MethodPost, "/izvjesca", obrazac), s.ime+" sprema")
	}
	if w := zovi(zamjenik, http.MethodGet, "/izvjesca/novo?dionica=P.1.1", nil); w.Code != http.StatusOK {
		t.Fatalf("zamjenik otvara obrazac: %d", w.Code)
	}
	w := zovi(rukovoditelj, http.MethodPost, "/izvjesca", obrazac)
	id := strings.SplitN(strings.TrimPrefix(w.Header().Get("Location"), "/izvjesca/"), "?", 2)[0]
	if iz, err := svc.Get(context.Background(), id); err != nil || iz == nil {
		t.Fatalf("rukovoditelj nije spremio izvješće: %q", w.Header().Get("Location"))
	}

	// uprava čita izvješće i vidi brisanje, ali ne obrazac ni predaju
	w = zovi(upravaPodrucja, http.MethodGet, "/izvjesca/"+id, nil)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `action="/izvjesca/`+id+`/predaj"`) || !strings.Contains(w.Body.String(), `action="/izvjesca/`+id+`/obrisi"`) {
		t.Errorf("uprava na izvješću: %d", w.Code)
	}
	if w := zovi(upravaPodrucja, http.MethodGet, "/izvjesca/"+id+"/uredi", nil); w.Code != http.StatusForbidden {
		t.Errorf("uprava otvara obrazac postojećeg: %d", w.Code)
	}
	greska(zovi(upravaPodrucja, http.MethodPost, "/izvjesca/"+id+"/predaj", url.Values{}), "uprava predaje")
	if iz, err := svc.Get(context.Background(), id); err != nil || iz.Predano() {
		t.Fatal("uprava je predala izvješće")
	}
	// zamjenik predaje; uprava briše
	if w := zovi(zamjenik, http.MethodPost, "/izvjesca/"+id+"/predaj", url.Values{}); !strings.Contains(w.Header().Get("Location"), "success=") {
		t.Errorf("zamjenik predaje: %q", w.Header().Get("Location"))
	}
	// predano izvješće mijenja onaj tko ga je izradio: drugi zamjenik
	// ne vidi gumb Obrazac i obrazac dobiva s 403 (Odluke (8. 10. 2026.),
	// pitanje 12); Pero ga i dalje otvara
	urediti := `href="/izvjesca/` + id + `/uredi"`
	if w := zovi(zamjenik, http.MethodGet, "/izvjesca/"+id, nil); !strings.Contains(w.Body.String(), urediti) {
		t.Error("autor ne vidi gumb Obrazac na predanom izvješću")
	}
	if w := zovi(zamjenik, http.MethodGet, "/izvjesca/"+id+"/uredi", nil); w.Code != http.StatusOK {
		t.Errorf("autor otvara obrazac predanog: %d", w.Code)
	}
	osoba = &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	if w := zovi(zamjenik, http.MethodGet, "/izvjesca/"+id, nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), urediti) {
		t.Errorf("drugi zamjenik na predanom izvješću: %d, gumb Obrazac %v", w.Code, strings.Contains(w.Body.String(), urediti))
	}
	if w := zovi(zamjenik, http.MethodGet, "/izvjesca/"+id+"/uredi", nil); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "tek kad on više ne vodi dionicu") {
		t.Errorf("drugi zamjenik otvara obrazac predanog: %d %s", w.Code, w.Body.String())
	}
	// kad Pero više ne vodi dionicu, drugi zamjenik vidi gumb i otvara
	// obrazac (Odluke (30. 10. 2026.), 10)
	if err := korisnici.RevokeDuty(perina.ID); err != nil {
		t.Fatal(err)
	}
	if w := zovi(zamjenik, http.MethodGet, "/izvjesca/"+id, nil); !strings.Contains(w.Body.String(), urediti) {
		t.Error("drugi zamjenik ne vidi gumb Obrazac kad autor više ne vodi dionicu")
	}
	if w := zovi(zamjenik, http.MethodGet, "/izvjesca/"+id+"/uredi", nil); w.Code != http.StatusOK {
		t.Errorf("drugi zamjenik otvara obrazac kad autor više ne vodi dionicu: %d", w.Code)
	}
	osoba = pero
	if w := zovi(upravaPodrucja, http.MethodPost, "/izvjesca/"+id+"/obrisi", url.Values{}); !strings.Contains(w.Header().Get("Location"), "success=") {
		t.Errorf("uprava briše: %q", w.Header().Get("Location"))
	}
}
