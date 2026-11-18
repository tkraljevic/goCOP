package web

import (
	"context"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
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

type javniPrijenos struct{ html string }

func (p javniPrijenos) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(p.html)), Header: http.Header{}}, nil
}

// Letva povezana s javnom stranicom ima gumb za preuzimanje; preuzimanje
// upiše što još nema i stranica pokaže stanje zadnjeg preuzimanja
func TestPreuzimanjeSJavneStraniceKrozRute(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "javni.db"))
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
<tr> <td>18.09.2026.</td> <td>10:00 h</td> <td>-119 cm</td> <td>+2</td> </tr>
<tr> <td>18.09.2026.</td> <td>09:00 h</td> <td>-121 cm</td> <td>0</td> </tr></table>`
	uvoznik := javnivodostaji.NoviUvoznik(repository.NewJavniSpremiste(baza, readingRepo), nil)
	uvoznik.Client.HTTP = &http.Client{Transport: javniPrijenos{tablica}}

	h := NewReadingsHandler(readings, stations, service.NewStructureService(structureRepo), users, tmpl("readings.html"), tmpl("reading_history.html"), tmpl("reading_form.html"))
	h.SetJavniUvoz(func() *javnivodostaji.Uvoznik { return uvoznik })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readings/station/{id}", h.ShowHistory)
	mux.HandleFunc("POST /readings/station/{id}/javni", h.HandlePreuzmiJavno)

	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "batina", Name: "Batina", JavniURL: "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424", JavniUvoz: true}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
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

	w := zovi(http.MethodGet, putanja)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Preuzmi s vodostaji.voda.hr") || !strings.Contains(w.Body.String(), "Satno preuzimanje je uključeno") {
		t.Fatalf("stranica bez gumba ili stanja: %d\n%s", w.Code, w.Body.String())
	}
	w = zovi(http.MethodPost, putanja+"/javni")
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "novih") {
		t.Fatalf("preuzimanje: %d %s", w.Code, w.Header().Get("Location"))
	}
	popis, _ := readingRepo.List(ctx, repository.ReadingFilter{StationID: st.ID.String()})
	if len(popis) != 2 || popis[0].Origin != javnivodostaji.Podrijetlo || popis[0].LevelCm == nil || *popis[0].LevelCm != -119 {
		t.Fatalf("očitanja nakon preuzimanja: %+v", popis)
	}
	w = zovi(http.MethodGet, putanja)
	if !strings.Contains(w.Body.String(), "novih 2") || !strings.Contains(w.Body.String(), "vodostaji.voda.hr") {
		t.Errorf("stanje preuzimanja nije na stranici")
	}
	// drugi put ništa novo
	zovi(http.MethodPost, putanja+"/javni")
	if popis, _ = readingRepo.List(ctx, repository.ReadingFilter{StationID: st.ID.String()}); len(popis) != 2 {
		t.Errorf("ponovljeno preuzimanje udvostručilo očitanja: %d", len(popis))
	}

	// „Provjeri sada” s Telemetrije vraća se onamo, s imenom letve; druga
	// vrijednost polja natrag ne vodi nikamo drugdje
	for natrag, zeli := range map[string]string{"telemetrija": "/administracija/telemetrija?success=Batina%3A+Preuzeto", "https://zlo.primjer": putanja + "?success="} {
		r := httptest.NewRequest(http.MethodPost, putanja+"/javni", strings.NewReader("natrag="+natrag))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, admin), contextKeyPerms, perms)
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		if !strings.HasPrefix(w.Header().Get("Location"), zeli) {
			t.Errorf("natrag %q: %s", natrag, w.Header().Get("Location"))
		}
	}

	// Telemetrija: stupac Zadnji dohvat čita isto stanje preuzimanja
	if _, ima := (&Server{}).stanjePreuzimanja(st.ID.String()); ima {
		t.Error("bez uvoznika nema stanja preuzimanja")
	}
	if stanje, ima := (&Server{javni: uvoznik}).stanjePreuzimanja(st.ID.String()); !ima || stanje.Kad.IsZero() {
		t.Error("stanje preuzimanja Batine nije pročitano")
	}
	tel := &TelemetrijaHandler{postaje: stations, stanje: uvoznik.Stanje}
	hv := &models.Station{ID: uuid.New(), Code: "hv-letva", Name: "Radarska", JavniURL: "https://hdv.voda.hr/postaja/1"}
	if err := stationRepo.CreateStation(ctx, hv); err != nil {
		t.Fatal(err)
	}
	letve := tel.letve(ctx, nil)
	if len(letve) != 1 || letve[0].Naziv != "Radarska" || letve[0].DohvatZnan {
		t.Errorf("letve telemetrije: %+v", letve)
	}
	var b strings.Builder
	d := TelemetrijaPageData{CurrentUser: admin, Permissions: perms, Letve: []LetvaTelemetrije{
		{ID: "a", Naziv: "Radarska", Uvoz: true, DohvatZnan: true, Dohvat: javnivodostaji.StanjeLetve{Kad: time.Now(), Novih: 3}},
		{ID: "b", Naziv: "Tlačna", DohvatZnan: true, Dohvat: javnivodostaji.StanjeLetve{Kad: time.Now(), Greska: "prijava odbijena"}},
		{ID: "c", Naziv: "Nova"},
	}}
	if err := tmpl("administracija_telemetrija.html").ExecuteTemplate(&b, "base.html", d); err != nil {
		t.Fatal(err)
	}
	for _, mora := range []string{"novih 3", "nije uspjelo</span> <span class=\"muted\">prijava odbijena", "nije preuzimana od pokretanja programa", `action="/readings/station/a/javni"`, `name="natrag" value="telemetrija"`} {
		if !strings.Contains(b.String(), mora) {
			t.Errorf("Telemetrija nema %q", mora)
		}
	}
}

// Adresa se sprema kakva je zalijepljena; goli broj je stara navika s
// Hrvatskih voda i pretvara se u njihovu adresu. Sektor u njoj ostaje nula jer
// se iz golog broja ne zna — stranica tada ne radi, ali se očitanje uzme s
// popisa postaja. Prije je takva adresa išla na vodostaji.voda.hr, gdje ta
// putanja vraća 404 za svaku postaju, pa i za one koje inače rade.
func TestJavnaAdresaIzObrasca(t *testing.T) {
	for unos, zeli := range map[string]string{
		"https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424": "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424",
		"424": "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=0&bpID=0&postajaID=424",
		"https://www.hydroinfo.hu/Html/vizallas/mohacs.html": "https://www.hydroinfo.hu/Html/vizallas/mohacs.html",
		"  ": "",
	} {
		adresa, uvoz := stationForm{JavniURL: unos, JavniUvoz: "1"}.javnaVeza()
		if adresa != zeli || uvoz != (zeli != "") {
			t.Errorf("%q: %q (uvoz %v), očekivano %q", unos, adresa, uvoz, zeli)
		}
	}
}
