package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
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

// „Vrijedi od” izvan uobičajenog raspona: obrazac se vraća s upisanim
// vremenom i pitanjem, a nacrt nastaje tek uz izričitu potvrdu
// (Odluke (8. 10. 2026.), pitanje 17)
func TestPotvrdaVrijediOdKrozObrazac(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "vrijedi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Primjerica', 'VGI Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'd.o. r. Primjerica', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.2', 34, 'B', 'l.o. r. Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users, nil, "cop")
	otvoren := time.Now().AddDate(0, 0, -10)
	akti.SetObrana(func(context.Context, string) *models.Journal {
		return &models.Journal{ID: "dnevnik-cop", CentarSektor: "B", StartedAt: &otvoren}
	}, nil)
	stations := service.NewStationService(stationRepo, service.NewSectionService(sectionRepo, service.NewSSEBroker()), service.NewSSEBroker())
	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"B.34.1", "B.34.2"} {
		if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`, uuid.NewString(), d, st.ID.String(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	ana := &models.User{ID: uuid.New(), Username: "aanic", FullName: "Ana Anić", IsGlobalAdmin: true, IsActive: true}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *ana}

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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /akti/novi", h.ShowForm)
	mux.HandleFunc("POST /akti/novi", h.HandleCreate)
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	mux.HandleFunc("POST /akti/{id}/ovjeri", h.HandleOvjeri)
	zovi := func(metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
		if forma != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, ana), contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	forma := func(vrijedi time.Time, potvrda string) url.Values {
		return url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"PRIPREMNO"},
			"vrijedi": {vrijedi.In(models.Zagreb).Format("2006-01-02T15:04")}, "potvrda_vrijedi": {potvrda}}
	}

	obrazac := zovi(http.MethodGet, "/akti/novi?station="+st.ID.String(), nil).Body.String()
	if !strings.Contains(obrazac, "Više od 24 sata unatrag") || strings.Contains(obrazac, `type="checkbox" name="potvrda_vrijedi"`) {
		t.Errorf("obrazac bez pitanja nudi potvrdu ili ne opisuje granice")
	}

	dvaDana := time.Now().Add(-48 * time.Hour)
	l := zovi(http.MethodPost, "/akti/novi", forma(dvaDana, "")).Header().Get("Location")
	natrag, _ := url.Parse(l)
	if natrag.Path != "/akti/novi" || natrag.Query().Get("potvrdi") != "vrijedi" || !strings.Contains(natrag.Query().Get("error"), "jeste li sigurni?") {
		t.Fatalf("bez potvrde obrazac se vraća s pitanjem: %s", l)
	}
	obrazac = zovi(http.MethodGet, natrag.String(), nil).Body.String()
	for _, d := range []string{`type="checkbox" name="potvrda_vrijedi"`, `value="` + dvaDana.In(models.Zagreb).Format("2006-01-02T15:04") + `"`, "jeste li sigurni?"} {
		if !strings.Contains(obrazac, d) {
			t.Errorf("vraćeni obrazac nema %q", d)
		}
	}
	l = zovi(http.MethodPost, "/akti/novi", forma(dvaDana, "1")).Header().Get("Location")
	if !strings.Contains(l, "success=") {
		t.Fatalf("uz potvrdu nacrt nastaje: %s", l)
	}
	a, _ := akti.Get(ctx, strings.TrimPrefix(strings.SplitN(l, "?", 2)[0], "/akti/"))
	if a == nil || !a.ImaPotvrdu(models.PotvrdaVrijedi) || a.Potvrde[0].Tko != "Ana Anić" {
		t.Fatalf("potvrda nije zabilježena: %+v", a)
	}
	if s := zovi(http.MethodGet, "/akti/"+a.ID, nil).Body.String(); !strings.Contains(s, "Potvrđeno uz akt:") || !strings.Contains(s, "jeste li sigurni? Potvrđeno. (Ana Anić,") {
		t.Error("stranica akta ne pokazuje zabilježenu potvrdu")
	}
	// tvrda granica: potvrda ne pomaže, a obrazac je i ne nudi
	l = zovi(http.MethodPost, "/akti/novi", forma(time.Now().AddDate(0, 0, 10), "1")).Header().Get("Location")
	if natrag, _ = url.Parse(l); !strings.Contains(natrag.Query().Get("error"), "7 dana unaprijed") || natrag.Query().Get("potvrdi") != "" {
		t.Errorf("deset dana unaprijed: %s", l)
	}
	// nacrt sastavljen bez potvrde dok je „Vrijedi od” bio sada; dva dana
	// poslije ovjera pita i bilježi potvrdu
	l = zovi(http.MethodPost, "/akti/novi", forma(time.Now().Add(-time.Hour), "")).Header().Get("Location")
	id := strings.TrimPrefix(strings.SplitN(l, "?", 2)[0], "/akti/")
	if _, err := baza.Exec(`UPDATE akti SET vrijedi = ? WHERE id = ?`, dvaDana.UTC(), id); err != nil {
		t.Fatal(err)
	}
	stranica := zovi(http.MethodGet, "/akti/"+id, nil).Body.String()
	if !strings.Contains(stranica, `name="potvrda_vrijedi" value="1" form="ovjera-obrazac" required`) || !strings.Contains(stranica, "jeste li sigurni?") {
		t.Fatal("stranica nacrta ne pita za „Vrijedi od” pri ovjeri")
	}
	otisak := regexp.MustCompile(`name="otisak" value="([^"]+)"`).FindStringSubmatch(stranica)
	if otisak == nil {
		t.Fatal("stranica nacrta nema otisak popisa primatelja")
	}
	ovjera := url.Values{"otisak": {otisak[1]}, "potvrda_primatelja": {"1"}}
	if l = zovi(http.MethodPost, "/akti/"+id+"/ovjeri", ovjera).Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("ovjera bez potvrde „Vrijedi od”: %s", l)
	}
	ovjera.Set("potvrda_vrijedi", "1")
	if l = zovi(http.MethodPost, "/akti/"+id+"/ovjeri", ovjera).Header().Get("Location"); strings.Contains(l, "error=") {
		t.Fatalf("ovjera uz potvrdu: %s", l)
	}
	if b, _ := akti.Get(ctx, id); b == nil || !b.Ovjeren() || !b.ImaPotvrdu(models.PotvrdaVrijedi) || b.Potvrde[0].Tko != "Ana Anić" {
		t.Errorf("potvrda pri ovjeri: %+v", b)
	}
	if s := zovi(http.MethodGet, "/akti/"+id, nil).Body.String(); strings.Contains(s, `name="potvrda_vrijedi"`) || !strings.Contains(s, "Potvrđeno pri ovjeri.") {
		t.Error("ovjeren akt pokazuje potvrdu, a ne pita ponovno")
	}

	// obrazac vraćen s pitanjem zadrži sve upisano (dionice, osnovu,
	// napomenu), pa nacrt uz potvrdu nastaje iz istog unosa
	unos := forma(dvaDana, "")
	unos["dionica"] = []string{"B.34.2"}
	unos.Set("tendencija", models.TendencijaOpadanje)
	unos.Set("prognoza", "najavljen porast do 600 cm")
	unos.Set("napomena", "Napomena za Ivu Ivić")
	natrag, _ = url.Parse(zovi(http.MethodPost, "/akti/novi", unos).Header().Get("Location"))
	if natrag.Query().Get("potvrdi") != "vrijedi" {
		t.Fatalf("bez potvrde obrazac se vraća s pitanjem: %s", natrag)
	}
	obrazac = zovi(http.MethodGet, natrag.String(), nil).Body.String()
	for _, d := range []string{`name="dionica" value="B.34.1">`, `name="dionica" value="B.34.2" checked>`, `value="najavljen porast do 600 cm"`,
		`>Napomena za Ivu Ivić</textarea>`, `value="` + models.TendencijaOpadanje + `" selected>`, `type="checkbox" name="potvrda_vrijedi"`} {
		if !strings.Contains(obrazac, d) {
			t.Errorf("vraćeni obrazac ne zadrži unos %q", d)
		}
	}
	unos.Set("potvrda_vrijedi", "1")
	l = zovi(http.MethodPost, "/akti/novi", unos).Header().Get("Location")
	if !strings.Contains(l, "success=") {
		t.Fatalf("uz potvrdu nacrt nastaje: %s", l)
	}
	if b, _ := akti.Get(ctx, strings.TrimPrefix(strings.SplitN(l, "?", 2)[0], "/akti/")); b == nil || len(b.Dionice) != 1 || b.Dionice[0].Code != "B.34.2" ||
		b.Prognoza != "najavljen porast do 600 cm" || b.Napomena != "Napomena za Ivu Ivić" {
		t.Errorf("nacrt iz zadržanog unosa: %+v", b)
	}
	// obrazac otvoren bez pitanja: sve dionice označene, ništa upisano
	obrazac = zovi(http.MethodGet, "/akti/novi?station="+st.ID.String(), nil).Body.String()
	if !strings.Contains(obrazac, `name="dionica" value="B.34.1" checked>`) || !strings.Contains(obrazac, `name="dionica" value="B.34.2" checked>`) ||
		strings.Contains(obrazac, `value="`+models.TendencijaOpadanje+`" selected>`) {
		t.Error("nov obrazac mora imati sve dionice označene i tendenciju prema očitanjima")
	}

	// zadano očitanje je zadnje, a zadržano ono koje je osoba odabrala
	if !(UnosObrasca{}).Ocitanje("o-1", true) || (UnosObrasca{}).Ocitanje("o-2", false) ||
		!(UnosObrasca{OcitanjeID: "o-2"}).Ocitanje("o-2", false) || (UnosObrasca{OcitanjeID: "o-2"}).Ocitanje("o-1", true) {
		t.Error("izbor očitanja u obrascu")
	}

	// neispravno vrijeme u upitu ne kvari obrazac
	var data AktiPageData
	obrazacVrijedi(url.Values{"vrijedi": {"sutra"}}, &data)
	if data.LocalValue == "sutra" || data.LocalValue == "" || data.TraziPotvrduVrijedi {
		t.Errorf("obrazac s neispravnim vremenom: %+v", data.LocalValue)
	}
}
