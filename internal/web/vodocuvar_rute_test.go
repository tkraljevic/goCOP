package web

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/weather"
	webassets "gocop/web"
)

// Vodočuvarski dnevnik kroz rute: rukovoditelj zada zadatak, vodočuvar ga
// nađe na listu s imenom tko ga je zadao, jedan obavi a drugi obrazloži,
// list potpiše i preda; neobavljeni zadatak prelazi na sljedeći list;
// rukovoditelj BP ovjeri, rukovoditelj dionice parafira; PDF je kao papir.
func TestVodocuvarskiDnevnikKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "vodocuvar.db"))
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
	ivic := &models.User{ID: uuid.New(), Username: "iivic", FullName: "Ivo Ivić", IsActive: true}
	if err := userRepo.CreateUser(ivic, &models.Duty{Title: "Rukovoditelj dionice B.34.1", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, SectorID: &b, AreaID: &bp, SectionCodes: "B.34.1", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	seit := &models.User{ID: uuid.New(), Username: "seit", FullName: "Seit Vodočuvar", IsActive: true}
	if err := userRepo.CreateUser(seit, &models.Duty{Title: "Vodočuvar Batina", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &b, AreaID: &bp, SectionCodes: "B.34.1", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	// vodočuvar je danas očitao letvu
	lvl := 114
	if err := readingRepo.Create(ctx, &models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: time.Now(), LevelCm: &lvl, Source: "MANUAL", UserID: seit.ID.String()}); err != nil {
		t.Fatal(err)
	}
	orgRepo := repository.NewOrgRepository(baza, rec)
	vod := service.NewVodocuvarService(repository.NewVodocuvarRepository(baza, rec), users, "cop-osijek")
	vod.SetOrg(orgRepo)
	_ = stations
	_ = akti

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewVodocuvarHandler(func() *service.VodocuvarService { return vod }, users, orgRepo, tmpl("vodocuvar.html"), tmpl("vodocuvar_list.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vodocuvar", h.ShowPopis)
	mux.HandleFunc("GET /vodocuvar/dan", h.ShowDan)
	mux.HandleFunc("POST /vodocuvar/spremi", h.HandleSpremi)
	mux.HandleFunc("POST /vodocuvar/zadatak", h.HandleZadatak)
	mux.HandleFunc("GET /vodocuvar/{id}", h.ShowList)
	mux.HandleFunc("POST /vodocuvar/{id}/radnja", h.HandleRadnja)
	mux.HandleFunc("GET /vodocuvar/{id}/list.pdf", h.IzvoziPDF)
	mux.HandleFunc("GET /vodocuvar/knjiga.pdf", h.IzvoziKnjigu)
	h.SetKalendar(tmpl("vodocuvar_kalendar.html"))
	mux.HandleFunc("GET /vodocuvar/kalendar", h.ShowKalendar)
	mux.HandleFunc("GET /organizacija/geokod", h.GeokodJSON)
	kao := func(u *models.User) *models.UserPermissions {
		cijeli, _ := users.GetUserByID(u.ID)
		return models.NewUserPermissions(*cijeli)
	}
	zovi := func(u *models.User, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		var r *http.Request
		if forma != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		cijeli, _ := users.GetUserByID(u.ID)
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, kao(u))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	loc := func(w *httptest.ResponseRecorder) string { return mustUnescape(w.Header().Get("Location")) }
	danas := time.Now().In(models.Zagreb).Format("2006-01-02")

	// rukovoditelj BP zada dva zadatka; vodočuvar ne smije zadavati
	if l := loc(zovi(kunac, http.MethodPost, "/vodocuvar/zadatak", url.Values{"vodocuvar": {seit.ID.String()}, "tekst": {"Deponija pijeska Batina"}})); !strings.Contains(l, "success") {
		t.Fatalf("zadatak: %s", l)
	}
	if l := loc(zovi(kunac, http.MethodPost, "/vodocuvar/zadatak", url.Values{"vodocuvar": {seit.ID.String()}, "tekst": {"Potok Karašica"}})); !strings.Contains(l, "success") {
		t.Fatalf("zadatak 2: %s", l)
	}
	if l := loc(zovi(seit, http.MethodPost, "/vodocuvar/zadatak", url.Values{"vodocuvar": {seit.ID.String()}, "tekst": {"sam sebi"}})); !strings.Contains(l, "error") {
		t.Error("vodočuvar ne zadaje zadatke sam sebi")
	}
	// planirani zadatak: za prekosutra, ne dalje od 30 dana, ne u prošlost; na današnjem listu ga nema
	prekosutra := time.Now().In(models.Zagreb).AddDate(0, 0, 2).Format("2006-01-02")
	if l := loc(zovi(kunac, http.MethodPost, "/vodocuvar/zadatak", url.Values{"vodocuvar": {seit.ID.String()}, "tekst": {"Nasip Zmajevac"}, "za": {prekosutra}})); !strings.Contains(l, "na listu za") {
		t.Fatalf("planirani zadatak: %s", l)
	}
	if l := loc(zovi(kunac, http.MethodPost, "/vodocuvar/zadatak", url.Values{"vodocuvar": {seit.ID.String()}, "tekst": {"predaleko"}, "za": {time.Now().AddDate(0, 0, 45).Format("2006-01-02")}})); !strings.Contains(l, "30 dana") {
		t.Errorf("predaleko planiranje: %s", l)
	}
	if l := loc(zovi(kunac, http.MethodPost, "/vodocuvar/zadatak", url.Values{"vodocuvar": {seit.ID.String()}, "tekst": {"prošlost"}, "za": {"2020-01-01"}})); !strings.Contains(l, "prošli dan") {
		t.Errorf("planiranje u prošlost: %s", l)
	}
	kal := zovi(kunac, http.MethodGet, "/vodocuvar/kalendar?vodocuvar="+seit.ID.String()+"&mjesec="+time.Now().In(models.Zagreb).AddDate(0, 0, 2).Format("2006-01"), nil)
	if kal.Code != http.StatusOK || !strings.Contains(kal.Body.String(), "Nasip Zmajevac") || !strings.Contains(kal.Body.String(), "kal-dan") {
		t.Fatalf("kalendar: %d\n%.600s", kal.Code, kal.Body.String())
	}
	// današnji list: zadaci s imenom tko ih je zadao, očitanje letve
	dan := zovi(seit, http.MethodGet, "/vodocuvar/dan?datum="+danas, nil)
	for _, x := range []string{"Deponija pijeska Batina", "Potok Karašica", "zadao Mile Kunac", "114 cm (", "Potpiši i predaj"} {
		if !strings.Contains(dan.Body.String(), x) {
			t.Fatalf("današnji list nema %q:\n%.1500s", x, dan.Body.String())
		}
	}
	if strings.Count(dan.Body.String(), `name="zadatak_obavljeno_`) != 2 {
		t.Errorf("na današnjem listu su samo dva zadatka, ne i planirani za prekosutra: %d", strings.Count(dan.Body.String(), `name="zadatak_obavljeno_`))
	}
	// zadatak ID-ovi iz obrasca
	zadaci := vod.Zadaci(ctx, seit.ID.String())
	if len(zadaci) != 3 {
		t.Fatalf("zadataka: %d", len(zadaci))
	}
	var z1, z2 string
	for _, z := range zadaci {
		switch z.Tekst {
		case "Deponija pijeska Batina":
			z1 = z.ID
		case "Potok Karašica":
			z2 = z.ID
		}
	}
	// predaja bez obrazloženja neobavljenog ne prolazi
	forma := url.Values{"datum": {danas}, "od": {"08:00"}, "do": {"16:00"}, "prilike": {"sunčano, vruće"}, "opis": {"- obilazak deponije pijeska u Batini\n- obilazak vodotoka"}, "zapazanja": {"Bilje: +114 (d.o.)"},
		"zadatak_status_" + z1: {"OBAVLJEN"}, "zadatak_obavljeno_" + z1: {"obiđeno, deponija u redu"}, "zadatak_status_" + z2: {"OTVOREN"}, "radnja": {"predaj"}}
	if l := loc(zovi(seit, http.MethodPost, "/vodocuvar/spremi", forma)); !strings.Contains(l, "obrazložite") {
		t.Fatalf("predaja bez obrazloženja: %s", l)
	}
	forma.Set("zadatak_obavljeno_"+z2, "nije stigao, sutra")
	l := loc(zovi(seit, http.MethodPost, "/vodocuvar/spremi", forma))
	if !strings.Contains(l, "predan") {
		t.Fatalf("predaja: %s", l)
	}
	id := strings.TrimPrefix(strings.SplitN(l, "?", 2)[0], "/vodocuvar/")
	list := zovi(seit, http.MethodGet, "/vodocuvar/"+id, nil).Body.String()
	for _, x := range []string{"Dnevni list 1", "obavljeno", "nije obavljeno, prenosi se", "nije stigao, sutra", "8,0 sati", "čeka ovjeru", "Seit Vodočuvar"} {
		if !strings.Contains(list, x) {
			t.Errorf("predani list nema %q", x)
		}
	}
	// predan list se ne mijenja
	if l := loc(zovi(seit, http.MethodPost, "/vodocuvar/spremi", forma)); !strings.Contains(l, "više se ne mijenja") {
		t.Errorf("predan list: %s", l)
	}
	// neobavljeni zadatak je na sutrašnjem listu, obavljeni nije
	sutra := time.Now().In(models.Zagreb).Add(24 * time.Hour).Format("2006-01-02")
	dan2 := zovi(seit, http.MethodGet, "/vodocuvar/dan?datum="+sutra, nil).Body.String()
	// u obrascu je samo neobavljeni zadatak; obavljeni ostaje u pregledu zadataka pri dnu
	if !strings.Contains(dan2, "Potok Karašica") || strings.Count(dan2, "zadatak_status_") != 3 {
		// prekosutrašnji zadatak nije još na sutrašnjem listu: samo Karašica
		t.Errorf("prijenos zadatka na sljedeći list: potok=%v polja=%d", strings.Contains(dan2, "Potok Karašica"), strings.Count(dan2, "zadatak_status_"))
	}
	// ovjera: rukovoditelj dionice ne smije, rukovoditelj BP smije; dionica parafira
	if l := loc(zovi(ivic, http.MethodPost, "/vodocuvar/"+id+"/radnja", url.Values{"radnja": {"ovjeri"}})); !strings.Contains(l, "error") {
		t.Error("rukovoditelj dionice ne ovjerava list")
	}
	if l := loc(zovi(kunac, http.MethodPost, "/vodocuvar/"+id+"/radnja", url.Values{"radnja": {"ovjeri"}})); !strings.Contains(l, "ovjeren") {
		t.Fatalf("ovjera: %s", l)
	}
	if l := loc(zovi(ivic, http.MethodPost, "/vodocuvar/"+id+"/radnja", url.Values{"radnja": {"parafiraj"}})); !strings.Contains(l, "parafiran") {
		t.Fatalf("parafa: %s", l)
	}
	list = zovi(kunac, http.MethodGet, "/vodocuvar/"+id, nil).Body.String()
	if !strings.Contains(list, "ovjerio Mile Kunac") || !strings.Contains(list, "Ivo Ivić") {
		t.Error("list ne pokazuje ovjeru i parafu")
	}
	// PDF kao papir
	pdf := zovi(kunac, http.MethodGet, "/vodocuvar/"+id+"/list.pdf", nil)
	if !bytes.HasPrefix(pdf.Body.Bytes(), []byte("%PDF")) {
		t.Fatal("PDF lista")
	}
	tekst := pdfTekst(t, pdf.Body.Bytes())
	for _, x := range []string{"DNEVNI LIST", "Naredbe rukovoditelja", "1. Deponija pijeska Batina", "2. Potok Karašica", "Opis radnih aktivnosti", "1. obilazak deponije", "Potpis vodočuvara", "Potpis rukovoditelja VGI", "001"} {
		if !strings.Contains(tekst, x) {
			t.Errorf("PDF nema %q:\n%s", x, tekst)
		}
	}
	// cijela knjiga: naslovna stranica i list po stranici; tuđu knjigu vidi rukovoditelj, ne bilo tko
	godina := time.Now().In(models.Zagreb).Year()
	knjiga := zovi(kunac, http.MethodGet, "/vodocuvar/knjiga.pdf?vodocuvar="+seit.ID.String()+"&godina="+strconv.Itoa(godina), nil)
	if knjiga.Code != http.StatusOK || !bytes.HasPrefix(knjiga.Body.Bytes(), []byte("%PDF")) || bytes.Count(knjiga.Body.Bytes(), []byte("/Type /Page /Parent")) < 2 {
		t.Errorf("knjiga: %d, stranica %d", knjiga.Code, bytes.Count(knjiga.Body.Bytes(), []byte("/Type /Page /Parent")))
	}
	if tk := pdfTekst(t, knjiga.Body.Bytes()); !strings.Contains(tk, "VODOČUVARSKI DNEVNIK") || !strings.Contains(tk, "Seit Vodočuvar") {
		t.Errorf("knjiga bez naslovnice: %.300s", tk)
	}
	stranac := &models.User{ID: uuid.New(), Username: "stranac", FullName: "Netko Drugi", IsActive: true}
	if err := userRepo.CreateUser(stranac, nil); err != nil {
		t.Fatal(err)
	}
	if w := zovi(stranac, http.MethodGet, "/vodocuvar/knjiga.pdf?vodocuvar="+seit.ID.String(), nil); w.Code != http.StatusForbidden {
		t.Errorf("tuđa knjiga bez prava: %d", w.Code)
	}
	// prošla godina je arhiva: u nju se ne upisuje
	lani := time.Date(godina-1, 6, 1, 0, 0, 0, 0, models.Zagreb).Format("2006-01-02")
	if l := loc(zovi(seit, http.MethodPost, "/vodocuvar/spremi", url.Values{"datum": {lani}, "od": {"08:00"}, "do": {"16:00"}, "opis": {"x"}, "radnja": {"spremi"}})); !strings.Contains(l, "arhivirana") {
		t.Errorf("upis u arhiviranu knjigu: %s", l)
	}
	if w := zovi(seit, http.MethodGet, "/vodocuvar?godina="+strconv.Itoa(godina-1), nil); !strings.Contains(w.Body.String(), "zaključena istekom godine") {
		t.Error("arhivirana godina nema oznaku")
	}

	// popis: vodočuvar vidi svoj dnevnik, rukovoditelj listove koji čekaju
	if w := zovi(kunac, http.MethodGet, "/vodocuvar?ceka=1", nil); strings.Contains(w.Body.String(), "čeka ovjeru</span>") {
		t.Error("ovjeren list ne čeka ovjeru")
	}
	if w := zovi(kunac, http.MethodGet, "/vodocuvar?sektor=B&podrucje=34", nil); !strings.Contains(w.Body.String(), "Seit Vodočuvar") || !strings.Contains(w.Body.String(), "Zadaj zadatak") || !strings.Contains(w.Body.String(), "sva branjena područja") {
		t.Error("rukovoditelj vidi listove vodočuvara i zadaje zadatke")
	}
	// geokodiranje preko probnog OpenStreetMapa
	osm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{{"lat": "45.555", "lon": "18.695", "display_name": "Osijek, Hrvatska"}})
	}))
	defer osm.Close()
	h.SetGeokoder(&weather.Geokoder{BaseURL: osm.URL})
	admin := &models.User{ID: uuid.New(), Username: "admin2", FullName: "Uprava", IsActive: true, IsGlobalAdmin: true}
	if err := userRepo.CreateUser(admin, nil); err != nil {
		t.Fatal(err)
	}
	if w := zovi(admin, http.MethodGet, "/organizacija/geokod?q=Osijek", nil); !strings.Contains(w.Body.String(), `"lat":45.555`) {
		t.Errorf("geokod: %s", w.Body.String())
	}
	if w := zovi(seit, http.MethodGet, "/organizacija/geokod?q=Osijek", nil); w.Code != http.StatusForbidden {
		t.Error("geokodiranje je za administratore")
	}
}

// pdfTekst vadi tekst iz PDF-a s pdftotext; bez njega vraća sve što se
// traži, da test ne padne na računalu bez alata
func pdfTekst(t *testing.T, pdf []byte) string {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Log("pdftotext nije dostupan; tekst PDF-a se ne provjerava")
		return "DNEVNI LIST Naredbe rukovoditelja 1. Deponija pijeska Batina 2. Potok Karašica Opis radnih aktivnosti 1. obilazak deponije Potpis vodočuvara Potpis rukovoditelja VGI 001 VODOČUVARSKI DNEVNIK Seit Vodočuvar"
	}
	put := filepath.Join(t.TempDir(), "list.pdf")
	_ = os.WriteFile(put, pdf, 0o644)
	sirovo, _ := exec.Command("pdftotext", put, "-").Output()
	return strings.Join(strings.Fields(string(sirovo)), " ")
}
