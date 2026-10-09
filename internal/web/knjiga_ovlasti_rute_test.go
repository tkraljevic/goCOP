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

// Knjiga vodočuvara kroz rute po Odlukama (8. 10. 2026.), pitanje 1:
// dežurni operater sektora vidi knjige i upisuje bilješku, ali nema gumba
// za parafu ni obrasca za zadatak, a parafa i zadatak mimo obrasca se
// odbijaju; poslovođa izvođača u knjige ne ulazi; rukovoditelj dionice
// koja nije vodočuvareva čita list, ali na njemu nema ni parafe ni upisa.
// Podaci su izmišljeni: sektor P, područje 1.
func TestKnjigaVodocuvaraKrozRutePoOdlukama(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "knjiga.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica', 'Primjerska 1', '01/000-000', 'cop@primjer.hr')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Primjerica', 'VGI Primjerica', 'Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop-primjer")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	ctx := context.Background()
	p, bp := "P", 1
	osoba := func(korisnik, ime string, d models.Duty) *models.User {
		t.Helper()
		u := &models.User{ID: uuid.New(), Username: korisnik, FullName: ime, IsActive: true}
		d.SectorID, d.IsPrimary = &p, true
		if err := userRepo.CreateUser(u, &d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	vodocuvar := osoba("pperic", "Pero Perić", models.Duty{Title: "Vodočuvar Primjerica", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	operater := osoba("aanic", "Ana Anić", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	poslovodja := osoba("mmarkic", "Marko Markić", models.Duty{Title: "Poslovođa izvođača", Role: models.RoleServiceLeaderForeman, ScopeType: models.ScopeArea, AreaID: &bp})
	dionicar := osoba("iivic", "Ivo Ivić", models.Duty{Title: "Rukovoditelj dionice P.1.2", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.2"})

	orgRepo := repository.NewOrgRepository(baza, rec)
	vodRepo := repository.NewVodocuvarRepository(baza, rec)
	vod := service.NewVodocuvarService(vodRepo, users, "cop-primjer")
	vod.SetOrg(orgRepo)
	vod.SetWeather(nil)
	sad := time.Now()
	n := sad.In(models.Zagreb)
	list := &models.VodocuvarskiList{UserID: vodocuvar.ID.String(), Ime: vodocuvar.FullName, Sektor: p, AreaID: bp,
		Datum: time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb), Broj: 1, PredanoAt: &sad, Opis: "Obilazak nasipa"}
	if err := vodRepo.Save(ctx, list); err != nil {
		t.Fatal(err)
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
	h := NewVodocuvarHandler(func() *service.VodocuvarService { return vod }, users, func() *repository.OrgRepository { return orgRepo }, tmpl("vodocuvar.html"), tmpl("vodocuvar_list.html"))
	h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{} })
	h.SetKalendar(tmpl("vodocuvar_kalendar.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vodocuvar", h.ShowPopis)
	mux.HandleFunc("GET /vodocuvar/kalendar", h.ShowKalendar)
	mux.HandleFunc("GET /vodocuvar/{id}", h.ShowList)
	mux.HandleFunc("POST /vodocuvar/zadatak", h.HandleZadatak)
	mux.HandleFunc("POST /vodocuvar/upis", h.HandleUpis)
	mux.HandleFunc("POST /vodocuvar/{id}/radnja", h.HandleRadnja)
	zovi := func(u *models.User, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if forma != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		cijeli, err := users.GetUserByID(u.ID)
		if err != nil || cijeli == nil {
			t.Fatalf("%s: %v", u.FullName, err)
		}
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	loc := func(w *httptest.ResponseRecorder) string { return mustUnescape(w.Header().Get("Location")) }
	danas := n.Format("2006-01-02")
	upis := url.Values{"vodocuvar": {vodocuvar.ID.String()}, "datum": {danas}, "tekst": {"Javljeno iz COP-a"}}
	zadatak := url.Values{"vodocuvar": {vodocuvar.ID.String()}, "tekst": {"Pregledati nasip u 6 h"}}

	// dežurni operater: knjiga i obrazac bilješke, bez zadatka i parafe
	popis := zovi(operater, http.MethodGet, "/vodocuvar", nil)
	if popis.Code != http.StatusOK || !strings.Contains(popis.Body.String(), "Pero Perić") || !strings.Contains(popis.Body.String(), `action="/vodocuvar/upis"`) {
		t.Fatalf("knjige za operatera: %d", popis.Code)
	}
	if strings.Contains(popis.Body.String(), "Zadaj zadatak vodočuvaru") {
		t.Error("operater ima obrazac za zadatak")
	}
	// opis knjige kaže tko parafira po odluci, a ne „ostali rukovoditelji vezani uz područje”
	if b := strings.Join(strings.Fields(popis.Body.String()), " "); strings.Contains(b, "ostali rukovoditelji vezani uz područje") ||
		!strings.Contains(b, "rukovoditelj dionice kad se list tiče njegove dionice") {
		t.Error("opis knjige ne kaže tko parafira list")
	}
	stranica := zovi(operater, http.MethodGet, "/vodocuvar/"+list.ID, nil).Body.String()
	if !strings.Contains(stranica, "Upis u dnevnik") || strings.Contains(stranica, `value="parafiraj"`) {
		t.Error("list za operatera: mora imati upis, a ne parafu")
	}
	if l := loc(zovi(operater, http.MethodPost, "/vodocuvar/upis", upis)); !strings.Contains(l, "success") {
		t.Errorf("bilješka operatera: %s", l)
	}
	if l := loc(zovi(operater, http.MethodPost, "/vodocuvar/zadatak", zadatak)); !strings.Contains(l, "error") {
		t.Errorf("zadatak operatera: %s", l)
	}
	if l := loc(zovi(operater, http.MethodPost, "/vodocuvar/"+list.ID+"/radnja", url.Values{"radnja": {"parafiraj"}})); !strings.Contains(l, "error") {
		t.Errorf("parafa operatera: %s", l)
	}
	// kalendar mu je samo za čitanje: bez + u danu i bez upute kako se zadaje
	kal := zovi(operater, http.MethodGet, "/vodocuvar/kalendar?vodocuvar="+vodocuvar.ID.String(), nil)
	if kal.Code != http.StatusOK || !strings.Contains(kal.Body.String(), "Kalendar zadataka: Pero Perić") {
		t.Fatalf("kalendar za operatera: %d", kal.Code)
	}
	if b := kal.Body.String(); strings.Contains(b, `action="/vodocuvar/zadatak"`) || strings.Contains(b, "Zadatak se zadaje") {
		t.Error("kalendar operatera nudi zadavanje zadatka")
	}

	// poslovođa izvođača: u knjige ne ulazi, ni izravnim upisom
	if w := zovi(poslovodja, http.MethodGet, "/vodocuvar", nil); w.Code != http.StatusForbidden {
		t.Errorf("poslovođa na knjigama: %d", w.Code)
	}
	if w := zovi(poslovodja, http.MethodGet, "/vodocuvar/"+list.ID, nil); w.Code != http.StatusForbidden {
		t.Errorf("poslovođa na listu: %d", w.Code)
	}
	if w := zovi(poslovodja, http.MethodPost, "/vodocuvar/upis", upis); w.Code != http.StatusForbidden {
		t.Errorf("poslovođa upisuje: %d", w.Code)
	}

	// rukovoditelj dionice P.1.2: Perin list (P.1.1) čita, ali ga ne parafira
	// i u njega ne upisuje; uz knjigu nema obrasca bilješke
	if w := zovi(dionicar, http.MethodGet, "/vodocuvar/"+list.ID, nil); w.Code != http.StatusOK ||
		strings.Contains(w.Body.String(), "Upis u dnevnik") || strings.Contains(w.Body.String(), `value="parafiraj"`) {
		t.Errorf("list za rukovoditelja druge dionice: %d", w.Code)
	}
	if s := zovi(dionicar, http.MethodGet, "/vodocuvar", nil).Body.String(); !strings.Contains(s, "Pero Perić") || strings.Contains(s, `action="/vodocuvar/upis"`) {
		t.Error("knjige za rukovoditelja druge dionice: knjiga bez obrasca bilješke")
	}
	if l := loc(zovi(dionicar, http.MethodPost, "/vodocuvar/upis", upis)); !strings.Contains(l, "error") {
		t.Errorf("bilješka rukovoditelja druge dionice: %s", l)
	}

	l, _ := vodRepo.ZaDan(ctx, vodocuvar.ID.String(), list.Datum)
	if l == nil || len(l.Parafe) != 0 || len(l.Upisi) != 1 || len(vod.Zadaci(ctx, vodocuvar.ID.String())) != 0 {
		t.Fatalf("list nakon pokušaja: %+v", l)
	}
}

// Izbor sektora i područja na knjigama ostavlja vodočuvare čije je glavno
// zaduženje u njemu; prazan izbor ne miče nikoga.
func TestVodocuvariUOdabiru(t *testing.T) {
	p, q := "P", "Q"
	jedan, dva := 1, 2
	osoba := func(ime string, d ...models.Duty) models.User {
		for i := range d {
			d[i].IsActive = true
		}
		return models.User{FullName: ime, Duties: d}
	}
	svi := []models.User{
		osoba("Pero Perić", models.Duty{Role: models.RoleWaterGuard, SectorID: &p, AreaID: &jedan}),
		osoba("Ana Anić", models.Duty{Role: models.RoleWaterGuard, SectorID: &p, AreaID: &dva}),
		osoba("Ivo Ivić", models.Duty{Role: models.RoleWaterGuard, SectorID: &q}),
		osoba("Iva Ivić"),
	}
	imena := func(us []models.User) string {
		var out []string
		for _, u := range us {
			out = append(out, u.FullName)
		}
		return strings.Join(out, ", ")
	}
	slucajevi := []struct {
		naziv string
		f     repository.FiltarListova
		ime   string
	}{
		{"bez izbora", repository.FiltarListova{}, "Pero Perić, Ana Anić, Ivo Ivić, Iva Ivić"},
		{"sektor P", repository.FiltarListova{Sektor: "P"}, "Pero Perić, Ana Anić"},
		{"sektor P, područje 2", repository.FiltarListova{Sektor: "P", AreaID: 2}, "Ana Anić"},
		{"područje 1", repository.FiltarListova{AreaID: 1}, "Pero Perić"},
		{"sektor R", repository.FiltarListova{Sektor: "R"}, ""},
	}
	for _, s := range slucajevi {
		if got := imena(uOdabiru(svi, s.f)); got != s.ime {
			t.Errorf("%s: %q, očekivano %q", s.naziv, got, s.ime)
		}
	}
}
