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

// Sredstva kroz rute, kao uprava sektora: skladište se upiše, primka i
// punjenje prođu obrascem, kartica pokazuje stanje i knjigu, popis se
// otvori s knjižnim stanjem, spremi i zaključi, a „gdje ima“ nađe skladište.
func TestSredstvaKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "mts.db"))
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
	repo := repository.NewMtsRepository(baza, rec)
	if err := repo.OsigurajKatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	svc := service.NewMtsService(repo, repository.NewSectionRepository(baza, rec), userRepo)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	predlosci := map[string]*template.Template{}
	tmpl := func(stranica string) *template.Template {
		if tp, ok := predlosci[stranica]; ok {
			return tp
		}
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		predlosci[stranica] = tp
		return tp
	}
	h := NewMtsHandler(func() *service.MtsService { return svc }, users, nil, nil, tmpl)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sredstva", h.ShowPregled)
	mux.HandleFunc("GET /sredstva/promet", h.ShowPromet)
	mux.HandleFunc("GET /sredstva/mts.xlsx", h.IzvoziTablicu)
	mux.HandleFunc("GET /sredstva/katalog", h.ShowKatalog)
	mux.HandleFunc("POST /sredstva/katalog", h.HandleSaveVrsta)
	mux.HandleFunc("POST /sredstva/katalog/{id}", h.HandleSaveVrsta)
	mux.HandleFunc("GET /sredstva/na-terenu", h.ShowNaTerenu)
	mux.HandleFunc("GET /sredstva/gdje/{vrsta}", h.ShowGdjeIma)
	mux.HandleFunc("GET /sredstva/skladista/novo", h.ShowSkladisteForm)
	mux.HandleFunc("POST /sredstva/skladista", h.HandleSaveSkladiste)
	mux.HandleFunc("GET /sredstva/skladista/{id}", h.ShowSkladiste)
	mux.HandleFunc("GET /sredstva/skladista/{id}/uredi", h.ShowSkladisteForm)
	mux.HandleFunc("POST /sredstva/skladista/{id}", h.HandleSaveSkladiste)
	mux.HandleFunc("GET /sredstva/skladista/{id}/promet/novo", h.ShowPrometForm)
	mux.HandleFunc("POST /sredstva/skladista/{id}/promet", h.HandleSavePromet)
	mux.HandleFunc("GET /sredstva/skladista/{id}/popis", h.ShowPopisNovo)
	mux.HandleFunc("GET /sredstva/popisi", h.ShowPopisi)
	mux.HandleFunc("POST /sredstva/popisi", h.HandleSavePopis)
	mux.HandleFunc("GET /sredstva/popisi/{id}", h.ShowPopis)
	mux.HandleFunc("GET /sredstva/popisi/{id}/uredi", h.ShowPopisUredi)
	mux.HandleFunc("POST /sredstva/popisi/{id}", h.HandleSavePopis)
	mux.HandleFunc("POST /sredstva/popisi/{id}/zakljuci", h.HandleZakljuciPopis)

	skladistar := &models.User{ID: uuid.New(), FullName: "Skladištar Osijek"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	zovi := func(metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, skladistar)
		ctx = context.WithValue(ctx, contextKeyPerms, uprava)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, kod int, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != kod {
			t.Fatalf("%s: %d, a mora biti %d\n%s", sto, w.Code, kod, w.Body.String())
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
	odredište := func(w *httptest.ResponseRecorder, sto string) string {
		t.Helper()
		l := w.Header().Get("Location")
		if w.Code != http.StatusSeeOther || strings.Contains(l, "error=") {
			t.Fatalf("%s: %d %s", sto, w.Code, l)
		}
		return l
	}

	// katalog: nova vrsta ide na kraj skupine i odmah se nudi u obrascu zahvata; gašenje je vidljivo
	mora(zovi(http.MethodGet, "/sredstva/katalog", nil), http.StatusOK, "katalog", "Nova vrsta sredstva", "16.</td>", "Čekić tesarski")
	odredište(zovi(http.MethodPost, "/sredstva/katalog", url.Values{"naziv": {"Ćuskija velika"}, "grupa": {"ALAT"}, "jedinica": {"kom"}}), "nova vrsta")
	mora(zovi(http.MethodGet, "/sredstva/katalog", nil), http.StatusOK, "katalog s novom", "Ćuskija velika", "17.</td>", "cuskija-velika")
	odredište(zovi(http.MethodPost, "/sredstva/katalog/cuskija-velika", url.Values{"naziv": {"Ćuskija velika"}, "grupa": {"ALAT"}, "jedinica": {"kom"}, "aktivna": {"0"}}), "gašenje vrste")
	mora(zovi(http.MethodGet, "/sredstva/katalog", nil), http.StatusOK, "ugašena", "ugašena")

	// prazan pregled nudi novo skladište; katalog je već u tablici stanja
	mora(zovi(http.MethodGet, "/sredstva?sektor=B", nil), http.StatusOK, "pregled", "Nema upisanih skladišta", "Novo skladište", "Vreće 50x80 cm", "Pribor i osobna zaštitna sredstva")
	if w := zovi(http.MethodGet, "/sredstva?sektor=B", nil); strings.Contains(w.Body.String(), "Ćuskija") {
		t.Error("ugašena vrsta se nudi u stanju")
	}
	mora(zovi(http.MethodGet, "/sredstva/skladista/novo?sektor=B", nil), http.StatusOK, "obrazac skladišta", `value="34"`, "Drava i Dunav")
	kamo := odredište(zovi(http.MethodPost, "/sredstva/skladista", url.Values{"sektor": {"B"}, "area_id": {"34"}, "naziv": {"Centralno skladište Osijek"},
		"adresa": {"Splavarska 2a"}, "centralno": {"1"}}), "novo skladište")
	sk := strings.SplitN(strings.TrimPrefix(kamo, "/sredstva/skladista/"), "?", 2)[0]
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk, nil), http.StatusOK, "kartica", "Centralno skladište Osijek", "središnje skladište", "Još nema prometa", "Primka", "Izdaj na teren")

	// primka praznih vreća, pa punjenje
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk+"/promet/novo?vrsta=PRIMKA", nil), http.StatusOK, "obrazac zahvata", `name="vrsta" value="PRIMKA" checked`, "Vreće 50x80 cm", `data-oblici="PRAZNO,PUNJENO"`)
	danas := time.Now().In(models.Zagreb).Format("2006-01-02")
	odredište(zovi(http.MethodPost, "/sredstva/skladista/"+sk+"/promet", url.Values{"vrsta": {"PRIMKA"}, "datum": {danas}, "sredstvo": {"vrece-50x80"},
		"oblik": {"PRAZNO"}, "kolicina": {"100 000"}, "preuzeo": {"Vreće d.o.o."}, "dokument": {"OT 44/26"}, "nalozio": {"rukovoditelj sektora"}}), "primka")
	odredište(zovi(http.MethodPost, "/sredstva/skladista/"+sk+"/promet", url.Values{"vrsta": {"PUNJENJE"}, "datum": {danas}, "sredstvo": {"vrece-50x80"},
		"oblik": {"PRAZNO"}, "u_oblik": {"PUNJENO"}, "kolicina": {"5000"}}), "punjenje")
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk, nil), http.StatusOK, "kartica poslije", "95 000", "5 000", "Punjenje vreća", "nalog: rukovoditelj sektora", "Dopremio: Vreće d.o.o.", "OT 44/26")
	// preko zalihe se ne izdaje
	w := zovi(http.MethodPost, "/sredstva/skladista/"+sk+"/promet", url.Values{"vrsta": {"IZDANO"}, "datum": {danas}, "sredstvo": {"vrece-50x80"},
		"oblik": {"PUNJENO"}, "kolicina": {"9000"}, "dionica": {"B.34.1"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("izdavanje preko zalihe prošlo: %s", l)
	}
	odredište(zovi(http.MethodPost, "/sredstva/skladista/"+sk+"/promet", url.Values{"vrsta": {"IZDANO"}, "datum": {danas}, "sredstvo": {"vrece-50x80"},
		"oblik": {"PUNJENO"}, "kolicina": {"4000"}, "dionica": {"B.34.1"}, "preuzeo": {"vodočuvar"}}), "izdavanje")
	mora(zovi(http.MethodGet, "/sredstva/na-terenu?sektor=B", nil), http.StatusOK, "na terenu", "B.34.1", "4 000")
	mora(zovi(http.MethodGet, "/sredstva/promet?sektor=B", nil), http.StatusOK, "knjiga", "Izdano na teren", "teren · B.34.1", "−4 000", "&#43;4 000", "Preuzeo: vodočuvar")
	mora(zovi(http.MethodGet, "/sredstva/gdje/vrece-50x80", nil), http.StatusOK, "gdje ima", "Centralno skladište Osijek", "96 000")
	mora(zovi(http.MethodGet, "/sredstva?sektor=B", nil), http.StatusOK, "pregled sa stanjem", "96 000", "95 000", "1 000")

	// popis: predložak nosi knjižno, spremi se s prebrojanim i potrebama, zaključi
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk+"/popis?dan="+danas, nil), http.StatusOK, "popis predložak", "Popis sredstava", `name="u:vrece-50x80:PRAZNO"`, `value="95000"`)
	obrazac := url.Values{"skladiste": {sk}, "dan": {danas}, "u:vrece-50x80:PRAZNO": {"94 990"}, "k:vrece-50x80:PRAZNO": {"95000"},
		"u:vrece-50x80:PUNJENO": {"1000"}, "k:vrece-50x80:PUNJENO": {"1000"}, "p:vrece-50x80:PRAZNO": {"50 000"}, "n:vrece-50x80:PRAZNO": {"10 poderanih"}}
	kamo = odredište(zovi(http.MethodPost, "/sredstva/popisi", obrazac), "spremanje popisa")
	popis := strings.SplitN(strings.TrimPrefix(kamo, "/sredstva/popisi/"), "?", 2)[0]
	mora(zovi(http.MethodGet, "/sredstva/popisi/"+popis, nil), http.StatusOK, "popis nacrt", "nacrt", "94 990", "−10", "50 000", "10 poderanih", "Zaključi")
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk+"/popis?dan="+danas, nil), http.StatusSeeOther, "isti dan vodi na postojeći")
	odredište(zovi(http.MethodPost, "/sredstva/popisi/"+popis+"/zakljuci", url.Values{}), "zaključenje")
	mora(zovi(http.MethodGet, "/sredstva/popisi/"+popis, nil), http.StatusOK, "zaključen", "zaključen", "razlike proknjižene")
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk, nil), http.StatusOK, "stanje poslije popisa", "94 990", "Usklađenje po popisu")
	mora(zovi(http.MethodGet, "/sredstva/popisi?sektor=B", nil), http.StatusOK, "popisi", "Centralno skladište Osijek", "zaključen")
	if w := zovi(http.MethodGet, "/sredstva/popisi/"+popis+"/uredi", nil); w.Code != http.StatusForbidden {
		t.Errorf("uređivanje zaključenog: %d", w.Code)
	}

	// tablica za Glavni centar: redak po vrsti, stupci skladišta i zbroj sektora
	w = zovi(http.MethodGet, "/sredstva/mts.xlsx?sektor=B&dan="+danas, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "MTS_b_"+danas) {
		t.Fatalf("izvoz: %d %s", w.Code, w.Header().Get("Content-Disposition"))
	}
	redci, err := procitajXLSX(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var sve []string
	for _, r := range redci {
		sve = append(sve, strings.Join(r, "|"))
	}
	list := strings.Join(sve, "\n")
	for _, want := range []string{"POPIS SREDSTAVA ZA OBRANU OD POPLAVA PO SKLADIŠTIMA", "BP 34 - DRAVA I DUNAV", "Centralno skladište Osijek", "Splavarska 2a",
		"Dodatne potrebe za nabavom u", "III|Materijal", "9.|Vreće 50x80 cm|kom|95990|50000|95990|50000", "IV|Pribor i osobna zaštitna sredstva", "popis zaključen"} {
		if !strings.Contains(list, want) {
			t.Errorf("tablica nema %q\n%s", want, list)
		}
	}
}
