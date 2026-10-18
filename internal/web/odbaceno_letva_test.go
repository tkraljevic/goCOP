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
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "odbaceno.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
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
	if p := porukaPreuzimanja(javnivodostaji.StanjeLetve{Preuzeto: 3, Novih: 2}); p != "Preuzeto s javne stranice: 3 očitanja, novih 2." {
		t.Errorf("bez odbačenih: %q", p)
	}
	if p := porukaPreuzimanja(javnivodostaji.StanjeLetve{Preuzeto: 3, Novih: 1, Odbaceno: 2}); p != "Preuzeto s javne stranice: 3 očitanja, novih 1, odbačeno izvan raspona 2." {
		t.Errorf("s odbačenima: %q", p)
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): oznaku „kota u cm n. m.”
// imaju tri letve akumulacija HEP-a i nju upisuje popravak podataka. Obrazac
// letve je pokazuje, ali je ne mijenja: kvačica je na novoj letvi bila
// bačena (toStation je nije prenosio), a na postojećoj ju je svaki urednik
// dionice mogao skinuti ili staviti — i JSON klijent koji polje ne pošalje
// skidao ju je — pa bi se razina akumulacije odbacivala na svim putovima,
// ili obična letva odbacivala svaki vodostaj.
func TestObrazacLetveNeMijenjaOznakuKote(t *testing.T) {
	akumulacija := models.Station{ID: uuid.New(), Code: "gvb-he-varazdin", Name: "Akumulacija Probna", KotaNM: true}
	stationForm{Name: "Akumulacija Probna"}.primijeni(&akumulacija)
	if !akumulacija.KotaNM {
		t.Error("kartica bez polja oznake skinula je oznaku akumulacije")
	}

	zovi := func(vrsta, tijelo string) stationForm {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/stations/create", strings.NewReader(tijelo))
		r.Header.Set("Content-Type", vrsta)
		f, err := decodeStationForm(r)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	for _, f := range []stationForm{
		zovi("application/x-www-form-urlencoded", "name=Letva+Probna&code=letva-probna&kota_nm=1"),
		zovi("application/json", `{"name":"Letva Probna","code":"letva-probna","kota_nm":"1"}`),
	} {
		obicna := models.Station{Name: "Letva Probna"}
		f.primijeni(&obicna)
		if obicna.KotaNM || f.toStation().KotaNM {
			t.Errorf("obrazac je upisao oznaku kote: %+v", f)
		}
	}

	nova := iscrtaj(t, "station_form.html", StationPageData{CurrentUser: &models.User{FullName: "Pero Perić"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true}})
	uredjivanje := iscrtaj(t, "station_form.html", StationPageData{CurrentUser: &models.User{FullName: "Pero Perić"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Station: akumulacija, IsEdit: true})
	for ime, html := range map[string]string{"nova letva": nova, "letva akumulacije": uredjivanje} {
		if strings.Contains(html, `name="kota_nm"`) {
			t.Errorf("%s: obrazac nudi polje oznake kote", ime)
		}
	}
	if strings.Contains(nova, "kota u cm n. m.") {
		t.Error("obrazac nove letve spominje oznaku kote")
	}
	if !strings.Contains(uredjivanje, "kota u cm n. m.") || !strings.Contains(uredjivanje, "5000 do 40 000 cm") {
		t.Error("obrazac letve akumulacije ne pokazuje oznaku i raspon")
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
		"putuje razmjenom", "starijom verzijom programa (0.0.34) raspon ne provjerava",
		"Oznaku postavlja program; u obrascu letve se ne mijenja", "greška u zapisu", "ne poništava"} {
		if !strings.Contains(h, s) {
			t.Errorf("pomoć ne spominje %q", s)
		}
	}
	if strings.Contains(h, "U obrascu letve ima oznaku") {
		t.Error("pomoć još kaže da se oznaka kote stavlja u obrascu letve")
	}
	if strings.Contains(h, "na ostalima se ne vidi") {
		t.Error("pomoć još kaže da se broj vidi samo na računalu koje je vrijednost odbacilo")
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): odbačeno iz uvoza starih
// evidencija (21500 umjesto 215 cm na crpnoj stanici) je greška u zapisu, a
// ne kvar mjerila, i broj se ne poništava. Stranica zato ne tvrdi da je
// mjerilo u kvaru, nego kaže oba uzroka i datum zadnje vrijednosti, po kojem
// se vidi traje li to još.
func TestOdbacenoIzStareEvidencijeNijeKvarMjerila(t *testing.T) {
	kad := time.Date(1987, 5, 3, 7, 0, 0, 0, models.Zagreb)
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "Pero Perić"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Structure:   &models.Structure{ID: uuid.New(), Name: "CS Probna"},
		GaugeName:   "CS Probna",
		Odbaceno: &models.SazetakOdbacenih{Broj: 1, Zadnja: models.OdbacenaVrijednost{
			Kad: kad, Polje: models.PoljeVodostaj, Opis: "21500 cm", Izvor: models.ReadingOriginBP16}},
	})
	if !strings.Contains(html, "Odbačeno izvan raspona: 1, zadnje 21500 cm 3. 5. 1987. u 07 h (evidencija VGI Baranja)") {
		t.Errorf("upozorenje ne navodi datum zadnje vrijednosti:\n%s", html)
	}
	if strings.Contains(html, "jer je to znak da je mjerilo vjerojatno u kvaru") {
		t.Error("stranica tvrdi da je mjerilo u kvaru i za grešku u staroj evidenciji")
	}
	for _, s := range []string{"greška u zapisu", "mjerilo u kvaru", "datum zadnje"} {
		if !strings.Contains(html, s) {
			t.Errorf("stranica ne spominje %q", s)
		}
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): preuzimanje i uvoz uz
// vodostaj izvan raspona ne upisuju cijelo očitanje, kao ručni upis, pa sat
// ostaje slobodan; uz temperaturu ili protok izvan raspona otpada samo ta
// vrijednost. Pomoć to kaže, i da se upisano očitanje poslije ne
// dopunjuje, a ne smije reći da tipfeler u vodostaju ostavlja sate rada
// agregata.
func TestPomocKazeKadOtpadaCijeloOcitanje(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	for _, s := range []string{"ni tipfeler u vodostaju", "otpada vrijednost izvan raspona, a ostale vrijednosti istog očitanja ostaju"} {
		if strings.Contains(h, s) {
			t.Errorf("pomoć još kaže %q", s)
		}
	}
	for _, s := range []string{"ne upisuje se cijelo očitanje", "protok koji izvor računa iz vodostaja",
		"Sat tako ostaje slobodan", "otpada samo ta vrijednost", "Očitanje kojem ništa ne ostane ne upisuje se", "ne dopunjuju"} {
		if !strings.Contains(h, s) {
			t.Errorf("pomoć ne spominje %q", s)
		}
	}
}

// Stranica letve uz broj odbačenih kaže kad otpada cijelo očitanje, a kad
// samo vrijednost izvan raspona (dorada, Odluke (8. 10. 2026.), pitanje 21)
func TestStranicaLetveKazeKadOtpadaCijeloOcitanje(t *testing.T) {
	html := strings.Join(strings.Fields(iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "Pero Perić"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Pivnica Probna"},
		GaugeName:   "Pivnica Probna",
		Odbaceno: &models.SazetakOdbacenih{Broj: 1, Zadnja: models.OdbacenaVrijednost{
			Kad: time.Now(), Polje: models.PoljeTemperatura, Opis: "temperatura 85,0 °C", Izvor: "hdv.voda.hr"}},
	})), " ")
	for _, s := range []string{"uz vodostaj izvan raspona ne upisuje se cijelo očitanje",
		"uz temperaturu ili protok izvan raspona ostale vrijednosti istog očitanja upisuju se"} {
		if !strings.Contains(html, s) {
			t.Errorf("stranica letve ne kaže %q", s)
		}
	}
}

// Dorada (Odluke (8. 10. 2026.), pitanje 21): stranica letve pokazuje broj
// odbačenih i zadnju odbačenu vrijednost, s danom i satom; popisa svih nema
// ni na jednoj stranici. Pomoć ne smije obećavati da se svaki odbačeni zapis
// (npr. 40 tipfelera iz BP16) može naći i ispraviti.
func TestPomocNeObecavaPopisOdbacenih(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	if strings.Contains(h, "pa se zapis može naći i ispraviti") {
		t.Error("pomoć još obećava da se svaki odbačeni zapis može naći")
	}
	for _, s := range []string{"samo broj i zadnju odbačenu vrijednost", "Popisa svih odbačenih vrijednosti nema"} {
		if !strings.Contains(h, s) {
			t.Errorf("pomoć ne kaže %q", s)
		}
	}
}
