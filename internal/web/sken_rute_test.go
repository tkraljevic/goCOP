package web

import (
	"bytes"
	"context"
	"html/template"
	"image"
	"image/color"
	"image/png"
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
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Drugi put: nacrt se ispiše, rukovoditelj ga vlastoručno potpiše, udari se
// žig, sken se učita u goCOP i postane izvornik. Potpisnik mora imati pravo
// ovjere; bez odabranog potpisnika ili sa slikom koja nije sken ne prolazi.
func TestRucniPotpisISkenKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "sken.db"))
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
	mux.HandleFunc("GET /akti/{id}/za-ispis.pdf", h.IzvoziZaIspis)
	mux.HandleFunc("POST /akti/{id}/sken", h.HandleUcitajSken)
	zovi := func(r *http.Request) *httptest.ResponseRecorder {
		c := context.WithValue(r.Context(), contextKeyUser, voditelj)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	posalji := func(id, potpisnik, ime string, podaci []byte) string {
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		fw, _ := mw.CreateFormFile("sken", ime)
		_, _ = fw.Write(podaci)
		_ = mw.WriteField("potpisnik_id", potpisnik)
		_ = mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/akti/"+id+"/sken", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		return mustUnescape(zovi(r).Header().Get("Location"))
	}

	forma := url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"PRIPREMNO"}, "vrijedi": {"2026-09-15T09:00"}}
	r := httptest.NewRequest(http.MethodPost, "/akti/novi", strings.NewReader(forma.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	id := strings.TrimPrefix(strings.SplitN(zovi(r).Header().Get("Location"), "?", 2)[0], "/akti/")

	stranica := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)).Body.String()
	for _, ocekivano := range []string{"PDF za ispis", "Učitaj sken", "Mile Kunac"} {
		if !strings.Contains(stranica, ocekivano) {
			t.Fatalf("stranica nacrta nema %q", ocekivano)
		}
	}
	if strings.Contains(stranica, `value="`+dionica.ID.String()+`"`) {
		t.Error("rukovoditelj dionice ne smije biti ponuđen kao potpisnik")
	}

	ispis := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/za-ispis.pdf", nil)).Body.Bytes()
	if !bytes.HasPrefix(ispis, []byte("%PDF")) || !bytes.Contains(ispis, []byte("/Subject")) {
		t.Fatal("PDF za ispis nije PDF")
	}

	// sken fotografiran mobitelom: PNG
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))
	for x := 0; x < 40; x++ {
		img.Set(x, 30, color.Black)
	}
	var sken bytes.Buffer
	_ = png.Encode(&sken, img)

	for _, c := range []struct {
		opis, potpisnik string
		podaci          []byte
		greska          string
	}{
		{"bez potpisnika", "", sken.Bytes(), "potpis"},
		{"bez prava", dionica.ID.String(), sken.Bytes(), "ne smije ovjeriti"},
		{"nije sken", kunac.ID.String(), []byte("nešto"), "PDF"},
	} {
		if loc := posalji(id, c.potpisnik, "sken.png", c.podaci); !strings.Contains(loc, "error") || !strings.Contains(loc, c.greska) {
			t.Errorf("%s: %s", c.opis, loc)
		}
	}

	if loc := posalji(id, kunac.ID.String(), "sken.png", sken.Bytes()); !strings.Contains(loc, "success") {
		t.Fatalf("sken odbijen: %s", loc)
	}
	a, _ := akti.Get(ctx, id)
	if !a.Ovjeren() || a.Rucno == nil || a.Ovjerio != "Mile Kunac" || a.Broj != 1 || a.Kvalificirani != nil {
		t.Fatalf("ovjera skenom: %+v", a)
	}
	if e, _ := episodes.Open(ctx, "B.34.1"); e == nil || e.Phase != models.PhasePrep {
		t.Error("obrana nije proglašena ovjerom skena")
	}
	izvornik, _ := io.ReadAll(zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/akt.pdf", nil)).Body)
	if !bytes.HasPrefix(izvornik, []byte("%PDF")) || !bytes.Contains(izvornik, []byte("/Subtype /Image")) {
		t.Error("PDF ovjerenog akta nije sken")
	}
	if !strings.Contains(zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)).Body.String(), "Potpisan vlastoručno i ovjeren žigom") {
		t.Error("stranica ne pokazuje ručni potpis")
	}
	if loc := posalji(id, kunac.ID.String(), "sken.png", sken.Bytes()); !strings.Contains(loc, "već ovjeren") {
		t.Errorf("ponovni sken: %s", loc)
	}
}
