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

// Obrazac bilješke uz knjigu vodočuvara (zadani dan: danas) pokazuje se po
// današnjem pravu, kao što ga provjerava i upis, a ne po najnovijem listu
// u knjizi (Odluke (8. 10. 2026.), pitanje 1: rukovoditelj dionice upisuje
// bilješku samo na list vezan uz svoju dionicu). Pero Perić čuvao je P.1.1,
// a jučer navečer prešao je na P.1.2; najnoviji list mu je od prekjučer.
// Rukovoditelj P.1.2 dobiva obrazac i upis za danas prolazi; rukovoditelj
// P.1.1 knjigu i dalje čita, ali obrasca nema, a upis za danas se odbija.
// Podaci su izmišljeni.
func TestObrazacBiljeskeUKnjiziPoDanasnjemPravu(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "knjiga.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica')`,
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
	pero := osoba("pperic", "Pero Perić", models.Duty{Title: "Vodočuvar P.1.1", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	prvi := osoba("iivic", "Ivo Ivić", models.Duty{Title: "Rukovoditelj dionice P.1.1", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	drugi := osoba("aanic", "Ana Anić", models.Duty{Title: "Rukovoditelj dionice P.1.2", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.2"})

	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	// Perino zaduženje postoji od prije mjesec dana, a jučer u 18 h
	// izmijenjeno je u mjestu na P.1.2
	for _, q := range []string{
		`UPDATE record_versions SET created_at = ? WHERE entity = 'duties' AND entity_id IN (SELECT id FROM duties WHERE user_id = ?)`,
		`UPDATE duties SET created_at = ? WHERE user_id = ?`,
	} {
		if _, err := baza.Exec(q, danas.AddDate(0, 0, -30).UTC(), pero.ID.String()); err != nil {
			t.Fatal(err)
		}
	}
	zaduzenja, err := userRepo.GetDutiesForUser(pero.ID)
	if err != nil || len(zaduzenja) != 1 {
		t.Fatalf("Perina zaduženja: %v %+v", err, zaduzenja)
	}
	zaduzenja[0].Title, zaduzenja[0].SectionCodes = "Vodočuvar P.1.2", "P.1.2"
	if err := userRepo.UpdateDuty(&zaduzenja[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`UPDATE record_versions SET created_at = ? WHERE entity = 'duties' AND entity_id = ? AND created_at > ?`,
		danas.Add(-6*time.Hour).UTC(), zaduzenja[0].ID.String(), danas.AddDate(0, 0, -1).UTC()); err != nil {
		t.Fatal(err)
	}

	vodRepo := repository.NewVodocuvarRepository(baza, rec)
	vod := service.NewVodocuvarService(vodRepo, users, "cop-primjer")
	vod.SetWeather(nil)
	sad := time.Now()
	prekjucer := danas.AddDate(0, 0, -2)
	if err := vodRepo.Save(ctx, &models.VodocuvarskiList{UserID: pero.ID.String(), Ime: pero.FullName, Sektor: p, AreaID: bp,
		Datum: prekjucer, Broj: 1, PredanoAt: &sad, Opis: "Obilazak nasipa"}); err != nil {
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
	orgRepo := repository.NewOrgRepository(baza, rec)
	h := NewVodocuvarHandler(func() *service.VodocuvarService { return vod }, users, func() *repository.OrgRepository { return orgRepo }, tmpl("vodocuvar.html"), tmpl("vodocuvar_list.html"))
	h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{} })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vodocuvar", h.ShowPopis)
	mux.HandleFunc("POST /vodocuvar/upis", h.HandleUpis)
	zovi := func(u *models.User, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(metoda, putanja, nil)
		if forma != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
	knjige := "/vodocuvar?sektor=P&godina=" + strconv.Itoa(prekjucer.Year())
	upis := url.Values{"vodocuvar": {pero.ID.String()}, "datum": {danas.Format("2006-01-02")}, "tekst": {"Javljeno s dionice"}}
	slucajevi := []struct {
		ime     string
		u       *models.User
		obrazac bool
	}{{"rukovoditelj P.1.2", drugi, true}, {"rukovoditelj P.1.1", prvi, false}}
	// najprije stranice, jer upis za danas otvara današnji list
	for _, x := range slucajevi {
		stranica := zovi(x.u, http.MethodGet, knjige, nil).Body.String()
		if !strings.Contains(stranica, "Pero Perić") {
			t.Errorf("%s ne vidi Perinu knjigu", x.ime)
		}
		if got := strings.Contains(stranica, `action="/vodocuvar/upis"`); got != x.obrazac {
			t.Errorf("%s: obrazac bilješke uz Perinu knjigu %v, očekivano %v", x.ime, got, x.obrazac)
		}
	}
	for _, x := range slucajevi {
		l := mustUnescape(zovi(x.u, http.MethodPost, "/vodocuvar/upis", upis).Header().Get("Location"))
		if strings.Contains(l, "success") != x.obrazac {
			t.Errorf("%s: upis za danas %s, a obrazac %v", x.ime, l, x.obrazac)
		}
	}
}
