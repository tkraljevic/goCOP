package web

import (
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webassets "gocop/web"

	"gocop/internal/db"
	"gocop/internal/kisomjeri"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/oborine"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func kisomjerPredlozak(t *testing.T) func(string) *template.Template {
	t.Helper()
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("kisomjer.html")...)
	if err != nil {
		t.Fatal(err)
	}
	return func(string) *template.Template { return tp }
}

func kisomjerZahtjev(h http.HandlerFunc, put, code string) string {
	req := httptest.NewRequest("GET", put, nil)
	req.SetPathValue("code", code)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true}))
	w := httptest.NewRecorder()
	h(w, req)
	return w.Body.String()
}

// Stranica kišomjera: Orahovica javlja i sate i 12-satne termine, pa se isto
// razdoblje ne smije zbrojiti dvaput; prognoza stvarnog kišomjera dolazi s
// najbliže izvedene točke i to piše; historijat zbraja godine i mjesece.
func TestStranicaKisomjera(t *testing.T) {
	dir := t.TempDir()
	baza, err := db.OpenDB(filepath.Join(dir, "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	svc := service.NewKisomjerService(repository.NewKisomjerRepository(baza, ledger.New(baza, "test")))
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	for _, k := range []models.Kisomjer{
		{Code: "dhmz-orahovica", Naziv: "Orahovica", Vrsta: models.KisomjerStvarni, Izvor: "dhmz", IzvorSifra: "1", Korak: "satni",
			Latitude: 45.54, Longitude: 17.88, Aktivan: true},
		{Code: "k-e-pobrde", Naziv: "Papuk pobrđe", Sliv: "E", Latitude: 45.6, Longitude: 17.8, Aktivan: true},
		{Code: "k-a-daleko", Naziv: "Koruška", Sliv: "A", Latitude: 46.7, Longitude: 13.8, Aktivan: true},
	} {
		k := k
		if err := svc.CreateKisomjer(ctx, admin, &k); err != nil {
			t.Fatal(err)
		}
	}

	ob, err := oborine.Otvori(filepath.Join(dir, "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ob.Close()
	sp := &kisomjeri.Spremiste{DB: ob}
	if err := sp.Pripremi(); err != nil {
		t.Fatal(err)
	}
	sada := time.Now()
	zadnji := sada.Truncate(time.Hour)
	var m []kisomjeri.Mjerenje
	for i := 0; i < 30; i++ { // 30 sati po 1 mm
		m = append(m, kisomjeri.Mjerenje{Kisomjer: "dhmz-orahovica", Kraj: zadnji.Add(-time.Duration(i) * time.Hour), Sati: 1, Oborina: 1, Izvor: "kisomjer-dhmz"})
	}
	// isti sati još jednom kao 12-satni termini — ne smiju se pribrojiti
	m = append(m, kisomjeri.Mjerenje{Kisomjer: "dhmz-orahovica", Kraj: zadnji.Add(-2 * time.Hour), Sati: 12, Oborina: 12, Izvor: "kisomjer-dhmz"})
	if _, err := sp.Upisi(ctx, m); err != nil {
		t.Fatal(err)
	}
	sat := sada.Unix() / 3600
	for i := int64(1); i <= 30; i++ { // prognoza: 0,5 mm na sat
		if _, err := ob.Exec(`INSERT INTO satne (kisomjer, sat, oborina, prognoza, preuzeto) VALUES ('k-e-pobrde', ?, 0.5, 1, ?)`, sat+i, sada.Unix()); err != nil {
			t.Fatal(err)
		}
	}

	arhPut := filepath.Join(dir, "vodostaji.db")
	a, err := sql.Open("sqlite", arhPut)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Exec(`
		CREATE TABLE nizovi (id INTEGER PRIMARY KEY, sliv TEXT NOT NULL DEFAULT 'drava',
			letva TEXT NOT NULL, izvor TEXT NOT NULL, velicina TEXT NOT NULL, vrsta TEXT NOT NULL,
			od TEXT NOT NULL DEFAULT '', do TEXT NOT NULL DEFAULT '', zapisa INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE ocitanja (niz INTEGER NOT NULL, vrijeme INTEGER NOT NULL,
			vrijednost REAL NOT NULL, PRIMARY KEY (niz, vrijeme)) WITHOUT ROWID;
		CREATE TABLE spoj (letva TEXT NOT NULL, velicina TEXT NOT NULL, korak TEXT NOT NULL,
			vrijeme INTEGER NOT NULL, vrijednost REAL NOT NULL, izvor TEXT NOT NULL,
			vrsta TEXT NOT NULL DEFAULT 'srednjak', tocnost REAL NOT NULL DEFAULT 0,
			PRIMARY KEY (letva, velicina, korak, vrijeme)) WITHOUT ROWID;
		INSERT INTO nizovi (letva, izvor, velicina, vrsta, zapisa) VALUES ('dhmz-orahovica', 'kisomjer-dhmz', 'oborina', 'dnevni', 730);`); err != nil {
		t.Fatal(err)
	}
	// 2024. i 2025.: svaki deseti dan 10 mm
	for d := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() < 2026; d = d.AddDate(0, 0, 1) {
		v := 0.0
		if d.YearDay()%10 == 0 {
			v = 10
		}
		if _, err := a.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor) VALUES ('dhmz-orahovica','oborina','dnevni',?,?,'kisomjer-dhmz')`, d.Unix(), v); err != nil {
			t.Fatal(err)
		}
	}
	a.Close()
	arh, err := repository.OpenArhiva(arhPut)
	if err != nil || arh == nil {
		t.Fatalf("arhiva: %v", err)
	}
	defer arh.Close()

	h := &KisomjerStranicaHandler{svc: func() *service.KisomjerService { return svc },
		arhiva: func() *repository.ArhivaRepository { return arh }, mjerenja: func() *kisomjeri.Spremiste { return sp },
		tmpl: kisomjerPredlozak(t)}

	s := kisomjerZahtjev(h.ShowOcitanja, "/slivovi/kisomjer/dhmz-orahovica", "dhmz-orahovica")
	for _, zeli := range []string{
		"Zadnja 24 h", ">24,0 <small>mm</small>", // 24 sata po 1 mm, bez 12-satnog termina
		"Prognoza 24 h", ">12,0 <small>mm</small>", // 24 sata po 0,5 mm
		"najbližu izvedenu\n    točku <strong>Papuk pobrđe</strong>",
		`class="graf-kise"`, "kišomjer DHMZ-a",
	} {
		if !strings.Contains(s, zeli) {
			t.Errorf("očitanja: nema %q", zeli)
		}
	}
	if strings.Contains(s, ">36,0 <small>mm</small>") || strings.Contains(s, ">42,0 <small>mm</small>") {
		t.Error("12-satni termin pribrojen satima")
	}

	s = kisomjerZahtjev(h.ShowHistorijat, "/slivovi/kisomjer/dhmz-orahovica/historijat?god=2024", "dhmz-orahovica")
	for _, zeli := range []string{"Po godinama", "Po mjesecima 2024.", ">360,0<", "Prosječna godina", "dnevni zbroj", "najkišniji prvo"} {
		if !strings.Contains(s, zeli) {
			t.Errorf("historijat: nema %q", zeli)
		}
	}

	// izvedena točka: piše da je procjena, prognoza je njezina
	s = kisomjerZahtjev(h.ShowOcitanja, "/slivovi/kisomjer/k-e-pobrde", "k-e-pobrde")
	if !strings.Contains(s, "Izvedena točka ne mjeri") || strings.Contains(s, "najbližu izvedenu") {
		t.Error("izvedena točka: nema napomene o procjeni ili prognoza nije njezina")
	}
}

// TestStranicaKisomjeraNaPravimPodacima iscrta stranice na pravim bazama
// čvora, za pregled okom. Pokreće se samo s GOCOP_KISA_PROBA=<mapa za HTML>.
func TestStranicaKisomjeraNaPravimPodacima(t *testing.T) {
	izlaz := os.Getenv("GOCOP_KISA_PROBA")
	if izlaz == "" {
		t.Skip("GOCOP_KISA_PROBA nije postavljen")
	}
	podaci := "../../data/"
	baza, err := sql.Open("sqlite", podaci+"gocop.db?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewKisomjerService(repository.NewKisomjerRepository(baza, nil))
	ob, err := sql.Open("sqlite", podaci+"oborine.db?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	arh, _ := repository.OpenArhiva(podaci + "vodostaji.db")
	h := &KisomjerStranicaHandler{svc: func() *service.KisomjerService { return svc },
		arhiva: func() *repository.ArhivaRepository { return arh }, mjerenja: func() *kisomjeri.Spremiste { return &kisomjeri.Spremiste{DB: ob} },
		tmpl: kisomjerPredlozak(t)}
	for _, z := range []struct{ ime, put, code string }{
		{"orahovica", "/slivovi/kisomjer/dhmz-orahovica", "dhmz-orahovica"},
		{"orahovica30", "/slivovi/kisomjer/dhmz-orahovica?pogled=30", "dhmz-orahovica"},
		{"osijek", "/slivovi/kisomjer/dhmz-osijek", "dhmz-osijek"},
		{"pljusak", "/slivovi/kisomjer/pljusak-belisce", "pljusak-belisce"},
		{"izvedena", "/slivovi/kisomjer/k-a-pobrde-1", "k-a-pobrde-1"},
		{"hist", "/slivovi/kisomjer/dhmz-varazdin/historijat?god=2023", "dhmz-varazdin"},
		{"histmj", "/slivovi/kisomjer/dhmz-varazdin/historijat?god=2023&mj=5", "dhmz-varazdin"},
	} {
		f := kisomjerZahtjev(h.ShowOcitanja, z.put, z.code)
		if strings.Contains(z.put, "historijat") {
			f = kisomjerZahtjev(h.ShowHistorijat, z.put, z.code)
		}
		if err := os.WriteFile(filepath.Join(izlaz, z.ime+".html"), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
