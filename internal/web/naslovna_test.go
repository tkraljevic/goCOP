package web

import (
	"context"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
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

// okolinaNaslovne je Naslovna nad praznom bazom sa sektorom B
type okolinaNaslovne struct {
	h   *DashboardHandler
	mux *http.ServeMux
}

func novaOkolinaNaslovne(t *testing.T) *okolinaNaslovne {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "naslovna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B — Dunav i donja Drava', 'VGO Osijek', 'COP Osijek')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	zid := service.NewZidService(rec, repository.NewJournalRepository(baza, rec), repository.NewSectionRepository(baza, rec), nil, userRepo, nil, repository.NewEpisodeRepository(baza, rec))
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl := func(stranica string) *template.Template {
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	o := &okolinaNaslovne{h: NewDashboardHandler(users, tmpl("dashboard.html"), tmpl("registri.html")), mux: http.NewServeMux()}
	o.h.SetZid(func() *service.ZidService { return zid }, nil, nil)
	o.mux.HandleFunc("GET /{$}", o.h.ShowDashboard)
	o.mux.HandleFunc("GET /naslovna/kartica/{id}", o.h.ShowKartica)
	return o
}

// zovi otvara stranicu kao osoba s danim pravima
func (o *okolinaNaslovne) zovi(putanja string, perms *models.UserPermissions) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, putanja, nil)
	ctx := context.WithValue(r.Context(), contextKeyUser, &perms.User)
	ctx = context.WithValue(ctx, contextKeyPerms, perms)
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, r.WithContext(ctx))
	return w
}

func dozvoleNaslovne(admin bool, uloge ...models.Role) *models.UserPermissions {
	u := models.User{ID: uuid.New(), FullName: "Pero Proba", IsGlobalAdmin: admin}
	for _, r := range uloge {
		u.Duties = append(u.Duties, models.Duty{Role: r, IsActive: true})
	}
	return &models.UserPermissions{User: u, IsGlobalAdmin: admin, AllowedSectors: map[string]bool{"B": true}}
}

// Globalni administrator dobije Naslovnu iz kartica, glavni rukovoditelj
// istu bez Sustava, a uloge koje još nisu prenesene dosadašnju stranicu
func TestNaslovnaPoUlogama(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	admin := o.zovi("/", dozvoleNaslovne(true))
	s := admin.Body.String()
	if admin.Code != http.StatusOK || strings.Contains(s, "template:") {
		t.Fatalf("administrator: %d\n%s", admin.Code, s)
	}
	for _, zeli := range []string{`id="kartica-stanje-obrane"`, "Sektor B", "preventivna obrana", `data-lijeno="/naslovna/kartica/za-rijesiti"`,
		`data-lijeno="/naslovna/kartica/vodostaji"`, `data-lijeno="/naslovna/kartica/sustav"`, "Još nema događanja.", "js/naslovna"} {
		if !strings.Contains(s, zeli) {
			t.Errorf("Naslovna administratora nema %q", zeli)
		}
	}
	glavni := o.zovi("/", dozvoleNaslovne(false, models.RoleNationalLeader)).Body.String()
	if !strings.Contains(glavni, `id="kartica-vrijeme"`) || strings.Contains(glavni, `id="kartica-sustav"`) {
		t.Error("glavni rukovoditelj nema Naslovnu bez Sustava")
	}
	if stari := o.zovi("/", dozvoleNaslovne(false, models.RoleWaterGuard)).Body.String(); !strings.Contains(stari, "dashboard-container") || strings.Contains(stari, "kartica-naslovne") {
		t.Error("vodočuvar nema dosadašnju Naslovnu")
	}
	if karticeUloge(nil) != nil {
		t.Error("bez prava nema kartica")
	}
}

// Spore kartice dođu cijele s izvorima; kartica koju uloga nema ne postoji,
// a bez izvora kartica kaže da podaci nisu dostupni
func TestSporeKarticeNaslovne(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	admin, glavni, vodocuvar := dozvoleNaslovne(true), dozvoleNaslovne(false, models.RoleNationalLeader), dozvoleNaslovne(false, models.RoleWaterGuard)
	for id, zeli := range map[string]string{
		"za-rijesiti":  "Nema ništa za riješiti.",
		"vodostaji":    "Vodostaji trenutačno nisu dostupni.",
		"vrijeme":      "Podaci DHMZ-a trenutačno nisu dostupni.",
		"jace-oborine": "Podaci o kiši trenutačno nisu dostupni.",
		"sustav":       "Stanje sustava trenutačno nije dostupno.",
	} {
		if w := o.zovi("/naslovna/kartica/"+id, admin); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), zeli) {
			t.Errorf("%s bez izvora: %d %s", id, w.Code, w.Body.String())
		}
	}
	for _, s := range []struct {
		putanja string
		perms   *models.UserPermissions
	}{{"/naslovna/kartica/sustav", glavni}, {"/naslovna/kartica/vrijeme", vodocuvar}, {"/naslovna/kartica/stanje-obrane", admin}, {"/naslovna/kartica/nema", admin}} {
		if w := o.zovi(s.putanja, s.perms); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, želi 404", s.putanja, w.Code)
		}
	}

	sad := time.Now()
	prijava := sad.AddDate(0, 0, -1)
	o.h.SetIzvori(IzvoriNaslovne{
		Kisa: func(context.Context) kisaSlivovaPodaci {
			return kisaSlivovaPodaci{Upozorenja: []SlivUKisi{{Naziv: "Vuka"}}}
		},
		Razmjena: func(context.Context) (RazmjenaStanje, bool) { return RazmjenaStanje{UMrezi: true}, true },
		Brojke:   func(context.Context) *BrojkePodataka { return &BrojkePodataka{Zapisa: 5, Letvi: 2} },
		Vrijeme:  func(*http.Request) PlocaVremena { return PlocaVremena{Mjesto: "Batina", Prognoza: "Sunčano."} },
		Letve: func(context.Context) ([]models.GaugeSummary, error) {
			return []models.GaugeSummary{{Name: "Batina", Phase: models.PhasePrep, Latest: &models.Reading{LevelCm: cmNaslovne(312), MeasuredAt: sad}}}, nil
		},
		Prije: func(context.Context, models.GaugeSummary) *int { return cmNaslovne(300) },
		Prava: func(context.Context) (int, string, error) { return 1, repository.OdlukaOvdje, nil },
		Djelatnici: func() ([]models.User, error) {
			return []models.User{{FullName: "Ana Proba", IsActive: true, LastLoginAt: &prijava}}, nil
		},
		Nepredani: func(context.Context, *models.UserPermissions, string) ([]models.VodocuvarskiList, error) {
			return []models.VodocuvarskiList{{Datum: sad.AddDate(0, 0, -2)}}, nil
		},
	})
	for id, zeli := range map[string]string{
		"za-rijesiti":  "1 promjena prava čeka potvrdu",
		"vodostaji":    "12 cm u 24 h",
		"vrijeme":      "Vrijeme · Batina",
		"jace-oborine": "Vuka",
		"sustav":       "5 zapisa · 2 letve",
	} {
		if w := o.zovi("/naslovna/kartica/"+id, admin); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), zeli) {
			t.Errorf("%s s izvorima: %d %q", id, w.Code, w.Body.String())
		}
	}
	if w := o.zovi("/naslovna/kartica/za-rijesiti", admin).Body.String(); !strings.Contains(w, "1 dnevni list nije predan") || !strings.Contains(w, "Ana Proba") {
		t.Errorf("za riješiti bez listova ili djelatnika: %s", w)
	}

	o.h.SetIzvori(IzvoriNaslovne{
		Letve:    func(context.Context) ([]models.GaugeSummary, error) { return nil, errors.New("baza zauzeta") },
		Razmjena: func(context.Context) (RazmjenaStanje, bool) { return RazmjenaStanje{}, false },
	})
	for id, zeli := range map[string]string{"vodostaji": "Vodostaji trenutačno nisu dostupni.", "sustav": "Stanje sustava trenutačno nije dostupno."} {
		if w := o.zovi("/naslovna/kartica/"+id, admin); !strings.Contains(w.Body.String(), zeli) {
			t.Errorf("%s s greškom izvora: %s", id, w.Body.String())
		}
	}

	o.h.tmpl = template.Must(template.New("prazan").Parse(""))
	if w := o.zovi("/naslovna/kartica/sustav", admin); w.Code != http.StatusInternalServerError {
		t.Errorf("kartica bez predloška: %d", w.Code)
	}
}
