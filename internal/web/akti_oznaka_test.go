package web

import (
	"context"
	"errors"
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

// Dva akta s istom oznakom (s dva čvora) u popisu akata stoje crveno, s
// oznakom „ista oznaka kao akt …”, dok Voditelj Centra obrane od poplava
// ili zamjenik sukob ne označi riješenim (Odluke (8. 10. 2026.), pitanje 4)
func TestIstaOznakaUPopisuIRjesenjeKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "oznaka.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (15, 'B', 'Mali sliv Primjerica', 'VGI Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "laptop")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	aktiRepo := repository.NewAktiRepository(baza, rec)
	akti := service.NewAktService(aktiRepo, stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users, nil, "laptop")
	stations := service.NewStationService(stationRepo, service.NewSectionService(sectionRepo, service.NewSSEBroker()), service.NewSSEBroker())
	ctx := context.Background()

	// isti broj na dva čvora: B-7/2026 s laptopa i s poslužitelja
	vrijedi := time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb)
	akt := func(postaja, cvor, kod string, stupanj models.DefensePhase) *models.Akt {
		a := &models.Akt{Sektor: "B", AreaID: 15, Broj: 7, Godina: 2026, Radnja: models.AktUspostava, Stupanj: stupanj, StationName: postaja,
			Status: models.AktOvjeren, Cvor: cvor, OvjeraKod: kod, Ovjerio: "Pero Perić", Vrijedi: vrijedi, Dionice: []models.AktDionica{{Code: "B.15.1"}}}
		if err := aktiRepo.SaveAkt(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	batina := akt("Batina", "laptop", "3FA9C21B0D", models.PhasePrep)
	vukovar := akt("Vukovar", "unraid", "7C11E0A2B4", models.PhaseRegular)
	// isti broj dvaput na istom čvoru (npr. prije brojača akata)
	aljmas, dalj := akt("Aljmaš", "laptop", "11AA22BB33", models.PhaseEmergency), akt("Dalj", "laptop", "44CC55DD66", models.PhaseState)
	aljmas.Broj, dalj.Broj = 8, 8
	for _, a := range []*models.Akt{aljmas, dalj} {
		if err := aktiRepo.SaveAkt(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	sektor := "B"
	voditelj := &models.User{ID: uuid.New(), Username: "aanic", FullName: "Ana Anić", IsActive: true,
		Duties: []models.Duty{{Role: models.RoleCopLeader, SectorID: &sektor, IsActive: true}}}
	vOvl := models.NewUserPermissions(*voditelj)
	operater := &models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", IsActive: true,
		Duties: []models.Duty{{Role: models.RoleSectorLeader, SectorID: &sektor, IsActive: true}}}
	oOvl := models.NewUserPermissions(*operater)

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
	mux.HandleFunc("GET /akti", h.ShowPopis)
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	mux.HandleFunc("POST /akti/{id}/oznaka", h.HandleRijesiOznaku)
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
	ima := func(w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "template:") {
			t.Fatalf("%s: %d\n%s", sto, w.Code, w.Body.String())
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}

	popis := zovi(operater, oOvl, http.MethodGet, "/akti", nil)
	ima(popis, "popis sa sukobom", `class="akt-sukob"`, "ista oznaka kao akt", "Vukovar, RJEŠENJE o uspostavi", "kod 7C11E0A2B4", "čvor unraid", "čeka odluku")
	if n := strings.Count(popis.Body.String(), `class="akt-sukob"`); n != 4 {
		t.Errorf("akti iste oznake (dva para) moraju biti crveni, a crvenih redaka je %d", n)
	}
	ima(zovi(operater, oOvl, http.MethodGet, "/akti/"+aljmas.ID, nil), "isti broj na istom čvoru", "Ista oznaka kao akt", "Dalj", "ovjereni na istom čvoru")
	// rukovoditelj sektora vidi sukob, ali ga ne rješava
	stranica := zovi(operater, oOvl, http.MethodGet, "/akti/"+batina.ID, nil)
	ima(stranica, "akt rukovoditelju", "Ista oznaka kao akt", "Vukovar", "ista oznaka kao drugi akt", "Voditelj Centra obrane od poplava ili zamjenik",
		"ovjereni na različitim čvorovima")
	if strings.Contains(stranica.Body.String(), "potpisan") {
		t.Error("akti bez potpisa čvora ne smiju se opisati kao potpisani")
	}
	if strings.Contains(stranica.Body.String(), "Označi riješenim") {
		t.Error("rukovoditelj sektora ne rješava sukob oznake")
	}
	if l := zovi(operater, oOvl, http.MethodPost, "/akti/"+batina.ID+"/oznaka", url.Values{"napomena": {"riješeno"}}).Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("rješenje bez ovlasti: %s", l)
	}
	ima(zovi(voditelj, vOvl, http.MethodGet, "/akti/"+batina.ID, nil), "akt voditelju COP-a", "Označi riješenim", `name="napomena"`)
	if l := zovi(voditelj, vOvl, http.MethodPost, "/akti/"+batina.ID+"/oznaka", url.Values{"napomena": {"Primateljima poslan dopis s ispravkom oznake."}}).Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("rješenje sukoba: %s", l)
	}
	popis = zovi(operater, oOvl, http.MethodGet, "/akti", nil)
	ima(popis, "popis nakon rješenja", "ista oznaka kao akt", "riješeno")
	if n := strings.Count(popis.Body.String(), `class="akt-sukob"`); n != 2 {
		t.Errorf("riješen sukob više nije crven, a nerješen ostaje: %d crvenih redaka", n)
	}
	ima(zovi(operater, oOvl, http.MethodGet, "/akti/"+vukovar.ID, nil), "drugi akt nakon rješenja", "Sukob je riješen: Primateljima poslan dopis", "Ana Anić")
	if s, _ := akti.SukobiOznaka(ctx); !s[vukovar.ID].Rijesen() || !s[batina.ID].Rijesen() {
		t.Error("rješenje ne obuhvaća oba akta iste oznake")
	}
	if w := zovi(voditelj, vOvl, http.MethodPost, "/akti/nema-ga/oznaka", url.Values{"napomena": {"x"}}); w.Code != http.StatusNotFound {
		t.Errorf("rješenje akta kojeg nema: %d", w.Code)
	}
}

func TestUzGresku(t *testing.T) {
	if got := uzGresku("Spremljeno.", "Sukobi", nil); got != "Spremljeno." {
		t.Errorf("bez greške poruka ostaje: %q", got)
	}
	if got := uzGresku("", "Akti s istom oznakom nisu provjereni", errors.New("baza je zauzeta")); got != "Akti s istom oznakom nisu provjereni: baza je zauzeta" {
		t.Errorf("greška uz praznu poruku: %q", got)
	}
}
