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
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Olovka i gumb za brisanje očitanja (Odluke (8. 10. 2026.), pitanje 9):
// olovka stoji samo uz očitanja koja osoba smije ispraviti, a „Obriši
// očitanje” vidi samo onaj tko odgovara za letvu. Izmišljeni sektor P,
// letva Primjerovo na dionici P.1.1; Ana Anić je Dežurna operaterka u
// COP-u, Pero Perić rukovoditelj sektora P.
func TestOlovkaIBrisanjeOcitanjaPoPravu(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "prava.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, nil, service.NewSSEBroker())
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	readings := service.NewReadingService(readingRepo, stationRepo, repository.NewStructureRepository(baza, rec), sections, users)
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewReadingsHandler(readings, service.NewStationService(stationRepo, sections, service.NewSSEBroker()),
		service.NewStructureService(repository.NewStructureRepository(baza, rec)), users,
		tmpl("readings.html"), tmpl("reading_history.html"), tmpl("reading_form.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readings/station/{id}", h.ShowHistory)
	mux.HandleFunc("GET /readings/edit/{id}", h.ShowForm)

	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, 'P.1.1', ?, ?)`,
		uuid.NewString(), st.ID.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
	sektor := "P"
	osoba := func(ime string, uloga models.Role) (*models.User, *models.UserPermissions) {
		u := models.User{ID: uuid.New(), FullName: ime, IsActive: true,
			Duties: []models.Duty{{ID: uuid.New(), Role: uloga, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}}}
		return &u, models.NewUserPermissions(u)
	}
	ana, anaOvl := osoba("Ana Anić", models.RoleOperator)
	pero, peroOvl := osoba("Pero Perić", models.RoleSectorLeader)
	upisi := func(autor *models.User, cm int) models.Reading {
		v := cm
		rd := models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: time.Now().Add(-time.Duration(cm) * time.Minute),
			LevelCm: &v, Source: models.ReadingSourceManual, UserID: autor.ID.String()}
		if err := readingRepo.Create(ctx, &rd); err != nil {
			t.Fatal(err)
		}
		return rd
	}
	anino, perino := upisi(ana, 101), upisi(pero, 102)

	zovi := func(u *models.User, perms *models.UserPermissions, putanja string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, putanja, nil)
		c := context.WithValue(r.Context(), contextKeyUser, u)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	olovka := func(rd models.Reading) string { return `href="/readings/edit/` + rd.ID.String() + `"` }

	// operaterka upisuje na letvu i ispravlja svoje očitanje, ne tuđe
	w := zovi(ana, anaOvl, "/readings/station/"+st.ID.String())
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), olovka(anino)) || strings.Contains(w.Body.String(), olovka(perino)) {
		t.Errorf("olovke za operaterku: %d", w.Code)
	}
	w = zovi(ana, anaOvl, "/readings/edit/"+anino.ID.String())
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Obriši očitanje") {
		t.Errorf("operaterka ispravlja svoje, ali ga ne briše: %d", w.Code)
	}
	if w = zovi(ana, anaOvl, "/readings/edit/"+perino.ID.String()); w.Code != http.StatusForbidden {
		t.Errorf("tuđe očitanje operaterki: %d", w.Code)
	}

	// uprava sektora odgovara za letvu: ispravlja i briše svako
	w = zovi(pero, peroOvl, "/readings/station/"+st.ID.String())
	if !strings.Contains(w.Body.String(), olovka(anino)) || !strings.Contains(w.Body.String(), olovka(perino)) {
		t.Error("uprava sektora ima olovku uz svako očitanje")
	}
	if w = zovi(pero, peroOvl, "/readings/edit/"+anino.ID.String()); !strings.Contains(w.Body.String(), "Obriši očitanje") {
		t.Errorf("uprava sektora briše: %d", w.Code)
	}
}

// Na objektu olovka ide kao dosad: uz sva očitanja, kad osoba upisuje na objekt
func TestOlovkaNaObjektuKaoDosad(t *testing.T) {
	h := &ReadingsHandler{}
	ocitanja := []models.Reading{{ID: uuid.New()}, {ID: uuid.New()}}
	if m := h.ispravljiva(nil, nil, true, ocitanja); !m[ocitanja[0].ID] || !m[ocitanja[1].ID] {
		t.Error("tko upisuje na objekt, ispravlja")
	}
	if m := h.ispravljiva(nil, nil, false, ocitanja); m[ocitanja[0].ID] || len(m) != 2 {
		t.Error("tko ne upisuje na objekt, ne ispravlja")
	}
}
