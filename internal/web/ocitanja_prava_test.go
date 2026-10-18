package web

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"io/fs"
	"mime/multipart"
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

// okolinaPrava je izmišljeni sektor P, letva Primjerovo na dionici P.1.1;
// Ana Anić je Dežurna operaterka u COP-u, Pero Perić rukovoditelj sektora P.
// Svako je upisao po jedno očitanje.
type okolinaPrava struct {
	mux             *http.ServeMux
	st              *models.Station
	ana, pero       *models.User
	anaOvl, peroOvl *models.UserPermissions
	anino, perino   models.Reading
	upisi           func(autor *models.User, cm int) models.Reading
	h               *ReadingsHandler
	ocitanja        *repository.ReadingRepository
	baza            *sql.DB
	stanice         *repository.StationRepository
}

func novaOkolinaPrava(t *testing.T) *okolinaPrava {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "prava.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
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
	h.SetOcitanjaCSV(tmpl("ocitanja_ispravci.html"))
	o := &okolinaPrava{mux: http.NewServeMux(), h: h, ocitanja: readingRepo, baza: baza, stanice: stationRepo}
	o.mux.HandleFunc("GET /readings/station/{id}", h.ShowHistory)
	o.mux.HandleFunc("GET /readings/edit/{id}", h.ShowForm)
	o.mux.HandleFunc("POST /readings/station/{id}/ocitanja/uvoz", h.HandleOcitanjaUvoz)

	ctx := context.Background()
	o.st = &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stationRepo.CreateStation(ctx, o.st); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, 'P.1.1', ?, ?)`,
		uuid.NewString(), o.st.ID.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
	sektor := "P"
	osoba := func(ime string, uloga models.Role) (*models.User, *models.UserPermissions) {
		u := models.User{ID: uuid.New(), FullName: ime, IsActive: true,
			Duties: []models.Duty{{ID: uuid.New(), Role: uloga, ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}}}
		return &u, models.NewUserPermissions(u)
	}
	o.ana, o.anaOvl = osoba("Ana Anić", models.RoleOperator)
	o.pero, o.peroOvl = osoba("Pero Perić", models.RoleSectorLeader)
	o.upisi = func(autor *models.User, cm int) models.Reading {
		v := cm
		rd := models.Reading{ID: uuid.New(), StationID: o.st.ID.String(), MeasuredAt: time.Now().Add(-time.Duration(cm) * time.Minute),
			LevelCm: &v, Source: models.ReadingSourceManual, UserID: autor.ID.String()}
		if err := readingRepo.Create(ctx, &rd); err != nil {
			t.Fatal(err)
		}
		return rd
	}
	o.anino, o.perino = o.upisi(o.ana, 101), o.upisi(o.pero, 102)
	return o
}

// zovi šalje zahtjev u ime osobe
func (o *okolinaPrava) zovi(u *models.User, perms *models.UserPermissions, r *http.Request) *httptest.ResponseRecorder {
	c := context.WithValue(r.Context(), contextKeyUser, u)
	c = context.WithValue(c, contextKeyPerms, perms)
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

// Olovka i gumb za brisanje očitanja (Odluke (8. 10. 2026.), pitanje 9):
// olovka stoji samo uz očitanja koja osoba smije ispraviti, a „Obriši
// očitanje” vidi samo onaj tko odgovara za letvu.
func TestOlovkaIBrisanjeOcitanjaPoPravu(t *testing.T) {
	o := novaOkolinaPrava(t)
	st, ana, anaOvl, pero, peroOvl, anino, perino := o.st, o.ana, o.anaOvl, o.pero, o.peroOvl, o.anino, o.perino
	zovi := func(u *models.User, perms *models.UserPermissions, putanja string) *httptest.ResponseRecorder {
		t.Helper()
		return o.zovi(u, perms, httptest.NewRequest(http.MethodGet, putanja, nil))
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

// Ispravak iz datoteke (Odluke (8. 10. 2026.), pitanje 9): pregled prije
// upisa provjerava pravo za svaki redak kao olovka i gumb za brisanje. Tko
// upisuje, a ne odgovara za letvu, vidi da tuđe očitanje ne smije ni
// ispraviti ni obrisati, i ne nudi mu se upis tih redaka; svoje ispravlja.
func TestIspravakIzDatotekePoPravuRetka(t *testing.T) {
	o := novaOkolinaPrava(t)
	jos := o.upisi(o.pero, 103)
	redak := func(rd models.Reading, cm int, obrisi string) string {
		return fmt.Sprintf("%s;%s;%d;;;;;;%s;;\n", rd.ID, rd.LocalTime().Format("2006-01-02 15:04"), cm, obrisi)
	}
	csv := strings.Join(stupciOcitanja, ";") + "\n" +
		redak(o.anino, 111, "") + redak(o.perino, 102, "da") + redak(jos, 113, "")
	vrati := func(u *models.User, perms *models.UserPermissions) string {
		t.Helper()
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		fw, err := mw.CreateFormFile("datoteka", "primjerovo_ocitanja.csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(csv)); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/readings/station/"+o.st.ID.String()+"/ocitanja/uvoz", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		w := o.zovi(u, perms, r)
		if w.Code != http.StatusOK {
			t.Fatalf("pregled ispravaka: %d %s", w.Code, w.Header().Get("Location"))
		}
		return w.Body.String()
	}
	ponudjeno := func(html string, rd models.Reading) bool {
		return strings.Contains(html, `name="id" value="`+rd.ID.String()+`"`)
	}

	html := vrati(o.ana, o.anaOvl)
	for _, zeli := range []string{"1 za ispravak", "2 s greškom", "nemate pravo brisati ovo očitanje", "nemate pravo ispraviti ovo očitanje"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("u pregledu operaterke nema %q", zeli)
		}
	}
	if strings.Contains(html, "za brisanje") || !ponudjeno(html, o.anino) || ponudjeno(html, o.perino) || ponudjeno(html, jos) {
		t.Error("operaterki se nudi samo ispravak vlastitog očitanja")
	}

	html = vrati(o.pero, o.peroOvl)
	if !strings.Contains(html, "1 za brisanje") || !strings.Contains(html, "2 za ispravak") || strings.Contains(html, "nemate pravo") ||
		!ponudjeno(html, o.anino) || !ponudjeno(html, o.perino) || !ponudjeno(html, jos) {
		t.Error("uprava sektora ispravlja i briše svako očitanje letve")
	}
}

// Uputa uz ispravak u tablici kaže brisanje samo onome tko smije brisati;
// ostali čitaju da ispravljaju svoja očitanja (Odluke (8. 10. 2026.),
// pitanje 9).
func TestUputaIspravkaUTabliciPoPravu(t *testing.T) {
	o := novaOkolinaPrava(t)
	stranica := func(u *models.User, perms *models.UserPermissions) string {
		t.Helper()
		w := o.zovi(u, perms, httptest.NewRequest(http.MethodGet, "/readings/station/"+o.st.ID.String(), nil))
		if w.Code != http.StatusOK {
			t.Fatalf("stranica letve: %d", w.Code)
		}
		return w.Body.String()
	}
	const brisanje, svoja = "za brisanje upiši", "ispraviti možeš samo svoja očitanja"
	if html := stranica(o.ana, o.anaOvl); strings.Contains(html, brisanje) || !strings.Contains(html, svoja) {
		t.Error("operaterka ne briše: uputa joj kaže da ispravlja svoja očitanja")
	}
	if html := stranica(o.pero, o.peroOvl); !strings.Contains(html, brisanje) || strings.Contains(html, svoja) {
		t.Error("uprava sektora briše: uputa joj kaže kako")
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
