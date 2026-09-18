package web

import (
	"bytes"
	"context"
	"crypto/ed25519"
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
	_, kljucCvora, _ := ed25519.GenerateKey(nil)
	akti.SetKljuc(kljucCvora)
	h := NewAktiHandler(func() *service.AktService { return akti }, users, stations, tmpl("akti.html"), tmpl("akt_form.html"), tmpl("akt.html"), tmpl("primatelji.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /akti/novi", h.HandleCreate)
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	mux.HandleFunc("GET /akti/{id}/akt.pdf", h.IzvoziPDF)
	mux.HandleFunc("GET /akti/{id}/za-ispis.pdf", h.IzvoziZaIspis)
	mux.HandleFunc("POST /akti/{id}/ovjeri", h.HandleOvjeri)
	h.SetZig(tmpl("administracija_zig.html"))
	mux.HandleFunc("GET /administracija/zig", h.ShowZig)
	mux.HandleFunc("POST /administracija/zig", h.HandleZig)
	mux.HandleFunc("GET /administracija/zig/slika", h.ZigSlika)
	mux.HandleFunc("POST /profile/potpis-slika", h.HandlePotpisSlika)
	mux.HandleFunc("GET /profile/potpis-slika", h.PotpisSlika)
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

	// fotografija s mobitela prolazi kao rezerva, ali ovdje bez potpisnika ili prava
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

	// skener daje PDF; on je izvornik bajt za bajt
	skenPDF := []byte("%PDF-1.4\n% sken s potpisom i žigom\n%%EOF\n")
	if loc := posalji(id, kunac.ID.String(), "sken.pdf", skenPDF); !strings.Contains(loc, "success") {
		t.Fatalf("sken odbijen: %s", loc)
	}
	a, _ := akti.Get(ctx, id)
	if !a.Ovjeren() || a.Rucno == nil || a.Ovjerio != "Mile Kunac" || a.Broj != 1 {
		t.Fatalf("ovjera skenom: %+v", a)
	}
	if e, _ := episodes.Open(ctx, "B.34.1"); e == nil || e.Phase != models.PhasePrep {
		t.Error("obrana nije proglašena ovjerom skena")
	}
	izvornik, _ := io.ReadAll(zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/akt.pdf", nil)).Body)
	if !bytes.Equal(izvornik, skenPDF) {
		t.Error("PDF ovjerenog akta nije učitani sken")
	}
	if !strings.Contains(zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)).Body.String(), "Potpisan vlastoručno i ovjeren žigom") {
		t.Error("stranica ne pokazuje ručni potpis")
	}
	if loc := posalji(id, kunac.ID.String(), "sken.png", sken.Bytes()); !strings.Contains(loc, "već ovjeren") {
		t.Errorf("ponovni sken: %s", loc)
	}

	// žig centra: uprava sektora učita sken, pa PDF akta ovjerenog u goCOP-u nosi otisak
	perms.IsGlobalAdmin = true
	zig := image.NewRGBA(image.Rect(0, 0, 120, 120))
	for x := 0; x < 120; x++ {
		zig.Set(x, 60, color.RGBA{0, 0, 200, 255})
	}
	var zigPNG bytes.Buffer
	_ = png.Encode(&zigPNG, zig)
	var tijeloZ bytes.Buffer
	mwZ := multipart.NewWriter(&tijeloZ)
	_ = mwZ.WriteField("sektor", "B")
	fz, _ := mwZ.CreateFormFile("slika", "zig.png")
	_, _ = fz.Write(zigPNG.Bytes())
	_ = mwZ.Close()
	rz := httptest.NewRequest(http.MethodPost, "/administracija/zig", &tijeloZ)
	rz.Header.Set("Content-Type", mwZ.FormDataContentType())
	if loc := mustUnescape(zovi(rz).Header().Get("Location")); !strings.Contains(loc, "success") {
		t.Fatalf("žig: %s", loc)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/administracija/zig?sektor=B", nil)); !strings.Contains(w.Body.String(), "zig/slika?sektor=B") {
		t.Error("stranica žiga ne pokazuje spremljeni žig")
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/administracija/zig/slika?sektor=B", nil)); w.Header().Get("Content-Type") != "image/png" {
		t.Error("slika žiga")
	}
	// premala slika ne prolazi
	var tijeloM bytes.Buffer
	mwM := multipart.NewWriter(&tijeloM)
	_ = mwM.WriteField("sektor", "B")
	fm, _ := mwM.CreateFormFile("slika", "m.png")
	_, _ = fm.Write(sken.Bytes())
	_ = mwM.Close()
	rm := httptest.NewRequest(http.MethodPost, "/administracija/zig", &tijeloM)
	rm.Header.Set("Content-Type", mwM.FormDataContentType())
	if loc := mustUnescape(zovi(rm).Header().Get("Location")); !strings.Contains(loc, "premala") {
		t.Errorf("premala slika: %s", loc)
	}
	// sken vlastoručnog potpisa ovjeritelja
	pot := image.NewRGBA(image.Rect(0, 0, 240, 60))
	for x := 0; x < 240; x++ {
		pot.Set(x, 30, color.Black)
	}
	var potPNG bytes.Buffer
	_ = png.Encode(&potPNG, pot)
	var tijeloP bytes.Buffer
	mwP := multipart.NewWriter(&tijeloP)
	fp, _ := mwP.CreateFormFile("slika", "potpis.png")
	_, _ = fp.Write(potPNG.Bytes())
	_ = mwP.Close()
	rp := httptest.NewRequest(http.MethodPost, "/profile/potpis-slika", &tijeloP)
	rp.Header.Set("Content-Type", mwP.FormDataContentType())
	if loc := mustUnescape(zovi(rp).Header().Get("Location")); !strings.Contains(loc, "success") {
		t.Fatalf("sken potpisa: %s", loc)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/profile/potpis-slika", nil)); w.Header().Get("Content-Type") != "image/png" {
		t.Error("slika potpisa")
	}
	// akt ovjeren u goCOP-u: PDF nosi žig i potpis, ispis za ruku ne
	forma2 := url.Values{"station_id": {st.ID.String()}, "radnja": {"PREKID"}, "stupanj": {"PRIPREMNO"}, "vrijedi": {"2026-09-16T09:00"}}
	r2 := httptest.NewRequest(http.MethodPost, "/akti/novi", strings.NewReader(forma2.Encode()))
	r2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	id2 := strings.TrimPrefix(strings.SplitN(zovi(r2).Header().Get("Location"), "?", 2)[0], "/akti/")
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id2+"/za-ispis.pdf", nil)); bytes.Contains(w.Body.Bytes(), []byte("/Subtype /Image")) {
		t.Error("ispis za vlastoručni potpis ne nosi skenirani žig")
	}
	r3 := httptest.NewRequest(http.MethodPost, "/akti/"+id2+"/ovjeri", strings.NewReader(""))
	r3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if loc := mustUnescape(zovi(r3).Header().Get("Location")); !strings.Contains(loc, "success") {
		t.Fatalf("ovjera u goCOP-u: %s", loc)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id2+"/akt.pdf", nil)); bytes.Count(w.Body.Bytes(), []byte("/Subtype /Image")) < 2 {
		t.Errorf("PDF ovjerenog akta mora nositi žig i potpis, slika: %d", bytes.Count(w.Body.Bytes(), []byte("/Subtype /Image")))
	}
}

func mustUnescape(s string) string {
	u, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return u
}
