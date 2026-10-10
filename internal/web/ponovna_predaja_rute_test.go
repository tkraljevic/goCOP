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

// Odluke (8. 10. 2026.), pitanje 12, kroz prave rute. Ana Anić (voditeljica
// centra) sastavi i preda izvješće sektora; Ivo Ivić (zamjenik) ga
// ispravi: izvješće postaje nacrt koji se mora ponovo predati, prvo vrijeme
// predaje ostaje uz novo, vidi se tko je mijenjao i tko je predao. Predano
// izvješće sektora ne briše se, nego se predaja povlači. Isto pravilo
// vrijedi za izvješće dionice Pere Perića prema podcentru.
func TestPonovnaPredajaKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "predaja.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	svc := service.NewIzvjescaService(repository.NewIzvjescaRepository(baza, rec), repository.NewSectionRepository(baza, rec), nil, nil, nil,
		repository.NewJournalRepository(baza, rec))
	svc.SetSektorska(repository.NewSektorskaIzvjescaRepository(baza, rec))
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewIzvjescaHandler(func() *service.IzvjescaService { return svc }, tmpl("izvjesca.html"), tmpl("izvjesce_form.html"), tmpl("izvjesce.html"))
	h.SetSektorsko(tmpl("sektorsko_form.html"), tmpl("sektorsko.html"))
	h.SetZaglavlje(func(sektor string) ZaglavljeIzvoza {
		return ZaglavljeIzvoza{Organizacija: "Hrvatske vode", Centar: "COP Primjerovo", Sektor: sektor, Mjesto: "Primjerovo", Datum: time.Now()}
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /izvjesca", h.ShowPopis)
	mux.HandleFunc("POST /izvjesca", h.HandleSpremi)
	mux.HandleFunc("GET /izvjesca/{id}", h.ShowIzvjesce)
	mux.HandleFunc("GET /izvjesca/{id}/uredi", h.ShowUredi)
	mux.HandleFunc("POST /izvjesca/{id}", h.HandleSpremi)
	mux.HandleFunc("POST /izvjesca/{id}/predaj", h.HandlePredaj)
	mux.HandleFunc("GET /izvjesca/{id}/izvjesce.xlsx", h.IzvoziIzvjesce)
	mux.HandleFunc("POST /sektorsko-izvjesce", h.HandleSektorskoSpremi)
	mux.HandleFunc("GET /sektorsko-izvjesce/{id}", h.ShowSektorsko)
	mux.HandleFunc("GET /sektorsko-izvjesce/{id}/uredi", h.ShowSektorskoUredi)
	mux.HandleFunc("POST /sektorsko-izvjesce/{id}", h.HandleSektorskoSpremi)
	mux.HandleFunc("POST /sektorsko-izvjesce/{id}/predaj", h.HandleSektorskoPredaj)
	mux.HandleFunc("POST /sektorsko-izvjesce/{id}/povuci", h.HandleSektorskoPovuci)
	mux.HandleFunc("POST /sektorsko-izvjesce/{id}/obrisi", h.HandleSektorskoObrisi)
	mux.HandleFunc("GET /sektorsko-izvjesce/{id}/izvjesce.xlsx", h.IzvoziSektorsko)

	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	ivo := &models.User{ID: uuid.New(), FullName: "Ivo Ivić"}
	pero := &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"P": true}, AllowedSectors: map[string]bool{"P": true}}
	dionica := &models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}}
	zovi := func(u *models.User, p *models.UserPermissions, metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(context.WithValue(r.Context(), contextKeyUser, u), contextKeyPerms, p)
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
			t.Errorf("%s: predložak puknuo", sto)
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}
	poruka := func(w *httptest.ResponseRecorder, vrsta, tekst string) {
		t.Helper()
		l, _ := url.QueryUnescape(w.Header().Get("Location"))
		if !strings.Contains(l, vrsta+"="+tekst) {
			t.Errorf("poruka: %q, a mora biti %s=%q", l, vrsta, tekst)
		}
	}
	nema := func(w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
		t.Helper()
		for _, d := range dijelovi {
			if strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: ne smije biti %q", sto, d)
			}
		}
	}
	danas := time.Now().In(models.Zagreb).Format("2006-01-02")

	// Ana sastavi i preda izvješće sektora
	obrazac := url.Values{"sektor": {"P"}, "dan": {danas}, "hidrometeo": {"Vodostaji rastu."}, "evakuacija": {"Evakuirano 40 ljudi."}, "predaj": {"1"}}
	w := zovi(ana, uprava, http.MethodPost, "/sektorsko-izvjesce", obrazac)
	poruka(w, "success", "Izvješće sektora je predano Glavnom centru.")
	id := strings.SplitN(strings.TrimPrefix(w.Header().Get("Location"), "/sektorsko-izvjesce/"), "?", 2)[0]
	dok := "/sektorsko-izvjesce/" + id
	w = zovi(ana, uprava, http.MethodGet, dok, nil)
	mora(w, http.StatusOK, "predano", "predano GCOP-u ", `action="`+dok+`/povuci"`, "Povuci predaju")
	nema(w, "predano", `action="`+dok+`/obrisi"`, "prvi put")
	mora(zovi(ana, uprava, http.MethodGet, dok+"/uredi", nil), http.StatusOK, "obrazac predanog",
		"Spremljena izmjena vraća ga u nacrt", "Spremi i ponovo predaj")
	// spremanje bez promjene predano izvješće ostavlja predanim
	bezPromjene := url.Values{"sektor": {"P"}, "dan": {danas}, "hidrometeo": {"Vodostaji rastu."}, "evakuacija": {"Evakuirano 40 ljudi."}}
	w = zovi(ivo, uprava, http.MethodPost, dok, bezPromjene)
	poruka(w, "success", "Izvješće sektora nije mijenjano i ostaje predano Glavnom centru.")
	w = zovi(ana, uprava, http.MethodGet, dok, nil)
	mora(w, http.StatusOK, "predano nakon spremanja bez promjene", "predano GCOP-u ", "Povuci predaju")
	nema(w, "predano nakon spremanja bez promjene", "prvi put", "izmijenio poslije predaje")

	// Ivo ispravi broj evakuiranih i samo spremi: nacrt, čeka ponovnu predaju
	obrazac.Set("evakuacija", "Evakuirano 4 ljudi.")
	obrazac.Del("predaj")
	w = zovi(ivo, uprava, http.MethodPost, dok, obrazac)
	poruka(w, "success", "Izmjena je spremljena; izvješće je sada nacrt i treba ga ponovo predati Glavnom centru.")
	w = zovi(ana, uprava, http.MethodGet, dok, nil)
	mora(w, http.StatusOK, "nacrt nakon izmjene", "badge badge-pending\">nacrt", "prvi put predano GCOP-u ", ", čeka ponovnu predaju",
		"izmijenio poslije predaje: Ivo Ivić", "Ponovo predaj GCOP-u", "Sastavio: <strong>Ana Anić</strong>")
	nema(w, "nacrt nakon izmjene", `action="`+dok+`/obrisi"`, "Povuci predaju")
	mora(zovi(ana, uprava, http.MethodGet, "/izvjesca", nil), http.StatusOK, "popis", "čeka ponovnu predaju")
	// nacrt koji je bio predan ne briše se
	w = zovi(ana, uprava, http.MethodPost, dok+"/obrisi", url.Values{})
	poruka(w, "error", "izvješće sektora koje je bilo predano Glavnom centru ne briše se")
	// povući se može samo predano
	w = zovi(ana, uprava, http.MethodPost, dok+"/povuci", url.Values{})
	poruka(w, "error", "izvješće nije predano")

	// Ivo ponovo predaje jednim gumbom
	obrazac.Set("predaj", "1")
	w = zovi(ivo, uprava, http.MethodPost, dok, obrazac)
	poruka(w, "success", "Izvješće sektora je ponovno predano Glavnom centru.")
	w = zovi(ana, uprava, http.MethodGet, dok, nil)
	mora(w, http.StatusOK, "ponovno predano", "prvi put predano GCOP-u ", ", ponovno ", "izmijenio poslije predaje: Ivo Ivić", "(predao Ivo Ivić)", "Povuci predaju")
	// izvoz nosi isti trag
	w = zovi(ana, uprava, http.MethodGet, dok+"/izvjesce.xlsx", nil)
	redci, err := procitajXLSX(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var sve []string
	for _, r := range redci {
		sve = append(sve, strings.Join(r, "|"))
	}
	if s := strings.Join(sve, "\n"); !strings.Contains(s, "sastavio: Ana Anić · izmijenio poslije predaje: Ivo Ivić · prvi put predano Glavnom centru ") || !strings.Contains(s, "(predao Ivo Ivić)") {
		t.Errorf("izvoz nema trag predaje:\n%s", s)
	}
	// Ana povlači predaju: nacrt, sadržaj ostaje
	w = zovi(ana, uprava, http.MethodPost, dok+"/povuci", url.Values{})
	poruka(w, "success", "Predaja je povučena")
	mora(zovi(ana, uprava, http.MethodGet, dok, nil), http.StatusOK, "povučeno", "badge badge-pending\">nacrt", "Evakuirano 4 ljudi.", "Ponovo predaj GCOP-u")
	// Pero (bez uprave sektora) ne povlači
	w = zovi(pero, dionica, http.MethodPost, dok+"/povuci", url.Values{})
	poruka(w, "error", "predaju izvješća sektora povlači voditelj centra ili rukovoditelj sektora")
	// ponovna predaja gumbom na izvješću javlja se kao ponovna
	w = zovi(ana, uprava, http.MethodPost, dok+"/predaj", url.Values{})
	poruka(w, "success", "Izvješće sektora je ponovno predano Glavnom centru.")

	// Izvješće dionice prema podcentru: isto pravilo
	izv := url.Values{"dan": {danas}, "dionica": {"P.1.1"}, "stadij": {"REDOVNA"}, "vodotok": {"Primjerica"}, "pregled": {"Nasip pregledan."}, "predaj": {"1"}}
	w = zovi(pero, dionica, http.MethodPost, "/izvjesca", izv)
	poruka(w, "success", "Izvješće je predano.")
	idd := strings.SplitN(strings.TrimPrefix(w.Header().Get("Location"), "/izvjesca/"), "?", 2)[0]
	nema(zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idd, nil), "predano izvješće dionice kao autor", `action="/izvjesca/`+idd+`/obrisi"`)
	// isti obrazac spremljen bez promjene: ostaje predano, ne povlači se
	isto := url.Values{"dan": {danas}, "dionica": {"P.1.1"}, "stadij": {"REDOVNA"}, "vodotok": {"Primjerica"}, "pregled": {"Nasip pregledan."}}
	w = zovi(pero, dionica, http.MethodPost, "/izvjesca/"+idd, isto)
	poruka(w, "success", "Izvješće nije mijenjano i ostaje predano u podcentar.")
	nema(zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idd, nil), "predano izvješće dionice nakon spremanja bez promjene", "badge badge-pending\">nacrt")
	// nacrt koji nikad nije predan briše i autor
	jucer := url.Values{"dan": {time.Now().In(models.Zagreb).AddDate(0, 0, -1).Format("2006-01-02")}, "dionica": {"P.1.1"}, "stadij": {"REDOVNA"}, "pregled": {"Nacrt."}}
	w = zovi(pero, dionica, http.MethodPost, "/izvjesca", jucer)
	idn := strings.SplitN(strings.TrimPrefix(w.Header().Get("Location"), "/izvjesca/"), "?", 2)[0]
	mora(zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idn, nil), http.StatusOK, "nacrt kao autor", `action="/izvjesca/`+idn+`/obrisi"`)
	mora(zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idd+"/uredi", nil), http.StatusOK, "obrazac predanog izvješća dionice",
		"Spremljena izmjena vraća ga u nacrt", "Spremi i ponovo predaj")
	izv.Set("pregled", "Procjeđivanje kod Primjerova.")
	izv.Del("predaj")
	w = zovi(pero, dionica, http.MethodPost, "/izvjesca/"+idd, izv)
	poruka(w, "success", "Izmjena je spremljena; izvješće je sada nacrt i treba ga ponovo predati u podcentar.")
	mora(zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idd, nil), http.StatusOK, "izvješće dionice nakon izmjene",
		"badge badge-pending\">nacrt", "prvi put predano ", ", čeka ponovnu predaju", "izmijenio poslije predaje: Pero Perić", "Ponovo predaj")
	// gumb za brisanje: izvješće koje je bilo predano briše samo uprava
	nema(zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idd, nil), "nacrt nakon predaje kao autor", `action="/izvjesca/`+idd+`/obrisi"`)
	mora(zovi(ana, uprava, http.MethodGet, "/izvjesca/"+idd, nil), http.StatusOK, "nacrt nakon predaje kao uprava", `action="/izvjesca/`+idd+`/obrisi"`)
	mora(zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idd+"/uredi", nil), http.StatusOK, "obrazac nacrta nakon predaje",
		"Izvješće je nacrt: prvi put predano u podcentar", "Spremi i ponovo predaj")
	izv.Set("predaj", "1")
	w = zovi(pero, dionica, http.MethodPost, "/izvjesca/"+idd, izv)
	poruka(w, "success", "Izvješće je ponovno predano.")
	w = zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idd+"/izvjesce.xlsx", nil)
	redci, err = procitajXLSX(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sve = sve[:0]
	for _, r := range redci {
		sve = append(sve, strings.Join(r, "|"))
	}
	if s := strings.Join(sve, "\n"); !strings.Contains(s, "izmijenio poslije predaje: Pero Perić · prvi put predano u podcentar ") || !strings.Contains(s, ", ponovno ") {
		t.Errorf("izvoz izvješća dionice nema trag predaje:\n%s", s)
	}
	mora(zovi(pero, dionica, http.MethodGet, "/izvjesca/"+idd, nil), http.StatusOK, "ponovno predano izvješće dionice", "prvi put predano ", ", ponovno ")
	// ponovna predaja gumbom na izvješću javlja se kao ponovna, prva kao prva
	izv.Del("predaj")
	izv.Set("pregled", "Procjeđivanje kod Primjerova sanirano.")
	zovi(pero, dionica, http.MethodPost, "/izvjesca/"+idd, izv)
	w = zovi(pero, dionica, http.MethodPost, "/izvjesca/"+idd+"/predaj", url.Values{})
	poruka(w, "success", "Izvješće je ponovno predano.")
	w = zovi(pero, dionica, http.MethodPost, "/izvjesca/"+idn+"/predaj", url.Values{})
	poruka(w, "success", "Izvješće je predano.")
}

func TestPorukePredajeIzvjesca(t *testing.T) {
	prvi := time.Date(2026, 10, 7, 9, 40, 0, 0, models.Zagreb)
	drugi := time.Date(2026, 10, 7, 14, 20, 0, 0, models.Zagreb)
	trag := models.TragPredaje{PrvoPredanoAt: &prvi}
	if p := porukaPredajeSektorskog(nil); p != "Izvješće sektora je predano Glavnom centru." {
		t.Errorf("sektor bez izvješća: %q", p)
	}
	if p := porukaPredajeSektorskog(&models.SektorskoIzvjesce{PredanoAt: &prvi, TragPredaje: trag}); p != "Izvješće sektora je predano Glavnom centru." {
		t.Errorf("sektor prvi put: %q", p)
	}
	if p := porukaPredajeSektorskog(&models.SektorskoIzvjesce{PredanoAt: &drugi, TragPredaje: trag}); p != "Izvješće sektora je ponovno predano Glavnom centru." {
		t.Errorf("sektor ponovno: %q", p)
	}
	if p := porukaPredajeIzvjesca(nil); p != "Izvješće je predano." {
		t.Errorf("dionica bez izvješća: %q", p)
	}
	if p := porukaPredajeIzvjesca(&models.DnevnoIzvjesce{PredanoAt: &prvi, TragPredaje: trag}); p != "Izvješće je predano." {
		t.Errorf("dionica prvi put: %q", p)
	}
	if p := porukaPredajeIzvjesca(&models.DnevnoIzvjesce{PredanoAt: &drugi, TragPredaje: trag}); p != "Izvješće je ponovno predano." {
		t.Errorf("dionica ponovno: %q", p)
	}
}

func TestPorukeSpremanjaIzvjesca(t *testing.T) {
	sad := time.Now()
	bilo := models.TragPredaje{PrvoPredanoAt: &sad}
	for _, s := range []struct {
		iz      *models.SektorskoIzvjesce
		predano bool
		poruka  string
	}{
		{&models.SektorskoIzvjesce{}, false, "Izvješće sektora je spremljeno kao nacrt."},
		{&models.SektorskoIzvjesce{}, true, "Izvješće sektora je predano Glavnom centru."},
		{&models.SektorskoIzvjesce{TragPredaje: bilo}, false, "Izmjena je spremljena; izvješće je sada nacrt i treba ga ponovo predati Glavnom centru."},
		{&models.SektorskoIzvjesce{TragPredaje: bilo}, true, "Izvješće sektora je ponovno predano Glavnom centru."},
		{&models.SektorskoIzvjesce{TragPredaje: bilo, PredanoAt: &sad}, false, "Izvješće sektora nije mijenjano i ostaje predano Glavnom centru."},
		{&models.SektorskoIzvjesce{TragPredaje: bilo, PredanoAt: &sad}, true, "Izvješće sektora nije mijenjano i ostaje predano Glavnom centru."},
	} {
		if p := porukaSpremanjaSektorskog(s.iz, s.predano); p != s.poruka {
			t.Errorf("sektor %v/%v: %q", s.iz.BiloPredano(), s.predano, p)
		}
	}
	for _, s := range []struct {
		iz      *models.DnevnoIzvjesce
		predano bool
		poruka  string
	}{
		{&models.DnevnoIzvjesce{}, false, "Izvješće je spremljeno kao nacrt."},
		{&models.DnevnoIzvjesce{}, true, "Izvješće je predano."},
		{&models.DnevnoIzvjesce{TragPredaje: bilo}, false, "Izmjena je spremljena; izvješće je sada nacrt i treba ga ponovo predati u podcentar."},
		{&models.DnevnoIzvjesce{TragPredaje: bilo}, true, "Izvješće je ponovno predano."},
		{&models.DnevnoIzvjesce{TragPredaje: bilo, PredanoAt: &sad}, false, "Izvješće nije mijenjano i ostaje predano u podcentar."},
		{&models.DnevnoIzvjesce{TragPredaje: bilo, PredanoAt: &sad}, true, "Izvješće nije mijenjano i ostaje predano u podcentar."},
	} {
		if p := porukaSpremanjaIzvjesca(s.iz, s.predano); p != s.poruka {
			t.Errorf("dionica %v/%v: %q", s.iz.BiloPredano(), s.predano, p)
		}
	}
}
