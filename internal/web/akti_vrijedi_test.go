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

// „Vrijedi od” izvan uobičajenog raspona: obrazac se vraća s upisanim
// vremenom i pitanjem, a nacrt nastaje tek uz izričitu potvrdu
// (Odluke (8. 10. 2026.), pitanje 17)
func TestPotvrdaVrijediOdKrozObrazac(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "vrijedi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Primjerica', 'VGI Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'd.o. r. Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users, nil, "cop")
	otvoren := time.Now().AddDate(0, 0, -10)
	akti.SetObrana(func(context.Context, string) *models.Journal {
		return &models.Journal{ID: "dnevnik-cop", CentarSektor: "B", StartedAt: &otvoren}
	}, nil)
	stations := service.NewStationService(stationRepo, service.NewSectionService(sectionRepo, service.NewSSEBroker()), service.NewSSEBroker())
	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, 'B.34.1', ?, ?)`, uuid.NewString(), st.ID.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
	ana := &models.User{ID: uuid.New(), Username: "aanic", FullName: "Ana Anić", IsGlobalAdmin: true, IsActive: true}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *ana}

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
	mux.HandleFunc("GET /akti/novi", h.ShowForm)
	mux.HandleFunc("POST /akti/novi", h.HandleCreate)
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	zovi := func(metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
		if forma != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, ana), contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	forma := func(vrijedi time.Time, potvrda string) url.Values {
		return url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"PRIPREMNO"},
			"vrijedi": {vrijedi.In(models.Zagreb).Format("2006-01-02T15:04")}, "potvrda_vrijedi": {potvrda}}
	}

	obrazac := zovi(http.MethodGet, "/akti/novi?station="+st.ID.String(), nil).Body.String()
	if !strings.Contains(obrazac, "Više od 24 sata unatrag") || strings.Contains(obrazac, `type="checkbox" name="potvrda_vrijedi"`) {
		t.Errorf("obrazac bez pitanja nudi potvrdu ili ne opisuje granice")
	}

	dvaDana := time.Now().Add(-48 * time.Hour)
	l := zovi(http.MethodPost, "/akti/novi", forma(dvaDana, "")).Header().Get("Location")
	natrag, _ := url.Parse(l)
	if natrag.Path != "/akti/novi" || natrag.Query().Get("potvrdi") != "vrijedi" || !strings.Contains(natrag.Query().Get("error"), "jeste li sigurni?") {
		t.Fatalf("bez potvrde obrazac se vraća s pitanjem: %s", l)
	}
	obrazac = zovi(http.MethodGet, natrag.String(), nil).Body.String()
	for _, d := range []string{`type="checkbox" name="potvrda_vrijedi"`, `value="` + dvaDana.In(models.Zagreb).Format("2006-01-02T15:04") + `"`, "jeste li sigurni?"} {
		if !strings.Contains(obrazac, d) {
			t.Errorf("vraćeni obrazac nema %q", d)
		}
	}
	l = zovi(http.MethodPost, "/akti/novi", forma(dvaDana, "1")).Header().Get("Location")
	if !strings.Contains(l, "success=") {
		t.Fatalf("uz potvrdu nacrt nastaje: %s", l)
	}
	a, _ := akti.Get(ctx, strings.TrimPrefix(strings.SplitN(l, "?", 2)[0], "/akti/"))
	if a == nil || !a.ImaPotvrdu(models.PotvrdaVrijedi) || a.Potvrde[0].Tko != "Ana Anić" {
		t.Fatalf("potvrda nije zabilježena: %+v", a)
	}
	if s := zovi(http.MethodGet, "/akti/"+a.ID, nil).Body.String(); !strings.Contains(s, "Potvrđeno uz akt:") || !strings.Contains(s, "jeste li sigurni? Potvrđeno. (Ana Anić,") {
		t.Error("stranica akta ne pokazuje zabilježenu potvrdu")
	}
	// tvrda granica: potvrda ne pomaže, a obrazac je i ne nudi
	l = zovi(http.MethodPost, "/akti/novi", forma(time.Now().AddDate(0, 0, 10), "1")).Header().Get("Location")
	if natrag, _ = url.Parse(l); !strings.Contains(natrag.Query().Get("error"), "7 dana unaprijed") || natrag.Query().Get("potvrdi") != "" {
		t.Errorf("deset dana unaprijed: %s", l)
	}
	// neispravno vrijeme u upitu ne kvari obrazac
	var data AktiPageData
	obrazacVrijedi(url.Values{"vrijedi": {"sutra"}}, &data)
	if data.LocalValue == "sutra" || data.LocalValue == "" || data.TraziPotvrduVrijedi {
		t.Errorf("obrazac s neispravnim vremenom: %+v", data.LocalValue)
	}
}
