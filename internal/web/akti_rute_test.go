package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
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

// Akt se sastavi po vodomjeru: dionice, vodostaj i tendencija, potpisnik i
// primatelji dođu sami; ovjera dodijeli broj i kod, proglasi obranu na
// dionicama i akt se više ne briše; PDF nosi tekst akta.
func TestAktOdVodomjeraDoOvjereKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akti.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, email) VALUES ('B', 'Sektor B', 'VGO za Dunav i donju Dravu', 'COP Osijek', 'Splavarska 2a, Osijek', 'copos@voda.hr')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'međudržavne rijeke Drava i Dunav', 'COP', 'Osijek')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'd.o. r. Dunav, rkm 1433+060 – 1421+770 (državna granica – Zeleni otok)', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.2', 34, 'B', 'd.o. r. Dunav, rkm 1421+770 – 1403+000 (Zeleni otok – Ludaš)', '2026-01-01', '2026-01-01')`,
		`INSERT INTO counties (id, code, name, seat, email) VALUES (14, 'OB', 'Osječko-baranjska županija', 'Osijek', 'zupan@obz.hr')`,
		`INSERT INTO municipalities (id, county_id, name, type) VALUES (301, 14, 'Draž', 'OPCINA'), (302, 14, 'Beli Manastir', 'GRAD')`,
		`INSERT INTO section_territories (id, section_code, county_id, municipality_id, created_at) VALUES ('t1', 'B.34.1', 14, 301, '2026-01-01')`,
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
	aktiRepo := repository.NewAktiRepository(baza, rec)
	akti := service.NewAktService(aktiRepo, stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users, episodes, "cop-osijek")
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())

	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "batina", Name: "Batina", Watercourse: "Dunav"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"B.34.1", "B.34.2"} {
		if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`, uuid.NewString(), code, st.ID.String(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// dva očitanja: 640 pa 652, tendencija porasta
	for i, cm := range []int{640, 652} {
		v := cm
		if err := readingRepo.Create(ctx, &models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: time.Date(2026, 9, 15, 10+i, 0, 0, 0, time.UTC), LevelCm: &v, Source: models.ReadingSourceManual}); err != nil {
			t.Fatal(err)
		}
	}
	// službe uz županiju: civilna zaštita s podstavkom 112, policija; postaja
	// u gradu koji nije ugroženo područje ne ide na akt
	teritorij := repository.NewTerritoryRepository(baza, rec)
	for _, x := range []models.Sluzba{
		{CountyID: 14, Vrsta: models.SluzbaCentar112, Naziv: "Županijski centar 112 Osijek", Email: "osijek112@mup.hr"},
		{CountyID: 14, Vrsta: models.SluzbaCivilnaZastita, Naziv: "Područni ured civilne zaštite Osijek", Email: "cz@mup.hr"},
		{CountyID: 14, Vrsta: models.SluzbaPolicija, Naziv: "PU Osječko-baranjska", Email: "pu@mup.hr"},
		{CountyID: 14, MunicipalityID: 302, Vrsta: models.SluzbaPolicijskaPost, Naziv: "PP Beli Manastir"},
		{CountyID: 14, Vrsta: models.SluzbaVatrogasci, Naziv: "Vatrogasna zajednica OBŽ"},
		{CountyID: 14, Vrsta: models.SluzbaStozerCZ, Naziv: "Stožer civilne zaštite OBŽ"},
	} {
		x := x
		if err := service.NewTerritoryService(teritorij, sections).SpremiSluzbu(ctx, &models.UserPermissions{IsGlobalAdmin: true}, &x); err != nil {
			t.Fatal(err)
		}
	}

	// registar primatelja: jedan uvijek, jedan od izvanrednog stanja
	admin := &models.User{ID: uuid.New(), Username: "uprava", FullName: "Uprava Sektora", IsGlobalAdmin: true, IsActive: true}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *admin}
	for _, p := range []models.Primatelj{
		{Sektor: "B", Naziv: "Glavni centar obrane od poplava Zagreb", Email: "GCOPRH@voda.hr", Skupina: models.SkupinaUprava, Aktivan: true, Redoslijed: 1},
		{Sektor: "B", Naziv: "Župan osječko-baranjski", Skupina: models.SkupinaSamouprava, OdStupnja: models.PhaseState, Aktivan: true},
	} {
		p := p
		if err := akti.SpremiPrimatelja(ctx, perms, &p); err != nil {
			t.Fatal(err)
		}
	}

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
	h.SetSpranca(tmpl("spranca.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /akti/spranca", h.ShowSpranca)
	mux.HandleFunc("POST /akti/spranca", h.HandleSpranca)
	mux.HandleFunc("POST /akti/{id}/tekst", h.HandleTekst)
	mux.HandleFunc("GET /akti", h.ShowPopis)
	mux.HandleFunc("GET /akti/novi", h.ShowForm)
	mux.HandleFunc("POST /akti/novi", h.HandleCreate)
	mux.HandleFunc("GET /akti/primatelji", h.ShowPrimatelji)
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	mux.HandleFunc("GET /akti/{id}/akt.pdf", h.IzvoziPDF)
	mux.HandleFunc("POST /akti/{id}/ovjeri", h.HandleOvjeri)
	mux.HandleFunc("POST /akti/{id}/obrisi", h.HandleObrisi)

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

	mora(zovi(http.MethodGet, "/akti", nil), "prazan popis", "Nema akata", "Novi akt")
	mora(zovi(http.MethodGet, "/akti/novi", nil), "izbor vodomjera", "Batina", "B.34.1, B.34.2")
	mora(zovi(http.MethodGet, "/akti/novi?station="+st.ID.String()+"&stupanj=IZVANREDNA", nil), "obrazac", "Akt po vodomjeru Batina", "652 cm", "640 cm", `name="ocitanje_id"`, `name="tendencija"`, `value="B.34.1" checked`, `value="B.34.2" checked`)
	mora(zovi(http.MethodGet, "/akti/spranca?sektor=B", nil), "špranca", "Pravna osnova", "XXIII", "N.N. br. 84/10", "Vrati zadano")

	// špranca: izdanje Glavnog provedbenog plana iz 2025.
	w0 := zovi(http.MethodPost, "/akti/spranca", url.Values{"sektor": {"B"},
		"osnova":         {"Na temelju Zakona o vodama, članak 130. (N.N. br. 66/19, 84/21 i 47/23) te odredbi članka {clanak} Državnog plana obrane od poplava (N.N. br. 84/10) i Glavnog provedbenog plana obrane od poplava (Hrvatske vode, ožujak 2025.),"},
		"zavrsno":        {"Za vrijeme provođenja mjera treba postupiti prema Glavnom provedbenom planu (ožujak 2025.)!"},
		"clanak_REDOVNA": {"XXIII"}})
	if !strings.Contains(w0.Header().Get("Location"), "success") {
		t.Fatalf("špranca: %s", w0.Header().Get("Location"))
	}
	mora(zovi(http.MethodGet, "/akti/primatelji?sektor=B", nil), "primatelji", "Glavni centar obrane od poplava Zagreb", "Župan osječko-baranjski", "Izvanredno stanje")

	// nacrt rješenja o izvanrednoj obrani
	sva, _ := akti.OcitanjaZaAkt(ctx, st.ID.String(), 0)
	if len(sva) != 2 || *sva[0].LevelCm != 652 {
		t.Fatalf("očitanja za akt: %+v", sva)
	}
	// prvo sastavi po starijem očitanju (640) i tendenciji po izboru, pa obriši
	w := zovi(http.MethodPost, "/akti/novi", url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"IZVANREDNA"}, "vrijedi": {"2026-09-15T12:00"}, "ocitanje_id": {sva[1].ID.String()}, "tendencija": {models.TendencijaNagliPorast}})
	stari := strings.TrimPrefix(strings.SplitN(w.Header().Get("Location"), "?", 2)[0], "/akti/")
	if a, _ := akti.Get(ctx, stari); a == nil || *a.VodostajCm != 640 || a.Tendencija != models.TendencijaNagliPorast || !strings.Contains(a.Uvod, "od 640 cm u") || !strings.Contains(a.Uvod, "naglog porasta") {
		t.Fatalf("nacrt po odabranom očitanju: %+v", a)
	}
	zovi(http.MethodPost, "/akti/"+stari+"/obrisi", url.Values{})

	w = zovi(http.MethodPost, "/akti/novi", url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"IZVANREDNA"}, "vrijedi": {"2026-09-15T12:00"}, "dionica": {"B.34.1", "B.34.2"}})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "/akti/") {
		t.Fatalf("nacrt: %d %s", w.Code, w.Header().Get("Location"))
	}
	putanja := strings.SplitN(w.Header().Get("Location"), "?", 2)[0]
	id := strings.TrimPrefix(putanja, "/akti/")
	a, _ := akti.Get(ctx, id)
	if a == nil || a.Sektor != "B" || a.AreaID != 34 || len(a.Dionice) != 2 || a.VodostajCm == nil || *a.VodostajCm != 652 || a.Tendencija != models.TendencijaPorast {
		t.Fatalf("nacrt nije sastavljen iz vodomjera: %+v", a)
	}
	if a.Vrsta() != "RJEŠENJE" || a.Clanak() != "XXIV" || !strings.HasPrefix(a.Potpisnik, "Rukovoditelj obrane od poplava Sektora B") {
		t.Errorf("vrsta, članak ili potpisnik krivi: %s %s %s", a.Vrsta(), a.Clanak(), a.Potpisnik)
	}
	imena := []string{}
	for _, p := range a.Primatelji {
		imena = append(imena, p.Naziv)
	}
	if !strings.Contains(strings.Join(imena, ";"), "Glavni centar") || strings.Contains(strings.Join(imena, ";"), "Župan osječko") || imena[len(imena)-1] != "Pismohrana" {
		t.Errorf("primatelji: %v", imena)
	}
	// službe županije ugroženog područja, civilna zaštita prije 112 koji je podstavka
	spojeno := strings.Join(imena, ";")
	if !strings.Contains(spojeno, "Područni ured civilne zaštite Osijek;– Županijski centar 112 Osijek;PU Osječko-baranjska") {
		t.Errorf("službe županije nisu na aktu kako treba: %v", imena)
	}
	if strings.Contains(spojeno, "PP Beli Manastir") {
		t.Error("postaja u gradu koji nije ugroženo područje ne ide na akt")
	}
	if strings.Contains(spojeno, "Vatrogasna zajednica") || strings.Contains(spojeno, "Stožer") {
		t.Error("vatrogasci ne idu na akt, a stožer tek od izvanrednog stanja")
	}
	mora(zovi(http.MethodGet, putanja, nil), "nacrt", "NACRT", "RJEŠENJE", "izvanredne obrane od poplava", "652 cm", "s tendencijom daljnjeg porasta", "B.34.1", "Zeleni otok", "Ovjeri", "Obriši nacrt", "ožujak 2025.", "članka XXIV", "Ispravi tekst nacrta")
	w = zovi(http.MethodPost, putanja+"/tekst", url.Values{"uvod": {a.Uvod + " i sukladno procjeni visokog stupnja ugroženosti,"}, "zavrsno": {a.Zavrsno}, "napomena": {"Probna napomena."}})
	if !strings.Contains(w.Header().Get("Location"), "success") {
		t.Fatalf("ispravak nacrta: %s", w.Header().Get("Location"))
	}
	a, _ = akti.Get(ctx, id)
	if !strings.Contains(a.Uvod, "procjeni visokog stupnja ugroženosti") || a.Napomena != "Probna napomena." {
		t.Fatalf("ispravak nije spremljen: %+v", a)
	}

	// ovjera: broj, kod, epizode na dionicama
	w = zovi(http.MethodPost, putanja+"/ovjeri", url.Values{})
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "ovjeren") {
		t.Fatalf("ovjera: %d %s", w.Code, w.Header().Get("Location"))
	}
	a, _ = akti.Get(ctx, id)
	if !a.Ovjeren() || a.Broj != 1 || a.Oznaka() != "B-1/2026" || a.OvjeraKod == "" || a.Ovjerio != "Uprava Sektora" {
		t.Fatalf("ovjera nije upisana: %+v", a)
	}
	// uprava bez zaduženja rukovoditelja sektora potpisuje u zamjeni
	if !a.UZamjeni || a.ImePotpisa() != "u.z. Uprava Sektora" {
		t.Errorf("ovjera bez nositelja funkcije bi trebala biti u zamjeni: %+v", a)
	}
	sektor := "B"
	nositelj := models.Akt{Sektor: "B", AreaID: 34, Stupanj: models.PhaseEmergency}
	if !nositelj.NositeljFunkcije([]models.Duty{{Role: models.RoleSectorLeader, SectorID: &sektor, IsActive: true}}) {
		t.Error("rukovoditelj sektora je nositelj funkcije za izvanrednu obranu")
	}
	if nositelj.NositeljFunkcije([]models.Duty{{Role: models.RoleSectorDeputy, SectorID: &sektor, IsActive: true}}) {
		t.Error("zamjenik rukovoditelja sektora potpisuje u zamjeni")
	}
	for _, code := range []string{"B.34.1", "B.34.2"} {
		e, _ := episodes.Open(ctx, code)
		if e == nil || e.Phase != models.PhaseEmergency {
			t.Errorf("obrana na %s nije proglašena ovjerom: %+v", code, e)
		}
	}
	mora(zovi(http.MethodGet, putanja, nil), "ovjeren", "ovjeren B-1/2026", a.OvjeraKod, "Uprava Sektora")
	if w := zovi(http.MethodPost, putanja+"/obrisi", url.Values{}); !strings.Contains(w.Header().Get("Location"), "error") {
		t.Error("ovjeren akt bi trebao odbiti brisanje")
	}
	if w := zovi(http.MethodPost, putanja+"/ovjeri", url.Values{}); !strings.Contains(w.Header().Get("Location"), "error") {
		t.Error("dvostruka ovjera bi trebala biti odbijena")
	}
	if w := zovi(http.MethodPost, putanja+"/tekst", url.Values{"uvod": {"x"}, "zavrsno": {"y"}}); !strings.Contains(w.Header().Get("Location"), "error") {
		t.Error("ovjeren akt ne smije se ispravljati")
	}

	// popis i pretraga
	mora(zovi(http.MethodGet, "/akti?q=Batina&stupanj=IZVANREDNA", nil), "popis", "B-1/2026", "Uprava Sektora", "B.34.1, B.34.2")
	mora(zovi(http.MethodGet, "/akti?q=nepostojeće", nil), "prazna pretraga", "Nema akata")

	// PDF
	w = zovi(http.MethodGet, putanja+"/akt.pdf", nil)
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "%PDF") || !strings.Contains(w.Header().Get("Content-Disposition"), "batina-uspostava-io-2026-09-15.pdf") {
		t.Fatalf("PDF: %d %s", w.Code, w.Header().Get("Content-Disposition"))
	}
	if _, err := exec.LookPath("pdftotext"); err == nil {
		put := filepath.Join(t.TempDir(), "akt.pdf")
		_ = os.WriteFile(put, w.Body.Bytes(), 0o644)
		sirovo, _ := exec.Command("pdftotext", put, "-").Output()
		// prijelom retka u PDF-u nije razlika u tekstu
		out := []byte(strings.Join(strings.Fields(string(sirovo)), " "))
		for _, zeli := range []string{"RJEŠENJE", "izvanredne obrane od poplava", "vodomjeru Batina", "652 cm", "B.34.2", "15.09.2026.", "12:00", "Rukovoditelj obrane od poplava Sektora B", "Glavni centar", "Pismohrana", "u.z. Uprava Sektora", "procjeni visokog stupnja ugroženosti", "ožujak 2025.", "O tome obavijest:", "GCOPRH@voda.hr", a.OvjeraKod} {
			if !strings.Contains(string(out), zeli) {
				t.Errorf("u PDF-u nema %q:\n%s", zeli, out)
			}
		}
	}

	// prekid pripremnog stanja ide obaviješću po članku XXII i gasi obranu
	w = zovi(http.MethodPost, "/akti/novi", url.Values{"station_id": {st.ID.String()}, "radnja": {"PREKID"}, "stupanj": {"PRIPREMNO"}, "vrijedi": {"2026-09-16T07:00"}})
	id2 := strings.TrimPrefix(strings.SplitN(w.Header().Get("Location"), "?", 2)[0], "/akti/")
	b, _ := akti.Get(ctx, id2)
	if b == nil || b.Vrsta() != "OBAVIJEST" || b.Clanak() != "XXII" || b.Naslov() != "OBAVIJEST o prekidu pripremnog stanja obrane od poplava" {
		t.Fatalf("obavijest o prekidu: %+v", b)
	}
	if (models.Akt{Stupanj: models.PhaseRegular}).Clanak() != "XXIII" {
		t.Error("redovitu obranu uređuje članak XXIII Državnog plana")
	}
	zovi(http.MethodPost, "/akti/"+id2+"/ovjeri", url.Values{})
	if e, _ := episodes.Open(ctx, "B.34.1"); e != nil {
		t.Errorf("prekid pripremnog stanja bi trebao zatvoriti obranu, a traje: %+v", e)
	}
	b, _ = akti.Get(ctx, id2)
	if b.Broj != 2 {
		t.Errorf("drugi akt u godini bi trebao imati broj 2, ima %d", b.Broj)
	}
}
