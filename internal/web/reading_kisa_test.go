package web

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Polje za kišu vidi se na letvi uz koju stoji kišomjer, i na očitanju koje
// kišu već ima; na drugim letvama ga nema.
func TestObrazacOcitanjaKisa(t *testing.T) {
	cm := 140
	rd := &models.Reading{MeasuredAt: time.Date(2026, 11, 22, 7, 0, 0, 0, models.Zagreb), LevelCm: &cm}
	podaci := ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Reading: rd, GaugeName: "Lipovac", BackURL: "/readings", LocalValue: "2026-11-22T07:00"}
	if strings.Contains(iscrtaj(t, "reading_form.html", podaci), `name="oborina_mm"`) {
		t.Error("letva bez kišomjera nudi kišu")
	}
	podaci.Kisomjer = &models.Kisomjer{Code: "k-lipa", Naziv: "Lipovac kiša"}
	html := iscrtaj(t, "reading_form.html", podaci)
	for _, want := range []string{`name="oborina_mm"`, "Kišomjer Lipovac kiša. Kiša od jutra prethodnog dana", "Oborina (mm)"} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac nema %q", want)
		}
	}
	kisa := 12.4
	rd.RainMm = &kisa
	podaci.Kisomjer = nil
	if html := iscrtaj(t, "reading_form.html", podaci); !strings.Contains(html, `value="12,4"`) {
		t.Error("očitanje s kišom bez kišomjera izgubilo je polje kiše")
	}

	r := obicniObrazac("/readings/create", "level_cm=120&oborina_mm=12,4&measured_at=2026-11-22T07:00")
	if rd, err := readingFromForm(r); err != nil || rd.RainMm == nil || *rd.RainMm != 12.4 {
		t.Errorf("kiša iz obrasca: %+v, %v", rd, err)
	}
	if rd, err := readingFromForm(obicniObrazac("/readings/create", "level_cm=120&measured_at=2026-11-22T07:00")); err != nil || rd.RainMm != nil {
		t.Errorf("prazno polje kiše: %+v, %v", rd, err)
	}
}

// Popis očitanja pokazuje kišu uz vodostaj.
func TestOpisOcitanjaSKisom(t *testing.T) {
	kisa := 12.4
	if s := opisOcitanja(models.Reading{Source: models.ReadingSourceManual, RainMm: &kisa}, ProtokLetve{}); !strings.HasSuffix(s, " · kiša 12,4 mm") {
		t.Errorf("opis = %q", s)
	}
}

// Kiša ide u izvoz i vraća se iz datoteke; stupac kojeg nema ne dira je.
func TestKisaUDatoteciOcitanja(t *testing.T) {
	stara, nova := 3.0, 4.5
	novo := models.Reading{RainMm: &stara}
	redak := []string{"", "", "4,5"}
	vrijednost := func(i int) string { return redak[i] }
	if g := decimaleIzRetka(&novo, vrijednost, -1, -1, -1); g != "" || *novo.RainMm != 3 {
		t.Errorf("bez stupca: %q, %v", g, *novo.RainMm)
	}
	if g := decimaleIzRetka(&novo, vrijednost, -1, -1, 2); g != "" || *novo.RainMm != nova {
		t.Errorf("sa stupcem: %q, %v", g, novo.RainMm)
	}
	redak[2] = "x"
	if g := decimaleIzRetka(&novo, vrijednost, -1, -1, 2); g != "oborina: „x“ nije broj" {
		t.Errorf("kriva kiša: %q", g)
	}
	izmjene := razlike(models.Reading{RainMm: &stara}, models.Reading{RainMm: &nova})
	if len(izmjene) != 1 || izmjene[0].Polje != "oborina" {
		t.Errorf("razlike = %+v", izmjene)
	}
	found := false
	for _, s := range stupciOcitanja {
		found = found || s == "oborina_mm"
	}
	if !found {
		t.Error("izvoz nema stupac oborina_mm")
	}
}

// Obrazac očitanja pita registar kišomjera samo za letvu.
func TestKisomjerLetveZaObrazac(t *testing.T) {
	ctx := context.Background()
	if k := (&ReadingsHandler{}).kisomjerLetve(ctx, &models.Station{Code: "lipa"}); k != nil {
		t.Errorf("bez registra = %+v", k)
	}
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "kisa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	sada := time.Now()
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('l', 'lipa', 'Lipovac', ?, ?)`, sada, sada); err != nil {
		t.Fatal(err)
	}
	svc := service.NewKisomjerService(repository.NewKisomjerRepository(baza, ledger.New(baza, "test")))
	if err := svc.CreateKisomjer(ctx, &models.UserPermissions{IsGlobalAdmin: true}, &models.Kisomjer{Code: "k-lipa", Naziv: "Lipovac kiša",
		Vrsta: models.KisomjerStvarni, Izvor: "hv", Letva: "lipa", Latitude: 45.5, Longitude: 18.5, Aktivan: true}); err != nil {
		t.Fatal(err)
	}
	h := &ReadingsHandler{}
	h.SetKisomjeri(func() *service.KisomjerService { return svc })
	if k := h.kisomjerLetve(ctx, nil); k != nil {
		t.Errorf("objekt = %+v", k)
	}
	if k := h.kisomjerLetve(ctx, &models.Station{Code: "lipa"}); k == nil || k.Code != "k-lipa" {
		t.Errorf("letva s kišomjerom = %+v", k)
	}
	if k := h.kisomjerLetve(ctx, &models.Station{Code: "brod"}); k != nil {
		t.Errorf("letva bez kišomjera = %+v", k)
	}
}
