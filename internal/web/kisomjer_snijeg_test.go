package web

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/kisomjeri"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/oborine"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/snijeg"
)

// izvorSnijegaZaTest je snijeg na tlu s izmišljenim brojevima: točka
// k-a-gorje u A ima SNOWGRID (tri dana do jučer, 30 → 40 mm), k-a-van ga
// nema pa ide model, a A ima pragove iz 25 zima.
func izvorSnijegaZaTest(sad time.Time) *snijeg.Izvor {
	danas := sad.UTC().Unix() / 86400
	niz := map[int64]float64{danas - 4: 30, danas - 3: 30, danas - 2: 35, danas - 1: 40}
	povijest := map[int64]float64{}
	for g := 1990; g < 2015; g++ {
		for d := time.Date(g, 8, 1, 0, 0, 0, 0, time.UTC).Unix() / 86400; d < time.Date(g+1, 8, 1, 0, 0, 0, 0, time.UTC).Unix()/86400; d++ {
			povijest[d] = float64(20 + g - 1990)
		}
	}
	return &snijeg.Izvor{
		Tocke: func() ([]prognoza.OborinskaTocka, error) {
			return []prognoza.OborinskaTocka{{Code: "k-a-gorje", Sliv: "A", Tezina: 1}}, nil
		},
		NaTlu: func(tocke []string, od, do int64) (map[string]map[int64]float64, error) {
			out := map[string]map[int64]float64{}
			for _, t := range tocke {
				out[t] = map[int64]float64{}
				if t != "k-a-gorje" {
					continue
				}
				for d, v := range niz {
					out[t][d] = v
				}
				for d, v := range povijest {
					if d >= od {
						out[t][d] = v
					}
				}
			}
			return out, nil
		},
		Model: func(string, int64) (map[int64]float64, error) {
			return map[int64]float64{danas - 2: 12, danas - 1: 14}, nil
		},
		Sad: func() time.Time { return sad },
	}
}

// Stranica izvedene točke alpskog sliva: oznaka, snijeg u stupcima, u
// popisu i u sedam dana, i kartica „Snijeg na tlu”; točka bez SNOWGRID-a
// piše da je krivulja procjena iz ERA5; nizinska točka nema ništa od toga.
func TestStranicaKisomjeraAlpskogSliva(t *testing.T) {
	dir := t.TempDir()
	baza, err := db.BazaIzPredloska(filepath.Join(dir, "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	svc := service.NewKisomjerService(repository.NewKisomjerRepository(baza, ledger.New(baza, "test")))
	ctx := context.Background()
	for _, k := range []models.Kisomjer{
		{Code: "k-a-gorje", Naziv: "A gorje", Sliv: "A", Latitude: 46.7, Longitude: 13.2, Aktivan: true},
		{Code: "k-a-van", Naziv: "A van", Sliv: "A", Latitude: 46.6, Longitude: 15.1, Aktivan: true},
		{Code: "k-g-ravnica", Naziv: "G ravnica", Sliv: "G", Latitude: 45.5, Longitude: 18.6, Aktivan: true},
	} {
		k := k
		if err := svc.CreateKisomjer(ctx, &models.UserPermissions{IsGlobalAdmin: true}, &k); err != nil {
			t.Fatal(err)
		}
	}
	ob, err := oborine.Otvori(filepath.Join(dir, "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ob.Close()
	sada := time.Now()
	sat := sada.Unix() / 3600
	for i := int64(-30); i <= 30; i++ { // 1 mm na sat, od toga 0,6 snijeg; prognoza 0,5 i sve snijeg
		v, sn, p := 1.0, 0.6, 0
		if i > 0 {
			v, sn, p = 0.5, 0.5, 1
		}
		for _, k := range []string{"k-a-gorje", "k-g-ravnica"} {
			if _, err := ob.Exec(`INSERT INTO satne (kisomjer, sat, oborina, snijeg, temperatura, prognoza, preuzeto) VALUES (?, ?, ?, ?, -2, ?, ?)`,
				k, sat+i, v, sn, p, sada.Unix()); err != nil {
				t.Fatal(err)
			}
		}
	}
	arh := arhivaSnijega(t, dir, sada)
	defer arh.Close()
	izvor := izvorSnijegaZaTest(sada)
	h := &KisomjerStranicaHandler{svc: func() *service.KisomjerService { return svc },
		arhiva: func() *repository.ArhivaRepository { return arh }, mjerenja: func() *kisomjeri.Spremiste { return &kisomjeri.Spremiste{DB: ob} },
		tmpl: kisomjerPredlozak(t), snijeg: func() *snijeg.Izvor { return izvor }}

	s := kisomjerZahtjev(h.ShowOcitanja, "/slivovi/kisomjer/k-a-gorje?korak=satni", "k-a-gorje")
	for _, zeli := range []string{`<span class="badge kis-alpski"`, "alpski sliv", "od toga snijeg <span", `data-stupac="od toga snijeg (mm)">0,6</td>`,
		`class="stupac-snijega"`, "od toga snijeg 0,6 mm vode", "snijeg (mm vode)", "od toga snijeg ", " mm vode. Dan bez podatka",
		"Snijeg na tlu", "40 mm vode", "oko točke, ", "raste", `class="graf-snijega"`, "SNOWGRID (GeoSphere Austria)", "procjena iz prognoze",
		"Do danas SNOWGRID oko točke; idućih dana procjena iz prognoze snijega i temperature."} {
		if !strings.Contains(s, zeli) {
			t.Errorf("alpska točka: nema %q", zeli)
		}
	}
	s = kisomjerZahtjev(h.ShowOcitanja, "/slivovi/kisomjer/k-a-gorje?pogled=30", "k-a-gorje")
	if !strings.Contains(s, `class="stupac-snijega"`) || !strings.Contains(s, "od toga snijeg <span") {
		t.Error("alpska točka po danima: nema snijega")
	}

	s = kisomjerZahtjev(h.ShowOcitanja, "/slivovi/kisomjer/k-a-van", "k-a-van")
	for _, zeli := range []string{"14 mm vode", "procjena iz ERA5, SNOWGRID ovdje nema podataka", "SNOWGRID ovdje nema podataka: do danas procjena iz ERA5"} {
		if !strings.Contains(s, zeli) {
			t.Errorf("točka bez SNOWGRID-a: nema %q", zeli)
		}
	}

	s = kisomjerZahtjev(h.ShowOcitanja, "/slivovi/kisomjer/k-g-ravnica", "k-g-ravnica")
	for _, nezeli := range []string{"alpski sliv", "stupac-snijega", "od toga snijeg", "Snijeg na tlu"} {
		if strings.Contains(s, nezeli) {
			t.Errorf("nizinska točka ima %q", nezeli)
		}
	}

	// bez izvora snijega nema kartice, a izvor s greškom je preskoči
	h.snijeg = nil
	if s := kisomjerZahtjev(h.ShowOcitanja, "/slivovi/kisomjer/k-a-gorje", "k-a-gorje"); strings.Contains(s, "Snijeg na tlu") {
		t.Error("bez izvora ima karticu")
	}
	izvor.NaTlu = func([]string, int64, int64) (map[string]map[int64]float64, error) { return nil, errors.New("baza") }
	h.snijeg = func() *snijeg.Izvor { return izvor }
	if s := kisomjerZahtjev(h.ShowOcitanja, "/slivovi/kisomjer/k-a-gorje", "k-a-gorje"); strings.Contains(s, "Snijeg na tlu") {
		t.Error("uz grešku ima karticu")
	}
}

// arhivaSnijega je arhiva sa satnim i dnevnim snijegom točke k-a-gorje za
// dane prije tjedan dana (izmišljeni brojevi)
func arhivaSnijega(t *testing.T, dir string, sada time.Time) *repository.ArhivaRepository {
	t.Helper()
	put := filepath.Join(dir, "arhiva-snijega.db")
	a, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Exec(`CREATE TABLE nizovi (id INTEGER PRIMARY KEY);
		CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL, izvor TEXT,
			vrsta TEXT DEFAULT 'srednjak', tocnost REAL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	dan := time.Date(sada.Year(), sada.Month(), sada.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -20)
	for _, r := range [][3]any{{"oborina", "dnevni", 8.0}, {"snijeg", "dnevni", 5.0}, {"oborina", "satni", 2.0}, {"snijeg", "satni", 1.5}} {
		if _, err := a.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor) VALUES ('k-a-gorje', ?, ?, ?, ?, 'era5-land')`,
			r[0], r[1], dan.Unix(), r[2]); err != nil {
			t.Fatal(err)
		}
	}
	a.Close()
	arh, err := repository.OpenArhiva(put)
	if err != nil || arh == nil {
		t.Fatalf("arhiva: %v", err)
	}
	return arh
}

// Stupac bez snijega, s prognozom i dan koji traje; izvan alpskih slivova
// snijega u stupcima nema.
func TestStupciSaSnijegom(t *testing.T) {
	kad := time.Date(2026, 11, 21, 0, 0, 0, 0, models.Zagreb)
	if o := opisStupca(stupacKise{Kad: kad, Sati: 24, Mm: 13.4, Snijeg: 20, Prognoza: true}); o != "21.11.2026., prognoza: 13,4 mm, od toga snijeg 13,4 mm vode" {
		t.Errorf("opis %q", o)
	}
	if o := opisStupca(stupacKise{Kad: kad, Sati: 1, Mm: 1, Nepotpun: true}); !strings.HasSuffix(o, ", dan još traje: 1,0 mm") {
		t.Errorf("opis %q", o)
	}
	if b, p := bojaStupca(stupacKise{Nepotpun: true}); b != "var(--primary)" || p != "0.6" {
		t.Errorf("boja %s %s", b, p)
	}
	s := snijegZaPrikaz(false, []stupacKise{{Mm: 2, Snijeg: 1}})
	if s[0].Snijeg != 0 || snijegZaPrikaz(true, []stupacKise{{Snijeg: 1}})[0].Snijeg != 1 {
		t.Error("snijeg za prikaz")
	}
	g := string(crtajKisu([]stupacKise{{Kad: kad, Sati: 24, Mm: 4, Snijeg: 4}}, time.Time{}))
	if !strings.Contains(g, "stupac-snijega") || strings.Contains(g, "fill:var(--primary)\"") {
		t.Errorf("samo snijeg: %s", g)
	}
	if (DanKise{}).SnijegTekst() != "" || (RedKise{}).SnijegMm() != 0 {
		t.Error("bez snijega")
	}
	v := 6.8
	if (DanKise{Snijeg: &v}).SnijegTekst() != "snijeg 6,8" {
		t.Error("snijeg dana")
	}
}

// Slivovi: alpski međusliv ima karticu snijega s pragovima i razinom,
// nizinski je nema; bez izvora ili uz grešku kartice nema.
func TestSlivoviSnijeg(t *testing.T) {
	sad := time.Now()
	h := NewSlivoviHandler(nil, nil, nil, nil, nil)
	skupine := map[string]*SkupinaKisomjera{"A": {Sliv: models.Sliv{Oznaka: "A", Naziv: "Drava iznad Borla"}}, "G": {Sliv: models.Sliv{Oznaka: "G"}}}
	h.dodajSnijeg(skupine, sad)
	if skupine["A"].Snijeg != nil {
		t.Error("bez izvora ima snijeg")
	}
	izvor := izvorSnijegaZaTest(sad)
	h.SetSnijeg(func() *snijeg.Izvor { return izvor })
	h.dodajSnijeg(skupine, sad)
	a := skupine["A"].Snijeg
	if a == nil || a.Mm != "40 mm vode" || a.Klasa != "kisa-narancasto" || a.Rijetko != "jednom u 5 zima" || a.Trend != "raste u zadnja 3 dana" || !a.Pragovi {
		t.Fatalf("A: %+v", a)
	}
	if skupine["G"].Snijeg != nil {
		t.Error("nizinski međusliv ima snijeg")
	}
	html := iscrtaj(t, "slivovi.html", SlivoviPageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{},
		Skupine: []SkupinaKisomjera{*skupine["A"], *skupine["G"]}})
	for _, zeli := range []string{"Snijeg na tlu · A · Drava iznad Borla", "Južni i Istočni Tirol, Koruška, slovenska Drava do Borla",
		"stanje ", `<span class="badge kisa-narancasto">jednom u 5 zima</span>`, "raste u zadnja 3 dana", ">3 zime ", ">10 zima ",
		"SNOWGRID, prosjek austrijskog dijela sliva", "Crte su pragovi ovog sliva: toliko snijega bude jednom u 3, 5 i 10 zima.",
		"Snijeg na tlu: SNOWGRID, GeoSphere Austria (austrijski dio sliva, kasni 1–2 dana)."} {
		if !strings.Contains(html, zeli) {
			t.Errorf("Slivovi: nema %q", zeli)
		}
	}
	if strings.Count(html, "Snijeg na tlu ·") != 1 {
		t.Error("kartica snijega u nizinskom međuslivu")
	}
	izvor.Tocke = func() ([]prognoza.OborinskaTocka, error) { return nil, errors.New("registar") }
	izvor.Zaboravi()
	skupine["A"].Snijeg = nil
	h.dodajSnijeg(skupine, sad)
	if skupine["A"].Snijeg != nil {
		t.Error("uz grešku ima snijeg")
	}
	if snijegOd(snijeg.StanjeSliva{Dan: 7}) != 7 {
		t.Error("os bez sezone")
	}
}
