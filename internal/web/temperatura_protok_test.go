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

// Uz vodostaj se upisuju temperatura vode i izmjereni protok, svaki na svojoj
// kartici obrasca; vide se u povijesti letve i na pregledu svih letvi.
func TestTemperaturaIProtokKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "tp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	structureRepo := repository.NewStructureRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	readings := service.NewReadingService(readingRepo, stationRepo, structureRepo, sections, users)
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())
	structures := service.NewStructureService(structureRepo)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewReadingsHandler(readings, stations, structures, users, tmpl("readings.html"), tmpl("reading_history.html"), tmpl("reading_form.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readings", h.ShowOverview)
	mux.HandleFunc("GET /readings/new", h.ShowForm)
	mux.HandleFunc("GET /readings/station/{id}", h.ShowHistory)
	mux.HandleFunc("POST /readings/create", h.HandleCreate)

	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "proba", Name: "Proba", Watercourse: "Dunav"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	admin := &models.User{ID: uuid.New(), Username: "uprava", FullName: "Uprava", IsGlobalAdmin: true, IsActive: true}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *admin}
	zovi := func(metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if forma != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		c := context.WithValue(r.Context(), contextKeyUser, admin)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d\n%s", sto, w.Code, w.Body.String())
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

	// obrazac ima tri kartice, vodostaj prva
	mora(zovi(http.MethodGet, "/readings/new?station="+st.ID.String(), nil), "obrazac",
		`aria-controls="kartica-vodostaj"`, "Temperatura", "Protok", `name="temp_c"`, `name="flow_m3s"`, "nema krivulju protoka",
		`name="temp_note"`, `name="flow_method"`, `name="flow_note"`, "Napomena uz vodostaj")

	// upis s decimalnim zarezom
	w := zovi(http.MethodPost, "/readings/create", url.Values{"station_id": {st.ID.String()}, "level_cm": {"300"}, "temp_c": {"12,5"}, "flow_m3s": {"1250"}, "measured_at": {"2026-09-18T07:00"},
		"temp_note": {"led uz obalu"}, "flow_method": {"ADCP"}, "flow_note": {"profil kod mosta"}, "note": {"letva oštećena"}})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "success") {
		t.Fatalf("upis: %d %s", w.Code, w.Header().Get("Location"))
	}
	popis, _ := readingRepo.List(ctx, repository.ReadingFilter{StationID: st.ID.String(), Limit: 1})
	if len(popis) != 1 || popis[0].TempC == nil || *popis[0].TempC != 12.5 || popis[0].FlowM3s == nil || *popis[0].FlowM3s != 1250 {
		t.Fatalf("temperatura i protok nisu spremljeni: %+v", popis)
	}
	if popis[0].TempNote != "led uz obalu" || popis[0].FlowMethod != models.FlowMethodADCP || popis[0].FlowNote != "profil kod mosta" || popis[0].Note != "letva oštećena" {
		t.Fatalf("bilješke po veličini nisu spremljene: %+v", popis[0])
	}

	// povijest letve i pregled
	mora(zovi(http.MethodGet, "/readings/station/"+st.ID.String(), nil), "povijest", "12,5 °C", "1.250,0 m³/s</strong> izmjereno", "led uz obalu", "ADCP", "profil kod mosta", "letva oštećena")
	mora(zovi(http.MethodGet, "/readings", nil), "pregled", "1.250 m³/s", "12,5 °C")

	// samo temperatura, bez vodostaja, je valjano očitanje
	w = zovi(http.MethodPost, "/readings/create", url.Values{"station_id": {st.ID.String()}, "temp_c": {"8"}, "measured_at": {"2026-09-18T08:00"}})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "success") {
		t.Fatalf("upis samo temperature: %d %s", w.Code, w.Header().Get("Location"))
	}
	// krivi broj ne prolazi tiho
	w = zovi(http.MethodPost, "/readings/create", url.Values{"station_id": {st.ID.String()}, "level_cm": {"300"}, "temp_c": {"toplo"}, "measured_at": {"2026-09-18T09:00"}})
	if !strings.Contains(w.Header().Get("Location"), "error") {
		t.Errorf("neispravna temperatura bi trebala vratiti grešku, dobiveno %s", w.Header().Get("Location"))
	}
	// pri uređivanju očitanja koje ima samo temperaturu otvara se ta kartica
	// (skripta), a obrazac nosi vrijednost
	samoTemp, _ := readingRepo.List(ctx, repository.ReadingFilter{StationID: st.ID.String(), Limit: 1})
	mux.HandleFunc("GET /readings/edit/{id}", h.ShowForm)
	mora(zovi(http.MethodGet, "/readings/edit/"+samoTemp[0].ID.String(), nil), "uređivanje", `value="8,0"`)
}

// Protok stoji i na slici korita, ispod natpisa vode
func TestProtokNaSliciKorita(t *testing.T) {
	c := crtajKoritoP(probniProfil(), 300, sirokoKoritoM.uSustavu(batinaSKotama()))
	c.postaviProtok("Q ≈ 1 250 m³/s iz krivulje")
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("dashboard.html")...)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := tp.ExecuteTemplate(&sb, "presjekKorita", c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "Q ≈ 1 250 m³/s iz krivulje") || !strings.Contains(sb.String(), `class="korito-voda-natpis korito-protok"`) {
		t.Errorf("natpis protoka nije na slici:\n%s", sb.String())
	}
	c.postaviProtok("")
	sb.Reset()
	_ = tp.ExecuteTemplate(&sb, "presjekKorita", c)
	if strings.Contains(sb.String(), "korito-protok") {
		t.Error("bez protoka ne smije biti natpisa")
	}
}
