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

// Gumb „Otključaj inventuru” (Odluke (13. 10. 2026.)): vidi ga samo globalni
// administrator i samo na zaključenoj inventuri. Uprava sektora koja pošalje
// zahtjev sama dobiva 403, a inventura ostaje zaključena.
func TestOtkljucajInventuruKrozRute(t *testing.T) {
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
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	repo := repository.NewMtsRepository(baza, rec)
	if err := repo.OsigurajKatalog(ctx); err != nil {
		t.Fatal(err)
	}
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	svc := service.NewMtsService(repo, repository.NewSectionRepository(baza, rec), userRepo)

	skladistar := &models.User{ID: uuid.New(), FullName: "Petra Perić"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	admin := &models.User{ID: uuid.New(), FullName: "Marko Marić", IsGlobalAdmin: true}
	glavni := &models.UserPermissions{IsGlobalAdmin: true}
	sk := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Bilje", Aktivno: true}
	if err := svc.SpremiSkladiste(ctx, uprava, sk); err != nil {
		t.Fatal(err)
	}
	dan := time.Now().In(models.Zagreb).AddDate(0, 0, -2)
	if _, err := svc.Provedi(ctx, skladistar, uprava, service.Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: "lopata",
		Kolicina: 12, Datum: dan.AddDate(0, 0, -3)}); err != nil {
		t.Fatal(err)
	}
	p, err := svc.PredlozakPopisa(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SpremiPopis(ctx, skladistar, uprava, p); err != nil {
		t.Fatal(err)
	}

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewMtsHandler(func() *service.MtsService { return svc }, users, nil, nil, tmpl)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sredstva/popisi/{id}", h.ShowPopis)
	mux.HandleFunc("POST /sredstva/popisi/{id}/otkljucaj", h.HandleOtkljucajPopis)
	zovi := func(u *models.User, perms *models.UserPermissions, metoda, putanja string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(metoda, putanja, strings.NewReader(url.Values{}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c := context.WithValue(r.Context(), contextKeyUser, u)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}
	stranica := "/sredstva/popisi/" + p.ID
	gumb := func(u *models.User, perms *models.UserPermissions) bool {
		t.Helper()
		w := zovi(u, perms, http.MethodGet, stranica)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "template:") {
			t.Fatalf("stranica inventure: %d\n%s", w.Code, w.Body.String())
		}
		return strings.Contains(w.Body.String(), "Otključaj inventuru")
	}
	zakljucena := func() bool {
		t.Helper()
		cur, err := svc.Popis(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return cur.Zakljucen()
	}

	// nacrt: gumba nema ni administratoru
	if gumb(admin, glavni) {
		t.Error("gumb na nacrtu")
	}
	if err := svc.ZakljuciPopis(ctx, skladistar, uprava, p.ID); err != nil {
		t.Fatal(err)
	}
	if gumb(skladistar, uprava) {
		t.Error("uprava sektora vidi gumb")
	}
	if !gumb(admin, glavni) {
		t.Fatal("globalni administrator ne vidi gumb na zaključenoj inventuri")
	}
	tijelo := zovi(admin, glavni, http.MethodGet, stranica).Body.String()
	for _, dio := range []string{`<form method="POST" action="/sredstva/popisi/` + p.ID + `/otkljucaj"`, "Otključati inventuru?", "vraća u nacrt",
		"usklađenja koja je zaključenje upisalo brišu se", "s knjižnim stanjem toga dana"} {
		if !strings.Contains(tijelo, dio) {
			t.Errorf("gumb nema %q", dio)
		}
	}

	// uprava sektora koja zahtjev pošalje sama: 403, ništa se ne mijenja
	if w := zovi(skladistar, uprava, http.MethodPost, stranica+"/otkljucaj"); w.Code != http.StatusForbidden ||
		!strings.Contains(w.Body.String(), "samo globalni administrator") {
		t.Errorf("otključavanje bez ovlasti: %d %s", w.Code, w.Body.String())
	}
	if w := zovi(nil, nil, http.MethodPost, stranica+"/otkljucaj"); w.Code != http.StatusForbidden {
		t.Errorf("otključavanje bez prijave: %d", w.Code)
	}
	if !zakljucena() {
		t.Fatal("inventura otključana bez ovlasti")
	}

	// administrator: natrag na inventuru s porukom, inventura je nacrt
	w := zovi(admin, glavni, http.MethodPost, stranica+"/otkljucaj")
	l := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(l, stranica+"?") || !strings.Contains(l, "success=") {
		t.Fatalf("otključavanje: %d %q", w.Code, l)
	}
	if poruka, _ := url.QueryUnescape(strings.SplitN(l, "success=", 2)[1]); !strings.Contains(poruka, "vraćena u nacrt") {
		t.Errorf("poruka: %q", poruka)
	}
	if zakljucena() {
		t.Fatal("inventura je i dalje zaključena")
	}
	tijelo = zovi(admin, glavni, http.MethodGet, stranica).Body.String()
	if strings.Contains(tijelo, "Otključaj inventuru") || !strings.Contains(tijelo, "nacrt") || !strings.Contains(tijelo, "Otključao/la: Marko Marić") {
		t.Errorf("stranica otključane inventure nema nacrt i napomenu ili ima gumb")
	}
	// drugi put: poruka da nije zaključena
	l = zovi(admin, glavni, http.MethodPost, stranica+"/otkljucaj").Header().Get("Location")
	if poruka, _ := url.QueryUnescape(l); !strings.Contains(poruka, "error=") || !strings.Contains(poruka, "nije zaključena") {
		t.Errorf("otključavanje nacrta: %q", poruka)
	}
}

// Ruta otključavanja stoji iza zaštite od tuđih stranica i prijave
func TestRutaOtkljucavanjaIzaZastita(t *testing.T) {
	s, err := NewServer("", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	putanja := "/sredstva/popisi/" + uuid.NewString() + "/otkljucaj"
	r := httptest.NewRequest(http.MethodPost, putanja, nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://zlo.primjer.hr")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "druga stranica") {
		t.Errorf("s tuđe stranice: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodPost, putanja, nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("bez prijave: %d %q", w.Code, w.Header().Get("Location"))
	}
}
