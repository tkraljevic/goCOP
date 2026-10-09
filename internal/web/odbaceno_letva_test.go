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
	"gocop/internal/javnivodostaji"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Odluke (8. 10. 2026.), pitanje 21: zaglavljeno mjerilo (−1102 cm) se ne
// upisuje, nego se broji i pokazuje na stranici letve; letva akumulacije nosi
// oznaku „kota u cm n. m.”.
func TestStranicaLetvePokazujeOdbaceneVrijednosti(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "odbaceno.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
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

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	tablica := `<table><tr><th>DATUM</th><th>VRIJEME</th><th>VODOSTAJ</th><th>TREND</th></tr>
<tr> <td>18.09.2026.</td> <td>03:00 h</td> <td>-1102 cm</td> <td>0</td> </tr>
<tr> <td>18.09.2026.</td> <td>02:00 h</td> <td>-121 cm</td> <td>0</td> </tr></table>`
	uvoznik := javnivodostaji.NoviUvoznik(repository.NewJavniSpremiste(baza, readingRepo), nil)
	uvoznik.Client.HTTP = &http.Client{Transport: javniPrijenos{tablica}}

	h := NewReadingsHandler(readings, stations, service.NewStructureService(structureRepo), users, tmpl("readings.html"), tmpl("reading_history.html"), tmpl("reading_form.html"))
	h.SetJavniUvoz(func() *javnivodostaji.Uvoznik { return uvoznik })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readings/station/{id}", h.ShowHistory)
	mux.HandleFunc("POST /readings/station/{id}/javni", h.HandlePreuzmiJavno)

	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "pivnica-probna", Name: "Pivnica Probna",
		JavniURL: "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424", JavniUvoz: true}
	akumulacija := &models.Station{ID: uuid.New(), Code: "akumulacija-probna", Name: "Akumulacija Probna", KotaNM: true}
	for _, s := range []*models.Station{st, akumulacija} {
		if err := stationRepo.CreateStation(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	admin := &models.User{ID: uuid.New(), Username: "uprava", FullName: "Uprava", IsGlobalAdmin: true, IsActive: true}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *admin}
	zovi := func(metoda, putanja string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(metoda, putanja, nil)
		c := context.WithValue(r.Context(), contextKeyUser, admin)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	putanja := "/readings/station/" + st.ID.String()

	if w := zovi(http.MethodGet, putanja); strings.Contains(w.Body.String(), "Odbačeno izvan raspona") {
		t.Fatal("letva bez odbačenih ne smije pokazivati upozorenje")
	}
	w := zovi(http.MethodPost, putanja+"/javni")
	poruka, _ := url.QueryUnescape(w.Header().Get("Location"))
	if !strings.Contains(poruka, "novih 1, odbačeno izvan raspona 1.") {
		t.Fatalf("poruka preuzimanja: %s", poruka)
	}
	tijelo := zovi(http.MethodGet, putanja).Body.String()
	if !strings.Contains(tijelo, "Odbačeno izvan raspona: 1, zadnje −1102 cm 18. 9. 2026. u 03 h (vodostaji.voda.hr)") ||
		!strings.Contains(tijelo, "odbačeno izvan raspona 1,") {
		t.Errorf("stranica letve ne pokazuje odbačeno:\n%s", tijelo)
	}
	// broj nije samo ovog računala: zapis putuje razmjenom (dorada)
	if !strings.Contains(tijelo, "zapis o njima putuje razmjenom") {
		t.Error("stranica letve ne kaže da se broje i odbačene na računalu koje preuzima")
	}
	if strings.Contains(tijelo, "kota u cm n. m.") {
		t.Error("obična letva nema oznaku kote")
	}
	if tijelo := zovi(http.MethodGet, "/readings/station/"+akumulacija.ID.String()).Body.String(); !strings.Contains(tijelo, "kota u cm n. m.") {
		t.Error("letva akumulacije nema oznaku kote")
	}
}

func TestPorukaPreuzimanja(t *testing.T) {
	if p := porukaPreuzimanja(javnivodostaji.StanjeLetve{Preuzeto: 3, Novih: 2}); p != "Preuzeto 3 očitanja s javne stranice, novih 2." {
		t.Errorf("bez odbačenih: %q", p)
	}
	if p := porukaPreuzimanja(javnivodostaji.StanjeLetve{Preuzeto: 3, Novih: 1, Odbaceno: 2}); p != "Preuzeto 3 očitanja s javne stranice, novih 1, odbačeno izvan raspona 2." {
		t.Errorf("s odbačenima: %q", p)
	}
}

// Oznaka „kota u cm n. m.” upisuje se kvačicom u obrascu letve, i skida
func TestOznakaKoteIzObrascaLetve(t *testing.T) {
	var st models.Station
	stationForm{KotaNM: "1"}.primijeni(&st)
	if !st.KotaNM {
		t.Error("kvačica nije upisala oznaku")
	}
	stationForm{}.primijeni(&st)
	if st.KotaNM {
		t.Error("prazna kvačica nije skinula oznaku")
	}
	// historijat ne nosi kvačicu pa je ne dira
	st.KotaNM = true
	stationForm{Obrazac: obrazacHistorijat}.primijeni(&st)
	if !st.KotaNM {
		t.Error("obrazac historijata skinuo je oznaku")
	}
	for v, ocekivano := range map[string]bool{"1": true, "on": true, "true": true, "": false, "0": false, "da": false} {
		if kvacica(v) != ocekivano {
			t.Errorf("%q: očekivano %v", v, ocekivano)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/stations", strings.NewReader("kota_nm=1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if f, err := decodeStationForm(r); err != nil || f.KotaNM != "1" {
		t.Errorf("obrazac: %+v, %v", f.KotaNM, err)
	}
}

// Kartica letve u registru nosi oznaku „kota u cm n. m.”, kao i stranica
// očitanja; obična letva je nema
func TestKarticaLetveNosiOznakuKote(t *testing.T) {
	for _, kota := range []bool{true, false} {
		st := models.Station{ID: uuid.New(), Code: "akumulacija-probna", Name: "Akumulacija Probna", KotaNM: kota}
		html := iscrtaj(t, "station_detail.html", StationPageData{
			CurrentUser: &models.User{FullName: "Pero Perić"},
			Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Station:     st,
		})
		if strings.Contains(html, ">kota u cm n. m.</span>") != kota {
			t.Errorf("letva s oznakom %v: oznaka na kartici %v", kota, !kota)
		}
	}
}

// Pomoć opisuje zajednički raspon, oznaku kote i upozorenje na stranici letve
func TestPomocOpisujeRasponOcitanja(t *testing.T) {
	h := pomocHTML(t)
	for _, s := range []string{`id="ocitanja-raspon"`, "vodostaj od −500 do 3000 cm", "satno preuzimanje",
		"evidencije BP16", "kota u cm n. m.", "5000 do 40 000 cm", "Odbačeno izvan raspona: 50",
		"putuje razmjenom", "starijom verzijom programa (0.0.34) raspon ne provjerava"} {
		if !strings.Contains(h, s) {
			t.Errorf("pomoć ne spominje %q", s)
		}
	}
	if strings.Contains(h, "na ostalima se ne vidi") {
		t.Error("pomoć još kaže da se broj vidi samo na računalu koje je vrijednost odbacilo")
	}
}
