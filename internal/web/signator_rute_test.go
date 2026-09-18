package web

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"io/fs"
	"mime/multipart"
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
	"gocop/internal/pdfpotpis"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Tok sa SIGNATOR-om: voditelj COP-a sastavi nacrt i preuzme PDF za potpis,
// rukovoditelj ga potpiše kvalificiranim potpisom, potpisani PDF se vrati u
// goCOP i postane izvornik. Tuđi, nekvalificiran ili zastario potpis ne prolazi.
func TestPotpisUSignatoruKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "sig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email) VALUES ('B', 'Sektor B', 'VGO za Dunav i donju Dravu, Osijek', 'COP Osijek', 'Splavarska 2a, 31000 Osijek', '031/252-802', 'copos@voda.hr')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'međudržavne rijeke Drava i Dunav', 'COP', 'Osijek')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'd.o. r. Dunav', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop-osijek")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	sections := service.NewSectionService(sectionRepo, service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	episodes := service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users, episodes, "cop-osijek")
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())
	ctx := context.Background()

	st := &models.Station{ID: uuid.New(), Code: "batina", Name: "Batina", Watercourse: "Dunav"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, 'B.34.1', ?, ?)`, uuid.NewString(), st.ID.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
	b, bp := "B", 34
	kunac := &models.User{ID: uuid.New(), Username: "mkunac", FullName: "Mile Kunac", IsActive: true}
	if err := userRepo.CreateUser(kunac, &models.Duty{Title: "Rukovoditelj BP 34", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &b, AreaID: &bp, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	dionica := &models.User{ID: uuid.New(), Username: "iivic", FullName: "Ivo Ivić", IsActive: true}
	if err := userRepo.CreateUser(dionica, &models.Duty{Title: "Rukovoditelj dionice", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, SectorID: &b, AreaID: &bp, SectionCodes: "B.34.1"}); err != nil {
		t.Fatal(err)
	}
	voditelj := &models.User{ID: uuid.New(), Username: "voditelj", FullName: "Voditelj COP-a"}
	perms := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}, User: *voditelj}

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewAktiHandler(func() *service.AktService { return akti }, users, stations, tmpl("akti.html"), tmpl("akt_form.html"), tmpl("akt.html"), tmpl("primatelji.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /akti/novi", h.HandleCreate)
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	mux.HandleFunc("GET /akti/{id}/akt.pdf", h.IzvoziPDF)
	mux.HandleFunc("GET /akti/{id}/za-potpis.pdf", h.IzvoziZaPotpis)
	mux.HandleFunc("POST /akti/{id}/potpisani", h.HandleUcitajPotpisani)
	mux.HandleFunc("POST /akti/{id}/tekst", h.HandleTekst)
	zovi := func(r *http.Request) *httptest.ResponseRecorder {
		c := context.WithValue(r.Context(), contextKeyUser, voditelj)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	ucitaj := func(id string, pdf []byte) string {
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		fw, _ := mw.CreateFormFile("potpisani", "potpisan.pdf")
		_, _ = fw.Write(pdf)
		_ = mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/akti/"+id+"/potpisani", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		return zovi(r).Header().Get("Location")
	}

	// voditelj COP-a sastavi nacrt
	forma := url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"PRIPREMNO"}, "vrijedi": {"2026-09-15T09:00"}, "prognoza": {"najavljen porast"}}
	r := httptest.NewRequest(http.MethodPost, "/akti/novi", strings.NewReader(forma.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	id := strings.TrimPrefix(strings.SplitN(zovi(r).Header().Get("Location"), "?", 2)[0], "/akti/")
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)); !strings.Contains(w.Body.String(), "Preuzmi PDF za potpis") {
		t.Fatalf("nacrt nema korake za SIGNATOR:\n%.400s", w.Body.String())
	}

	// PDF za potpis: isti dvaput, bez oznake nacrta, zabilježen
	w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/za-potpis.pdf", nil))
	zaPotpis := w.Body.Bytes()
	w2 := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/za-potpis.pdf", nil))
	if !bytes.HasPrefix(zaPotpis, []byte("%PDF")) || !bytes.Equal(zaPotpis, w2.Body.Bytes()) {
		t.Fatal("PDF za potpis nije isti pri ponovnom preuzimanju")
	}
	if a, _ := akti.Get(ctx, id); len(a.ZaPotpis) != 1 {
		t.Fatalf("preuzimanje nije zabilježeno: %+v", a.ZaPotpis)
	}

	potpisi := func(ime string, kval bool, pdf []byte) []byte {
		c, k, err := pdfpotpis.ProbniCertifikat(ime, "12345678903", kval)
		if err != nil {
			t.Fatal(err)
		}
		p, err := pdfpotpis.ProbnoPotpisi(pdf, c, k)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	// ne prolaze: nekvalificiran, nepoznat potpisnik, potpisnik bez prava,
	// dokument koji nije PDF za potpis
	for _, c := range []struct {
		opis, ime string
		kval      bool
		pdf       []byte
		greska    string
	}{
		{"nekvalificiran", "Mile Kunac", false, zaPotpis, "nije kvalificiran"},
		{"nepoznat", "Netko Treći", true, zaPotpis, "nije pronađen"},
		{"bez prava", "Ivo Ivić", true, zaPotpis, "ne smije ovjeriti"},
		{"drugi dokument", "Mile Kunac", true, []byte("%PDF-1.4\n% drugi\n%%EOF\n"), "nije PDF za potpis"},
	} {
		if loc := ucitaj(id, potpisi(c.ime, c.kval, c.pdf)); !strings.Contains(loc, "error") || !strings.Contains(mustUnescape(loc), c.greska) {
			t.Errorf("%s: %s", c.opis, mustUnescape(loc))
		}
	}

	// ispravak teksta poništava preuzeti PDF
	a, _ := akti.Get(ctx, id)
	r = httptest.NewRequest(http.MethodPost, "/akti/"+id+"/tekst", strings.NewReader(url.Values{"uvod": {a.Uvod + " (ispravljeno)"}, "zavrsno": {a.Zavrsno}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	zovi(r)
	if loc := ucitaj(id, potpisi("Mile Kunac", true, zaPotpis)); !strings.Contains(mustUnescape(loc), "nije PDF za potpis") {
		t.Errorf("potpisan stari tekst prošao: %s", mustUnescape(loc))
	}

	// novi PDF za potpis, rukovoditelj potpiše, voditelj vrati u goCOP
	zaPotpis = zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/za-potpis.pdf", nil)).Body.Bytes()
	potpisan := potpisi("KUNAC MILE", true, zaPotpis)
	if loc := ucitaj(id, potpisan); !strings.Contains(loc, "success") {
		t.Fatalf("ispravno potpisan PDF odbijen: %s", mustUnescape(loc))
	}
	a, _ = akti.Get(ctx, id)
	if !a.Ovjeren() || a.Kvalificirani == nil || a.Ovjerio != "Mile Kunac" || a.UZamjeni || a.Broj != 1 {
		t.Fatalf("ovjera kvalificiranim potpisom: %+v", a)
	}
	if e, _ := episodes.Open(ctx, "B.34.1"); e == nil || e.Phase != models.PhasePrep {
		t.Error("obrana nije proglašena ovjerom iz SIGNATOR-a")
	}
	// izvornik je bajt za bajt potpisani PDF
	w = zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/akt.pdf", nil))
	izvornik, _ := io.ReadAll(w.Body)
	if !bytes.Equal(izvornik, potpisan) {
		t.Error("PDF ovjerenog akta nije potpisani izvornik")
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)); !strings.Contains(w.Body.String(), "Kvalificirano potpisan u SIGNATOR-u") {
		t.Error("stranica ne pokazuje kvalificirani potpis")
	}
	if loc := ucitaj(id, potpisan); !strings.Contains(mustUnescape(loc), "već ovjeren") {
		t.Error("ponovno učitavanje mora javiti da je akt ovjeren")
	}
}

func mustUnescape(s string) string {
	u, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return u
}
