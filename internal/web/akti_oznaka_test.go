package web

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
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
	"gocop/internal/razmjena"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// okolinaOznake je čvor s aktima sektora B, voditeljem COP-a koji sukob
// oznake rješava i rukovoditeljem sektora koji ga samo vidi, te rutama akata
type okolinaOznake struct {
	rec      *ledger.Recorder
	aktiRepo *repository.AktiRepository
	akti     *service.AktService
	mux      *http.ServeMux
	voditelj *models.User
	vOvl     *models.UserPermissions
	operater *models.User
	oOvl     *models.UserPermissions
}

func novaOkolinaOznake(t *testing.T) *okolinaOznake {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "oznaka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (15, 'B', 'Mali sliv Primjerica', 'VGI Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	o := &okolinaOznake{rec: ledger.New(baza, "laptop")}
	userRepo := repository.NewUserRepository(baza, o.rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, o.rec)
	stationRepo := repository.NewStationRepository(baza, o.rec)
	readingRepo := repository.NewReadingRepository(baza, o.rec)
	o.aktiRepo = repository.NewAktiRepository(baza, o.rec)
	o.akti = service.NewAktService(o.aktiRepo, stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, o.rec), readingRepo, users, nil, "laptop")
	stations := service.NewStationService(stationRepo, service.NewSectionService(sectionRepo, service.NewSSEBroker()), service.NewSSEBroker())

	sektor := "B"
	o.voditelj = &models.User{ID: uuid.New(), Username: "aanic", FullName: "Ana Anić", IsActive: true,
		Duties: []models.Duty{{Role: models.RoleCopLeader, SectorID: &sektor, IsActive: true}}}
	o.vOvl = models.NewUserPermissions(*o.voditelj)
	o.operater = &models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", IsActive: true,
		Duties: []models.Duty{{Role: models.RoleSectorLeader, SectorID: &sektor, IsActive: true}}}
	o.oOvl = models.NewUserPermissions(*o.operater)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewAktiHandler(func() *service.AktService { return o.akti }, users, stations, tmpl("akti.html"), tmpl("akt_form.html"), tmpl("akt.html"), tmpl("primatelji.html"))
	o.mux = http.NewServeMux()
	o.mux.HandleFunc("GET /akti", h.ShowPopis)
	o.mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	o.mux.HandleFunc("POST /akti/{id}/oznaka", h.HandleRijesiOznaku)
	return o
}

// akt sprema ovjeren akt sektora B, broj 7 u 2026., na zadanom čvoru
func (o *okolinaOznake) akt(t *testing.T, postaja, cvor, kod string, stupanj models.DefensePhase) *models.Akt {
	t.Helper()
	a := &models.Akt{Sektor: "B", AreaID: 15, Broj: 7, Godina: 2026, Radnja: models.AktUspostava, Stupanj: stupanj, StationName: postaja,
		Status: models.AktOvjeren, Cvor: cvor, OvjeraKod: kod, Ovjerio: "Pero Perić", Vrijedi: time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb),
		Dionice: []models.AktDionica{{Code: "B.15.1"}}}
	if err := o.aktiRepo.SaveAkt(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func (o *okolinaOznake) zovi(t *testing.T, u *models.User, p *models.UserPermissions, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
	if forma != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, u), contextKeyPerms, p)
	w := httptest.NewRecorder()
	o.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

func imaNaStranici(t *testing.T, w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
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

// Dva akta s istom oznakom (s dva čvora) u popisu akata stoje crveno, s
// oznakom „ista oznaka kao akt …”, dok Voditelj Centra obrane od poplava
// ili zamjenik sukob ne označi riješenim (Odluke (8. 10. 2026.), pitanje 4)
func TestIstaOznakaUPopisuIRjesenjeKrozRute(t *testing.T) {
	o := novaOkolinaOznake(t)
	ctx := context.Background()
	zovi, ima := func(u *models.User, p *models.UserPermissions, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
		t.Helper()
		return o.zovi(t, u, p, metoda, putanja, forma)
	}, func(w *httptest.ResponseRecorder, sto string, dijelovi ...string) {
		t.Helper()
		imaNaStranici(t, w, sto, dijelovi...)
	}
	voditelj, vOvl, operater, oOvl, akti := o.voditelj, o.vOvl, o.operater, o.oOvl, o.akti

	// isti broj na dva čvora: B-7/2026 s laptopa i s poslužitelja
	batina := o.akt(t, "Batina", "laptop", "3FA9C21B0D", models.PhasePrep)
	vukovar := o.akt(t, "Vukovar", "unraid", "7C11E0A2B4", models.PhaseRegular)
	// isti broj dvaput na istom čvoru (npr. prije brojača akata)
	aljmas, dalj := o.akt(t, "Aljmaš", "laptop", "11AA22BB33", models.PhaseEmergency), o.akt(t, "Dalj", "laptop", "44CC55DD66", models.PhaseState)
	aljmas.Broj, dalj.Broj = 8, 8
	for _, a := range []*models.Akt{aljmas, dalj} {
		if err := o.aktiRepo.SaveAkt(ctx, a); err != nil {
			t.Fatal(err)
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

// Isti nacrt ovjeren na dva čvora: ovdje stoji kao B-7/2026, a laptop ga je
// ovjerio kao B-6/2026, oznakom koju ovdje nosi drugi akt. Oba akta stoje
// crveno, akt s drugom ovjerom pokazuje je s potpisom, kod druge ovjere ga
// nalazi u popisu, a rješenje jednog sukoba ne rješava drugi (Odluke
// (8. 10. 2026.), pitanje 4)
func TestDrugaOvjeraIstogNacrtaKrozRute(t *testing.T) {
	o := novaOkolinaOznake(t)
	ctx := context.Background()
	vukovar := o.akt(t, "Vukovar", "unraid", "806A521E9D", models.PhasePrep)
	batina := o.akt(t, "Batina", "unraid", "77B0AA11CC", models.PhasePrep)
	batina.Broj = 6
	if err := o.aktiRepo.SaveAkt(ctx, batina); err != nil {
		t.Fatal(err)
	}
	// ovjera laptopa stiže razmjenom kao starija verzija: površina je ne prikazuje
	_, kljuc, _ := ed25519.GenerateKey(nil)
	naLaptopu := *vukovar
	naLaptopu.Broj, naLaptopu.OvjeraKod, naLaptopu.Cvor, naLaptopu.Ovjerio = 6, "CCA9623B58", "laptop", "Iva Ivić"
	ovjereno := time.Date(2026, 10, 6, 2, 50, 0, 0, models.Zagreb)
	naLaptopu.OvjerenoAt = &ovjereno
	naLaptopu.KljucCvora = razmjena.PublicKeyString(kljuc.Public().(ed25519.PublicKey))
	naLaptopu.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(kljuc, naLaptopu.PorukaPotpisa()))
	p, _ := json.Marshal(naLaptopu)
	if _, err := o.rec.Apply(ctx, []ledger.Version{{VersionID: "00000000-0000-7000-8000-000000000001", Entity: repository.EntityAkti,
		EntityID: vukovar.ID, NodeID: "laptop", Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}}); err != nil {
		t.Fatal(err)
	}

	popis := o.zovi(t, o.operater, o.oOvl, http.MethodGet, "/akti", nil)
	imaNaStranici(t, popis, "popis s drugom ovjerom", "isti nacrt ovjeren je i kao B-6/2026, kod CCA9623B58, čvor laptop",
		"ista oznaka kao akt", "u drugoj ovjeri, kod CCA9623B58")
	if n := strings.Count(popis.Body.String(), `class="akt-sukob"`); n != 2 {
		t.Errorf("akt s drugom ovjerom i akt iste oznake moraju biti crveni, a crvenih redaka je %d", n)
	}
	imaNaStranici(t, o.zovi(t, o.operater, o.oOvl, http.MethodGet, "/akti?q=CCA9623B58", nil), "kod druge ovjere", "/akti/"+vukovar.ID)
	stranica := o.zovi(t, o.voditelj, o.vOvl, http.MethodGet, "/akti/"+vukovar.ID, nil)
	imaNaStranici(t, stranica, "akt s drugom ovjerom", "ovjeren i na drugom čvoru", "Isti nacrt ovjeren je i na drugom čvoru",
		"B-6/2026, kod <span class=\"mono\">CCA9623B58</span>, čvor laptop, ovjerio Iva Ivić, 06.10.2026 02:50; elektronički potpis vrijedi",
		"Ovdje stoji ovjera B-7/2026 (kod 806A521E9D, čvor unraid)", "Označi riješenim")
	if l := o.zovi(t, o.voditelj, o.vOvl, http.MethodPost, "/akti/"+vukovar.ID+"/oznaka", url.Values{"napomena": {"Primateljima javljeno da vrijedi B-7/2026."}}).Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("rješenje druge ovjere: %s", l)
	}
	popis = o.zovi(t, o.operater, o.oOvl, http.MethodGet, "/akti", nil)
	if n := strings.Count(popis.Body.String(), `class="akt-sukob"`); n != 1 || !strings.Contains(popis.Body.String(), "isti nacrt ovjeren je i kao B-6/2026") {
		t.Errorf("riješena druga ovjera nije crvena, a ista oznaka B-6/2026 ostaje: %d crvenih redaka", n)
	}
	imaNaStranici(t, o.zovi(t, o.operater, o.oOvl, http.MethodGet, "/akti/"+batina.ID, nil), "akt iste oznake kao druga ovjera",
		"ista oznaka kao drugi akt", "u drugoj ovjeri, koja u popisu ne stoji (kod CCA9623B58, čvor laptop)")
}

func TestUzGresku(t *testing.T) {
	if got := uzGresku("Spremljeno.", "Sukobi", nil); got != "Spremljeno." {
		t.Errorf("bez greške poruka ostaje: %q", got)
	}
	if got := uzGresku("", "Akti s istom oznakom nisu provjereni", errors.New("baza je zauzeta")); got != "Akti s istom oznakom nisu provjereni: baza je zauzeta" {
		t.Errorf("greška uz praznu poruku: %q", got)
	}
}
