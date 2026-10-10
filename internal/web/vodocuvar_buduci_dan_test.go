package web

import (
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
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

// okolinaDnevnika je vodočuvarski dnevnik kroz rute, s izmišljenim osobama:
// Pero Perić je vodočuvar, Ana Anić rukovoditeljica branjenog područja 1.
type okolinaDnevnika struct {
	vod       *service.VodocuvarService
	repo      *repository.VodocuvarRepository
	mux       *http.ServeMux
	users     *service.UserService
	vodocuvar *models.User
	rukovodi  *models.User
	baza      *sql.DB
	rec       *ledger.Recorder
	vodH      *VodocuvarHandler
	tmpl      func(stranica string) *template.Template
}

func novaOkolinaDnevnika(t *testing.T) *okolinaDnevnika {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "dnevnik.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica', 'Primjerska 1', '01/000-000', 'cop@primjer.hr')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Primjerica', 'VGI Primjerica', 'Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'd.o. r. Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop-primjer")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
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
	o := &okolinaDnevnika{users: users, baza: baza, rec: rec}
	o.vodocuvar = osoba("pperic", "Pero Perić", models.Duty{Title: "Vodočuvar Primjerica", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	o.rukovodi = osoba("aanic", "Ana Anić", models.Duty{Title: "Rukovoditeljica BP 1", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &bp})

	orgRepo := repository.NewOrgRepository(baza, rec)
	o.repo = repository.NewVodocuvarRepository(baza, rec)
	o.vod = service.NewVodocuvarService(o.repo, users, "cop-primjer")
	o.vod.SetOrg(orgRepo)
	o.vod.SetWeather(nil)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewVodocuvarHandler(func() *service.VodocuvarService { return o.vod }, users, func() *repository.OrgRepository { return orgRepo }, tmpl("vodocuvar.html"), tmpl("vodocuvar_list.html"))
	h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{} })
	o.vodH, o.tmpl = h, tmpl
	o.mux = http.NewServeMux()
	o.mux.HandleFunc("GET /vodocuvar", h.ShowPopis)
	o.mux.HandleFunc("GET /vodocuvar/dan", h.ShowDan)
	o.mux.HandleFunc("POST /vodocuvar/spremi", h.HandleSpremi)
	o.mux.HandleFunc("POST /vodocuvar/upis", h.HandleUpis)
	o.mux.HandleFunc("GET /vodocuvar/{id}", h.ShowList)
	return o
}

// zovi šalje zahtjev u ime osobe, s dužnostima kako ih vidi prijava
func (o *okolinaDnevnika) zovi(t *testing.T, u *models.User, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if forma != nil {
		r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(metoda, putanja, nil)
	}
	cijeli, err := o.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("%s: %v", u.FullName, err)
	}
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

func danZaTest(pomak int) string {
	return time.Now().In(models.Zagreb).AddDate(0, 0, pomak).Format("2006-01-02")
}

// Odluke (8. 10. 2026.), pitanja 5 i 6, kroz rute: rukovoditeljica upisuje
// bilješku na list za budući dan (nacrt bez broja), najviše 30 dana
// unaprijed; vodočuvar takav nacrt vidi kao „Nacrt bez broja”, s gumbom
// „Potpiši i predaj” koji se ne može pritisnuti, a predaja izravnim POST-om
// se odbija i upisano ostaje nacrt.
func TestBuduciDanKrozRute(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	prekosutra := danZaTest(2)
	dan, _ := time.ParseInLocation("2006-01-02", prekosutra, models.Zagreb)

	l := mustUnescape(o.zovi(t, o.rukovodi, http.MethodPost, "/vodocuvar/upis", url.Values{"vodocuvar": {o.vodocuvar.ID.String()}, "datum": {prekosutra}, "tekst": {"ophodnja nasipa svaka 2 sata"}}).Header().Get("Location"))
	if !strings.Contains(l, "na nacrtu lista bez broja od "+dan.Format("02.01.2006.")+" (broj dobiva kad taj dan počne)") {
		t.Fatalf("upis na budući dan: %s", l)
	}
	l = mustUnescape(o.zovi(t, o.rukovodi, http.MethodPost, "/vodocuvar/upis", url.Values{"vodocuvar": {o.vodocuvar.ID.String()}, "datum": {danZaTest(40)}, "tekst": {"predaleko"}}).Header().Get("Location"))
	if !strings.Contains(l, "error") || !strings.Contains(l, "najviše 30 dana unaprijed") {
		t.Errorf("upis dalje od 30 dana: %s", l)
	}

	// popis godine nacrta: 30. i 31. prosinca to je iduća godina
	godinaNacrta := "/vodocuvar?godina=" + dan.Format("2006")
	popis := o.zovi(t, o.rukovodi, http.MethodGet, godinaNacrta, nil).Body.String()
	for _, x := range []string{`max="` + danZaTest(30) + `"`, "Nacrt bez broja"} {
		if !strings.Contains(popis, x) {
			t.Errorf("knjige vodočuvara bez %q", x)
		}
	}
	if moj := o.zovi(t, o.vodocuvar, http.MethodGet, godinaNacrta, nil).Body.String(); !strings.Contains(moj, "Nacrt bez broja") || strings.Contains(moj, "(nepredan)") {
		t.Error("moj dnevnik: budući nacrt mora pisati „Nacrt bez broja”")
	}

	list := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar/dan?datum="+prekosutra, nil).Body.String()
	for _, x := range []string{"nacrt bez broja", "ophodnja nasipa svaka 2 sata", "potpisuje i predaje tek od 00:00 " + dan.Format("02.01.2006."), "disabled"} {
		if !strings.Contains(list, x) {
			t.Errorf("budući list bez %q", x)
		}
	}
	if strings.Contains(list, `value="predaj"`) {
		t.Error("budući list ne smije imati gumb predaje koji šalje obrazac")
	}
	forma := url.Values{"datum": {prekosutra}, "od": {"07:00"}, "do": {"15:00"}, "opis": {"plan obilaska"}, "radnja": {"predaj"}}
	l = mustUnescape(o.zovi(t, o.vodocuvar, http.MethodPost, "/vodocuvar/spremi", forma).Header().Get("Location"))
	if !strings.Contains(l, "error") || !strings.Contains(l, "tek kad taj dan počne") {
		t.Errorf("predaja budućeg lista: %s", l)
	}
	if spremljen, err := o.repo.ZaDan(context.Background(), o.vodocuvar.ID.String(), dan); err != nil || spremljen == nil || spremljen.Predan() || spremljen.Broj != 0 || spremljen.Opis != "plan obilaska" {
		t.Errorf("nakon odbijene predaje: %+v %v", spremljen, err)
	}

	// današnji list ima gumb predaje
	if danas := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar/dan?datum="+danZaTest(0), nil).Body.String(); !strings.Contains(danas, `value="predaj"`) || strings.Contains(danas, "nacrt bez broja") {
		t.Error("današnji list mora imati gumb „Potpiši i predaj”")
	}
	// dalje od 30 dana list se ne otvara
	if l := mustUnescape(o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar/dan?datum="+danZaTest(40), nil).Header().Get("Location")); !strings.Contains(l, "najviše 30 dana unaprijed") {
		t.Errorf("list dalje od 30 dana: %s", l)
	}
}

// Ispis knjige: preneseni listovi prvi, zatim po broju, a nacrti bez broja
// na kraju, po danu.
func TestRedoslijedUKnjizi(t *testing.T) {
	d := func(dan int) time.Time { return time.Date(2026, time.March, dan, 0, 0, 0, 0, models.Zagreb) }
	prenesen := models.VodocuvarskiList{ID: "p", Datum: d(20), Rekonstrukcija: true}
	prvi := models.VodocuvarskiList{ID: "a", Datum: d(10), Broj: 1}
	drugi := models.VodocuvarskiList{ID: "b", Datum: d(9), Broj: 2}
	nacrt := models.VodocuvarskiList{ID: "n", Datum: d(1)}
	kasniji := models.VodocuvarskiList{ID: "k", Datum: d(5)}
	for _, s := range []struct {
		a, b  models.VodocuvarskiList
		prije bool
	}{
		{prenesen, prvi, true},
		{prvi, drugi, true},
		{drugi, prvi, false},
		{drugi, nacrt, true},
		{nacrt, drugi, false},
		{prenesen, nacrt, true},
		{nacrt, kasniji, true},
		{kasniji, nacrt, false},
	} {
		if got := prijeUKnjizi(s.a, s.b); got != s.prije {
			t.Errorf("%s prije %s: %v, očekivano %v", s.a.ID, s.b.ID, got, s.prije)
		}
	}
}

// Prijava upisana na list bez broja to kaže, umjesto broja 000.
func TestListPrijaveBezBroja(t *testing.T) {
	p := &models.PrijavaSTerena{ListBroj: 7, Godina: 2026, Datum: time.Date(2026, time.March, 10, 9, 0, 0, 0, models.Zagreb)}
	if got := listPrijave(p); got != "007/2026" {
		t.Errorf("list s brojem: %q", got)
	}
	p.ListBroj = 0
	if got := listPrijave(p); got != "od 10.03.2026. (nacrt bez broja)" {
		t.Errorf("list bez broja: %q", got)
	}
}

// Popis nudi godine od godine najdaljeg dana za koji se list smije
// pripremiti unaprijed (30 dana) do najstarijeg lista: od 2. prosinca nacrt
// smije biti za siječanj iduće godine, pa popis nudi i tu godinu, inače
// takav nacrt (i rukovoditeljev „Upiši” na njega) ne bi bio ni u jednom
// popisu.
func TestGodineKnjigaSIducomGodinom(t *testing.T) {
	sat := func(mjesec time.Month, dan, sat int) time.Time {
		return time.Date(2026, mjesec, dan, sat, 0, 0, 0, models.Zagreb)
	}
	for _, s := range []struct {
		sad        time.Time
		najstarija int
		godine     []int
	}{
		{sat(time.December, 1, 23), 2025, []int{2026, 2025}},
		{sat(time.December, 2, 0), 2025, []int{2027, 2026, 2025}},
		{sat(time.December, 31, 23), 2026, []int{2027, 2026}},
		{sat(time.March, 10, 12), 2026, []int{2026}},
	} {
		if got := godineKnjiga(s.sad, s.najstarija); !reflect.DeepEqual(got, s.godine) {
			t.Errorf("%s: godine %v, očekivano %v", s.sad.Format("02.01.2006. 15:04"), got, s.godine)
		}
	}
}
