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

var otisakNaStranici = regexp.MustCompile(`name="otisak" value="([0-9a-f]+)"`)

// otisakSaStranice je otisak popisa primatelja koji stranica nacrta šalje uz
// ovjeru: ono što je osoba vidjela (Odluke (8. 10. 2026.), pitanje 19)
func otisakSaStranice(t *testing.T, stranica string) string {
	t.Helper()
	m := otisakNaStranici.FindStringSubmatch(stranica)
	if m == nil {
		t.Fatal("stranica nacrta ne nosi otisak popisa primatelja za ovjeru")
	}
	return m[1]
}

// Popis primatelja na stranici nacrta (Odluke (8. 10. 2026.), pitanje 19):
// upozorenje o dionici bez teritorija vidi se, ovjera dok ono stoji traži
// kvačicu, pri ovjeri se popis slaže ponovno i razlika se pokaže prije
// potvrde, a gumb „Ponovno složi primatelje” daje nacrtu novi popis.
func TestPrimateljiNacrtaKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "primatelji.db"))
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
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec),
		repository.NewReadingRepository(baza, rec), users, nil, "cop")
	stations := service.NewStationService(stationRepo, service.NewSectionService(sectionRepo, service.NewSSEBroker()), service.NewSSEBroker())
	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, 'B.34.1', ?, ?)`, uuid.NewString(), st.ID.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
	ana := &models.User{ID: uuid.New(), Username: "aanic", FullName: "Ana Anić", IsGlobalAdmin: true, IsActive: true}
	perms := &models.UserPermissions{IsGlobalAdmin: true, User: *ana}
	marko := &models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", IsActive: true}

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
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	mux.HandleFunc("POST /akti/{id}/ovjeri", h.HandleOvjeri)
	mux.HandleFunc("POST /akti/{id}/slozi-primatelje", h.HandleSloziPrimatelje)
	zovi := func(u *models.User, p *models.UserPermissions, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
		if forma != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, u), contextKeyPerms, p)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	stranica := func(id string, dijelovi ...string) string {
		t.Helper()
		w := zovi(ana, perms, http.MethodGet, "/akti/"+id, nil)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "template:") {
			t.Fatalf("stranica akta: %d\n%s", w.Code, w.Body.String())
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("stranica akta nema %q", d)
			}
		}
		return w.Body.String()
	}
	ovjeri := func(forma url.Values) string {
		t.Helper()
		return mustUnescape(zovi(ana, perms, http.MethodPost, "/akti/"+forma.Get("id")+"/ovjeri", forma).Header().Get("Location"))
	}

	a, err := akti.Pripremi(ctx, perms, ana, service.ZahtjevAkta{StationID: st.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := akti.Spremi(ctx, perms, a); err != nil {
		t.Fatal(err)
	}
	nacrt := stranica(a.ID, "Primatelji pri ovjeri", "Popis primatelja u nacrtu nije potpun", "dionice bez upisanog teritorija (naselja i općine): B.34.1",
		"Popis složen sada jednak je popisu u nacrtu", `name="potvrda_primatelja" value="1" form="ovjera-obrazac" required`, "Ponovno složi primatelje",
		"Sken nosi popis primatelja s ispisa, uz upozorenje koje i dalje stoji: svejedno ovjeravam")
	vidjela := otisakSaStranice(t, nacrt)
	// tko s nacrtom ne radi, vidi upozorenje, ali ne i radnje
	if w := zovi(marko, models.NewUserPermissions(*marko), http.MethodGet, "/akti/"+a.ID, nil).Body.String(); !strings.Contains(w, "Popis primatelja u nacrtu nije potpun") ||
		strings.Contains(w, "Ponovno složi primatelje") || strings.Contains(w, `name="otisak"`) {
		t.Error("nacrt onome tko s njim ne radi: upozorenje da, radnje ne")
	}

	// ovjera bez otiska (stranica nije otvorena) i bez kvačice ne prolazi
	if l := ovjeri(url.Values{"id": {a.ID}, "potvrda_primatelja": {"1"}}); !strings.Contains(l, "pregledajte popis primatelja") {
		t.Errorf("ovjera bez otiska: %s", l)
	}
	if l := ovjeri(url.Values{"id": {a.ID}, "otisak": {vidjela}}); !strings.Contains(l, "Ovjera traži izričitu potvrdu") || !strings.Contains(l, "B.34.1") {
		t.Errorf("ovjera bez potvrde dok stoji upozorenje: %s", l)
	}
	if n, _ := akti.Get(ctx, a.ID); n.Ovjeren() {
		t.Fatal("nacrt je ovjeren bez potvrde")
	}

	// registar se dopuni: stranica pokazuje razliku i pita uz ovjeru, a
	// stari otisak više ne vrijedi
	if err := akti.SpremiPrimatelja(ctx, perms, &models.Primatelj{Sektor: "B", Naziv: "Ravnateljstvo civilne zaštite", Email: "rcz@primjer.hr", Aktivan: true}); err != nil {
		t.Fatal(err)
	}
	stranica(a.ID, "Razlika prema popisu u nacrtu:", "+ Ravnateljstvo civilne zaštite &lt;rcz@primjer.hr&gt;", "razlikuje se od nacrta (dodano 1, uklonjeno 0)",
		"a popis složen sada bio bi drukčiji (dodano 1, uklonjeno 0): svejedno ovjeravam")
	if l := ovjeri(url.Values{"id": {a.ID}, "otisak": {vidjela}, "potvrda_primatelja": {"1"}}); !strings.Contains(l, "otkad je stranica otvorena") {
		t.Errorf("ovjera sa zastarjelim popisom: %s", l)
	}

	// „Ponovno složi primatelje”: smije tko radi s nacrtom
	if l := zovi(marko, models.NewUserPermissions(*marko), http.MethodPost, "/akti/"+a.ID+"/slozi-primatelje", url.Values{}).Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("slaganje bez ovlasti: %s", l)
	}
	if l := mustUnescape(zovi(ana, perms, http.MethodPost, "/akti/"+a.ID+"/slozi-primatelje", url.Values{}).Header().Get("Location")); !strings.Contains(l, "ponovno je složen") || !strings.Contains(l, "i dalje stoji") {
		t.Errorf("ponovno slaganje: %s", l)
	}
	if n, _ := akti.Get(ctx, a.ID); !strings.Contains(primateljiTekst(n), "Ravnateljstvo civilne zaštite") {
		t.Errorf("nacrt nema ponovno složen popis: %s", primateljiTekst(n))
	}
	nacrt = stranica(a.ID, "Popis složen sada jednak je popisu u nacrtu")

	// ovjera uz kvačicu: potvrda se bilježi i pokazuje uz akt
	if l := ovjeri(url.Values{"id": {a.ID}, "otisak": {otisakSaStranice(t, nacrt)}, "potvrda_primatelja": {"1"}}); !strings.Contains(l, "success") {
		t.Fatalf("ovjera uz potvrdu: %s", l)
	}
	ovjeren, _ := akti.Get(ctx, a.ID)
	if !ovjeren.Ovjeren() || !ovjeren.ImaPotvrdu(models.PotvrdaPrimatelji) {
		t.Fatalf("ovjeren akt bez potvrde popisa primatelja: %+v", ovjeren.Potvrde)
	}
	if s := stranica(a.ID, "Potvrđeno uz akt:", "Ovjereno uz upozorenje o popisu primatelja: dionice bez upisanog teritorija"); strings.Contains(s, "Primatelji pri ovjeri") || strings.Contains(s, "Ponovno složi primatelje") {
		t.Error("ovjeren akt više ne nudi slaganje popisa")
	}
	if l := mustUnescape(zovi(ana, perms, http.MethodPost, "/akti/"+a.ID+"/slozi-primatelje", url.Values{}).Header().Get("Location")); !strings.Contains(l, "ostaje onaj s kojim je ovjeren") {
		t.Errorf("slaganje popisa ovjerenog akta: %s", l)
	}
	if l := ovjeri(url.Values{"id": {a.ID}}); !strings.Contains(l, "već ovjeren") {
		t.Errorf("ponovna ovjera ovjerenog akta: %s", l)
	}
}

func primateljiTekst(a *models.Akt) string {
	var out []string
	for _, p := range a.Primatelji {
		out = append(out, p.Naziv)
	}
	return strings.Join(out, "; ")
}
