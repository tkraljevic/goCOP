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
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "sken.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
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
	anic := &models.User{ID: uuid.New(), Username: "aanic", FullName: "Ana Anić", IsActive: true}
	if err := userRepo.CreateUser(anic, &models.Duty{Title: "Rukovoditelj BP 34", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &b, AreaID: &bp, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	dionica := &models.User{ID: uuid.New(), Username: "iivic", FullName: "Ivo Ivić", IsActive: true}
	if err := userRepo.CreateUser(dionica, &models.Duty{Title: "Rukovoditelj dionice", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, SectorID: &b, AreaID: &bp, SectionCodes: "B.34.1"}); err != nil {
		t.Fatal(err)
	}
	// voditelj COP-a sastavlja nacrt po dužnosti voditelja centra sektora B
	// (namjerno promijenjeno prema Odlukama (8. 10. 2026.), pitanje 18: nacrt
	// sastavlja samo Centar obrane od poplava, a ne uprava sektora kao takva)
	voditelj := &models.User{ID: uuid.New(), Username: "voditelj", FullName: "Voditelj COP-a",
		Duties: []models.Duty{{Title: "Voditelj COP-a", Role: models.RoleCopLeader, ScopeType: models.ScopeSector, SectorID: &b, IsActive: true}}}
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
	mux.HandleFunc("POST /akti/{id}/obrisi", h.HandleObrisi)
	h.SetZig(tmpl("sektor.html"))
	mux.HandleFunc("GET /organizacija/sektori/{id}", h.ShowSektor)
	mux.HandleFunc("POST /organizacija/sektori/{id}/zig", h.HandleZig)
	mux.HandleFunc("GET /organizacija/sektori/{id}/zig/slika", h.ZigSlika)
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
		// dionica nema teritorija: sken s tim popisom ovjerava se uz potvrdu
		// (Odluke (8. 10. 2026.), pitanje 19)
		_ = mw.WriteField("potvrda_primatelja", "1")
		_ = mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/akti/"+id+"/sken", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		return mustUnescape(zovi(r).Header().Get("Location"))
	}

	forma := url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"PRIPREMNO"}, "vrijedi": {"2026-09-15T09:00"}, "potvrda_vrijedi": {"1"}}
	r := httptest.NewRequest(http.MethodPost, "/akti/novi", strings.NewReader(forma.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	id := strings.TrimPrefix(strings.SplitN(zovi(r).Header().Get("Location"), "?", 2)[0], "/akti/")

	// sken ovjere učitava samo onaj tko akt smije ovjeriti (Odluke
	// (11. 10. 2026.)): voditelj centra nacrt ispisuje, ali skena ne učitava
	stranica := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)).Body.String()
	if !strings.Contains(stranica, "PDF za ispis") || strings.Contains(stranica, "Učitaj sken") {
		t.Fatal("voditelju centra stranica nacrta nudi ispis, a ne sken")
	}
	if loc := posalji(id, anic.ID.String(), "sken.pdf", []byte("%PDF-1.4\n%%EOF\n")); !strings.Contains(loc, "error") || !strings.Contains(loc, "smije ovjeriti") {
		t.Fatalf("sken voditelja centra: %s", loc)
	}
	// namjerna izmjena testa: voditelj je ujedno zamjenik rukovoditelja
	// sektora, pa akt smije ovjeriti i sken učitati
	perms.User.Duties = append(perms.User.Duties, models.Duty{Title: "Zamjenik rukovoditelja sektora B", Role: models.RoleSectorDeputy,
		ScopeType: models.ScopeSector, SectorID: &b, IsActive: true})
	stranica = zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)).Body.String()
	for _, ocekivano := range []string{"PDF za ispis", "Učitaj sken", "Ana Anić"} {
		if !strings.Contains(stranica, ocekivano) {
			t.Fatalf("stranica nacrta nema %q", ocekivano)
		}
	}
	if strings.Contains(stranica, `value="`+dionica.ID.String()+`"`) {
		t.Error("rukovoditelj dionice ne smije biti ponuđen kao potpisnik")
	}
	// tuđim očima se akt ne ovjerava, pa stranica ne nudi ni ovjeru ni sken
	rTudje := httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)
	rTudje = rTudje.WithContext(context.WithValue(rTudje.Context(), contextKeyViewing, true))
	if s := zovi(rTudje).Body.String(); !strings.Contains(s, "PDF za ispis") || strings.Contains(s, "Učitaj sken") || strings.Contains(s, "Ovjeri u goCOP-u") {
		t.Error("tuđim očima stranica nacrta nudi ovjeru")
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
		{"nije sken", anic.ID.String(), []byte("nešto"), "PDF"},
	} {
		if loc := posalji(id, c.potpisnik, "sken.png", c.podaci); !strings.Contains(loc, "error") || !strings.Contains(loc, c.greska) {
			t.Errorf("%s: %s", c.opis, loc)
		}
	}

	// skener daje PDF; on je izvornik bajt za bajt
	skenPDF := []byte("%PDF-1.4\n% sken s potpisom i žigom\n%%EOF\n")
	if loc := posalji(id, anic.ID.String(), "sken.pdf", skenPDF); !strings.Contains(loc, "success") {
		t.Fatalf("sken odbijen: %s", loc)
	}
	a, _ := akti.Get(ctx, id)
	if !a.Ovjeren() || a.Rucno == nil || a.Ovjerio != "Ana Anić" || a.Broj != 1 {
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
	if loc := posalji(id, anic.ID.String(), "sken.png", sken.Bytes()); !strings.Contains(loc, "već ovjeren") {
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
	// stara adresa obrasca i dalje sprema, a vraća na vrh stranice sektora,
	// gdje stoji poruka
	if loc := mustUnescape(zovi(rz).Header().Get("Location")); !strings.Contains(loc, "success") || !strings.HasPrefix(loc, "/organizacija/sektori/B?") || strings.Contains(loc, "#") {
		t.Fatalf("žig: %s", loc)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/organizacija/sektori/B", nil)); !strings.Contains(w.Body.String(), "/organizacija/sektori/B/zig/slika?t=") {
		t.Error("stranica sektora ne pokazuje spremljeni žig")
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/administracija/zig?sektor=B", nil)); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/organizacija/sektori/B#zig" {
		t.Errorf("stara stranica žiga: %d %s", w.Code, w.Header().Get("Location"))
	}
	for _, put := range []string{"/organizacija/sektori/B/zig/slika", "/administracija/zig/slika?sektor=B"} {
		if w := zovi(httptest.NewRequest(http.MethodGet, put, nil)); w.Header().Get("Content-Type") != "image/png" {
			t.Errorf("slika žiga %s", put)
		}
	}
	// sliku žiga ne vidi tko nije uprava sektora
	perms.IsGlobalAdmin = false
	obicni := &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}, User: *voditelj}
	rObicni := httptest.NewRequest(http.MethodGet, "/organizacija/sektori/B/zig/slika", nil)
	cObicni := context.WithValue(context.WithValue(rObicni.Context(), contextKeyUser, voditelj), contextKeyPerms, obicni)
	wObicni := httptest.NewRecorder()
	mux.ServeHTTP(wObicni, rObicni.WithContext(cObicni))
	if wObicni.Code != http.StatusForbidden {
		t.Errorf("žig smije vidjeti samo uprava sektora, a dobiveno %d", wObicni.Code)
	}
	perms.IsGlobalAdmin = true
	// premala slika ne prolazi
	var tijeloM bytes.Buffer
	mwM := multipart.NewWriter(&tijeloM)
	fm, _ := mwM.CreateFormFile("slika", "m.png")
	_, _ = fm.Write(sken.Bytes())
	_ = mwM.Close()
	rm := httptest.NewRequest(http.MethodPost, "/organizacija/sektori/B/zig", &tijeloM)
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
	forma2 := url.Values{"station_id": {st.ID.String()}, "radnja": {"PREKID"}, "stupanj": {"PRIPREMNO"}, "vrijedi": {"2026-09-16T09:00"}, "potvrda_vrijedi": {"1"}}
	r2 := httptest.NewRequest(http.MethodPost, "/akti/novi", strings.NewReader(forma2.Encode()))
	r2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	id2 := strings.TrimPrefix(strings.SplitN(zovi(r2).Header().Get("Location"), "?", 2)[0], "/akti/")
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id2+"/za-ispis.pdf", nil)); bytes.Contains(w.Body.Bytes(), []byte("/Subtype /Image")) {
		t.Error("ispis za vlastoručni potpis ne nosi skenirani žig")
	}
	otisak := otisakSaStranice(t, zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id2, nil)).Body.String())
	r3 := httptest.NewRequest(http.MethodPost, "/akti/"+id2+"/ovjeri", strings.NewReader(url.Values{"otisak": {otisak}, "potvrda_primatelja": {"1"}}.Encode()))
	r3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if loc := mustUnescape(zovi(r3).Header().Get("Location")); !strings.Contains(loc, "success") {
		t.Fatalf("ovjera u goCOP-u: %s", loc)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id2+"/akt.pdf", nil)); bytes.Count(w.Body.Bytes(), []byte("/Subtype /Image")) < 2 {
		t.Errorf("PDF ovjerenog akta mora nositi žig i potpis, slika: %d", bytes.Count(w.Body.Bytes(), []byte("/Subtype /Image")))
	}
	// ovjeren akt bez izvornika (npr. iz ranije evidencije) crta se iznova:
	// žig ostaje, a sken potpisa ide samo u izvornik pri ovjeri
	if _, err := baza.Exec(`DELETE FROM akti_izvornici WHERE akt_id = ?`, id2); err != nil {
		t.Fatal(err)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id2+"/akt.pdf", nil)); bytes.Count(w.Body.Bytes(), []byte("/Subtype /Image")) != 1 {
		t.Errorf("akt bez izvornika crta se sa žigom, bez skena potpisa, slika: %d", bytes.Count(w.Body.Bytes(), []byte("/Subtype /Image")))
	}

	// trajno brisanje ovjerenog akta: samo uz uključenu opciju
	rb := httptest.NewRequest(http.MethodPost, "/akti/"+id2+"/obrisi", strings.NewReader(""))
	rb.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if loc := mustUnescape(zovi(rb).Header().Get("Location")); !strings.Contains(loc, "Opcije") {
		t.Fatalf("bez opcije se ovjeren akt ne briše: %s", loc)
	}
	if err := akti.SpremiOpcije(ctx, &models.UserPermissions{}, models.Opcije{BrisanjeOvjerenihAkata: true}); err == nil {
		t.Error("opcije smije mijenjati samo uprava organizacije")
	}
	if err := akti.SpremiOpcije(ctx, perms, models.Opcije{BrisanjeOvjerenihAkata: true}); err != nil {
		t.Fatal(err)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id2, nil)); !strings.Contains(w.Body.String(), "Obriši trajno") {
		t.Error("s uključenom opcijom stranica nudi trajno brisanje")
	}
	rb = httptest.NewRequest(http.MethodPost, "/akti/"+id2+"/obrisi", strings.NewReader(""))
	rb.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if loc := mustUnescape(zovi(rb).Header().Get("Location")); !strings.Contains(loc, "trajno obrisan") {
		t.Fatalf("trajno brisanje: %s", loc)
	}
	if a2, _ := akti.Get(ctx, id2); a2 != nil {
		t.Error("akt nije obrisan")
	}
	if pdf, _ := akti.Izvornik(ctx, id2); pdf != nil {
		t.Error("izvornik nije obrisan")
	}
}

func mustUnescape(s string) string {
	u, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return u
}
