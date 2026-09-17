package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'Dunav d.o.', '2026-01-01', '2026-01-01')`,
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
	mux.HandleFunc("POST /sredstva/katalog/{id}/obrisi", h.HandleObrisiVrstu)
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
	mux.HandleFunc("GET /sredstva/skladista/{id}/potrebe", h.ShowPotrebe)
	mux.HandleFunc("POST /sredstva/skladista/{id}/potrebe", h.HandleSavePotrebe)
	mux.HandleFunc("GET /sredstva/potrebe", h.ShowPotrebeSektora)
	mux.HandleFunc("GET /sredstva/skladista/{id}/skladiste.xlsx", h.IzvoziSkladiste)
	mux.HandleFunc("GET /sredstva/promet/{veza}/potvrda.xlsx", h.IzvoziPotvrdu)
	mux.HandleFunc("GET /sredstva/promet.xlsx", h.IzvoziPromet)
	mux.HandleFunc("GET /sredstva/popisi", h.ShowPopisi)
	mux.HandleFunc("POST /sredstva/popisi", h.HandleSavePopis)
	mux.HandleFunc("GET /sredstva/popisi/{id}", h.ShowPopis)
	mux.HandleFunc("GET /sredstva/popisi/{id}/uredi", h.ShowPopisUredi)
	mux.HandleFunc("POST /sredstva/popisi/{id}", h.HandleSavePopis)
	mux.HandleFunc("POST /sredstva/popisi/{id}/zakljuci", h.HandleZakljuciPopis)
	mux.HandleFunc("GET /sredstva/popisi/{id}/inventura.xlsx", h.IzvoziInventuru)

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
	// nekorištena vrsta se uklanja; korištena (poslije prometa) ne
	odredište(zovi(http.MethodPost, "/sredstva/katalog/cuskija-velika/obrisi", url.Values{}), "uklanjanje nekorištene")
	if w := zovi(http.MethodGet, "/sredstva/katalog", nil); strings.Contains(w.Body.String(), "Ćuskija") {
		t.Error("uklonjena vrsta još stoji u katalogu")
	}

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
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk, nil), http.StatusOK, "kartica poslije", "95 000", "5 000", "Punjenje", "nalog: rukovoditelj sektora", "Dopremio: Vreće d.o.o.", "OT 44/26")
	// preko zalihe se ne izdaje
	w := zovi(http.MethodPost, "/sredstva/skladista/"+sk+"/promet", url.Values{"vrsta": {"IZDANO"}, "datum": {danas}, "sredstvo": {"vrece-50x80"},
		"oblik": {"PUNJENO"}, "kolicina": {"9000"}, "podrucje": {"34"}, "dionica": {"B.34.1"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("izdavanje preko zalihe prošlo: %s", l)
	}
	kamo = odredište(zovi(http.MethodPost, "/sredstva/skladista/"+sk+"/promet", url.Values{"vrsta": {"IZDANO"}, "datum": {danas}, "sredstvo": {"vrece-50x80"},
		"oblik": {"PUNJENO"}, "kolicina": {"4000"}, "podrucje": {"34"}, "dionica": {"B.34.1"}, "preuzeo": {"vodočuvar"}, "nalozio": {"voditelj COP-a"}, "dokument": {"OT 7/26"}}), "izdavanje")
	// otpremnica za taj zahvat
	veza := ""
	if i := strings.Index(kamo, "potvrda="); i >= 0 {
		veza = strings.SplitN(kamo[i+len("potvrda="):], "&", 2)[0]
	}
	if veza == "" {
		t.Fatalf("izdavanje ne vodi na potvrdu: %s", kamo)
	}
	w = zovi(http.MethodGet, "/sredstva/promet/"+veza+"/potvrda.xlsx", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "otpremnica_") {
		t.Fatalf("otpremnica: %d %s", w.Code, w.Header().Get("Content-Disposition"))
	}
	if r, err := procitajXLSX(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	} else {
		var sve []string
		for _, x := range r {
			sve = append(sve, strings.Join(x, "|"))
		}
		list := strings.Join(sve, "\n")
		for _, want := range []string{"OTPREMNICA br. OT 7/26", "Centralno skladište Osijek", "BP 34 Drava i Dunav · B.34.1", "voditelj COP-a", "vodočuvar", "1.|Vreće 50x80 cm|napunjeno|kom|4000"} {
			if !strings.Contains(list, want) {
				t.Errorf("otpremnica nema %q\n%s", want, list)
			}
		}
	}
	mora(zovi(http.MethodGet, "/sredstva/na-terenu?sektor=B", nil), http.StatusOK, "na terenu", "BP 34 Drava i Dunav · B.34.1", "4 000")
	mora(zovi(http.MethodGet, "/sredstva/promet?sektor=B", nil), http.StatusOK, "knjiga", "Izdano na teren", "teren · BP 34 Drava i Dunav · B.34.1", "−4 000", "&#43;4 000", "Preuzeo: vodočuvar")
	mora(zovi(http.MethodGet, "/sredstva/gdje/vrece-50x80", nil), http.StatusOK, "gdje ima", "Centralno skladište Osijek", "96 000")
	if l := zovi(http.MethodPost, "/sredstva/katalog/vrece-50x80/obrisi", url.Values{}).Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("korištena vrsta uklonjena: %s", l)
	}
	mora(zovi(http.MethodGet, "/sredstva?sektor=B", nil), http.StatusOK, "pregled sa stanjem", "96 000", "95 000", "1 000")

	// popis: predložak nosi knjižno, spremi se s prebrojanim i potrebama, zaključi
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk+"/popis?dan="+danas, nil), http.StatusOK, "popis predložak", "Inventura na dan", `name="u:vrece-50x80:PRAZNO"`, `value="95000"`)
	obrazac := url.Values{"skladiste": {sk}, "dan": {danas}, "u:vrece-50x80:PRAZNO": {"94 990"}, "k:vrece-50x80:PRAZNO": {"95000"},
		"u:vrece-50x80:PUNJENO": {"1000"}, "k:vrece-50x80:PUNJENO": {"1000"}, "n:vrece-50x80:PRAZNO": {"10 poderanih"}}
	kamo = odredište(zovi(http.MethodPost, "/sredstva/popisi", obrazac), "spremanje popisa")
	popis := strings.SplitN(strings.TrimPrefix(kamo, "/sredstva/popisi/"), "?", 2)[0]
	mora(zovi(http.MethodGet, "/sredstva/popisi/"+popis, nil), http.StatusOK, "popis nacrt", "nacrt", "94 990", "−10", "10 poderanih", "Zaključi")
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk+"/popis?dan="+danas, nil), http.StatusSeeOther, "isti dan vodi na postojeći")
	odredište(zovi(http.MethodPost, "/sredstva/popisi/"+popis+"/zakljuci", url.Values{}), "zaključenje")
	mora(zovi(http.MethodGet, "/sredstva/popisi/"+popis, nil), http.StatusOK, "zaključen", "zaključen", "razlike proknjižene")
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk, nil), http.StatusOK, "stanje poslije popisa", "94 990", "Usklađenje po inventuri")
	mora(zovi(http.MethodGet, "/sredstva/popisi?sektor=B", nil), http.StatusOK, "popisi", "Centralno skladište Osijek", "zaključen")
	if w := zovi(http.MethodGet, "/sredstva/popisi/"+popis+"/uredi", nil); w.Code != http.StatusForbidden {
		t.Errorf("uređivanje zaključenog: %d", w.Code)
	}
	// inventura u Excelu: knjižno, prebrojano, razlika, potrebe
	w = zovi(http.MethodGet, "/sredstva/popisi/"+popis+"/inventura.xlsx", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz inventure: %d", w.Code)
	}
	if r, err := procitajXLSX(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	} else {
		var sve []string
		for _, x := range r {
			sve = append(sve, strings.Join(x, "|"))
		}
		list := strings.Join(sve, "\n")
		for _, want := range []string{"INVENTURA SREDSTAVA ZA OBRANU OD POPLAVA NA DAN", "zaključena", "9.|Vreće 50x80 cm|kom|prazno|95000|94990|-10|||10 poderanih", "|Vreće 50x80 cm|kom|napunjeno|1000|1000|"} {
			if !strings.Contains(list, want) {
				t.Errorf("inventura nema %q\n%s", want, list)
			}
		}
	}

	// knjiga prometa u Excelu, po filtru
	w = zovi(http.MethodGet, "/sredstva/promet.xlsx?sektor=B&vrsta=vrece-50x80", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz prometa: %d", w.Code)
	}
	if r, err := procitajXLSX(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	} else {
		var sve []string
		for _, x := range r {
			sve = append(sve, strings.Join(x, "|"))
		}
		list := strings.Join(sve, "\n")
		for _, want := range []string{"KNJIGA PROMETA SREDSTAVA", "Izdano na teren|Centralno skladište Osijek|Vreće 50x80 cm|napunjeno|-4000|kom", "OT 7/26"} {
			if !strings.Contains(list, want) {
				t.Errorf("knjiga nema %q\n%s", want, list)
			}
		}
	}

	// potrebe za nabavom, odvojeno od inventure, za godinu nabave koja pripada danas
	godina := models.GodinaPotreba(time.Now().In(models.Zagreb))
	mora(zovi(http.MethodGet, "/sredstva/skladista/"+sk+"/potrebe", nil), http.StatusOK, "potrebe obrazac", "Potrebe za nabavom u "+strconv.Itoa(godina)+".", `name="pt:vrece-50x80"`)
	odredište(zovi(http.MethodPost, "/sredstva/skladista/"+sk+"/potrebe", url.Values{"godina": {strconv.Itoa(godina)}, "pt:vrece-50x80": {"50 000"}, "pn:vrece-50x80": {"potrošeno u obrani"}}), "potrebe")
	mora(zovi(http.MethodGet, "/sredstva/potrebe?sektor=B&godina="+strconv.Itoa(godina), nil), http.StatusOK, "potrebe sektora", "Vreće 50x80 cm", "50 000", "Centralno skladište Osijek")

	// kartica skladišta u Excelu: stanje s oblicima i potrebama, pa promet
	w = zovi(http.MethodGet, "/sredstva/skladista/"+sk+"/skladiste.xlsx?dan="+danas, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz skladišta: %d", w.Code)
	}
	if r, err := procitajXLSX(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	} else {
		var sve []string
		for _, x := range r {
			sve = append(sve, strings.Join(x, "|"))
		}
		list := strings.Join(sve, "\n")
		if !strings.Contains(list, "9.|Vreće 50x80 cm|kom|95990|94990|1000|50000") {
			t.Errorf("kartica skladišta nema redak vreća:\n%s", list)
		}
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
		"Dodatne potrebe za nabavom u", "III|Materijal", "9.|Vreće 50x80 cm|kom|95990|50000|95990|50000", "IV|Pribor i osobna zaštitna sredstva", "inventura zaključena"} {
		if !strings.Contains(list, want) {
			t.Errorf("tablica nema %q\n%s", want, list)
		}
	}
}
