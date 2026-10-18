package service

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Voda u rečenici akta dobiva „r.” samo kad ga ili druge oznake vrste vode
// već nema; bez vode nema ni oznake
func TestNazivVodeUAktu(t *testing.T) {
	for voda, ocek := range map[string]string{
		"":              "",
		"   ":           "",
		"Primjerica":    "r. Primjerica",
		"kanal Probni":  "kanal Probni",
		"R. Primjerica": "R. Primjerica",
		" potok Mali ":  "potok Mali",
	} {
		if s := nazivVode(voda); s != ocek {
			t.Errorf("nazivVode(%q) = %q, očekivano %q", voda, s, ocek)
		}
	}
}

// Epizoda akta s prognozom proglašena je po prognozi, inače po pragu;
// nepoznata skupina primatelja ide iza svih poznatih
func TestOsnovaEpizodeIRedSkupine(t *testing.T) {
	if o := osnovaEpizode(&models.Akt{Prognoza: "val stiže sutra"}); o != models.BasisForecast {
		t.Errorf("akt s prognozom: %q", o)
	}
	if o := osnovaEpizode(&models.Akt{}); o != models.BasisThreshold {
		t.Errorf("akt bez prognoze: %q", o)
	}
	if r := redSkupine("nepoznata skupina"); r != len(models.SkupinePrimatelja) {
		t.Errorf("red nepoznate skupine: %d", r)
	}
	if r := redSkupine(models.SkupinePrimatelja[0]); r != 0 {
		t.Errorf("red prve skupine: %d", r)
	}
}

// Bez ovlasti nitko ne radi s nacrtom; dionica akta bez registra dionica
// daje pravo samo osobi kojoj je ta dionica u dužnosti
func TestPravoNaNacrtBezRegistraDionica(t *testing.T) {
	s := &AktService{}
	a := &models.Akt{Sektor: "P", AreaID: 1, Dionice: []models.AktDionica{{Code: "P.1.1"}}}
	u := &models.User{ID: uuid.New()}
	if s.smijePripremiti(nil, u, a) || s.smijePripremiti(&models.UserPermissions{}, nil, a) {
		t.Error("nacrt bez ovlasti ili osobe")
	}
	if s.pisePoAktu(nil, a) || s.pisePoAktu(&models.UserPermissions{}, nil) {
		t.Error("pisanje po aktu bez ovlasti ili akta")
	}
	if s.pisePoAktu(&models.UserPermissions{AllowedSections: map[string]bool{"P.1.2": true}}, a) {
		t.Error("tuđa dionica daje pravo na akt")
	}
	if !s.pisePoAktu(&models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}}, a) {
		t.Error("dionica akta ne daje pravo na akt")
	}
}

// Razina uprave koja nije organizacija, sektor ni područje ne pokriva nijednu
// dužnost
func TestDosegDuznostiNepoznateRazine(t *testing.T) {
	sektor, podrucje := "P", 1
	p := &models.UserPermissions{IsGlobalAdmin: true, AdminSectors: map[string]bool{"P": true}, AdminAreas: map[int]bool{1: true}}
	poSektoru := func(int) string { return "P" }
	for _, razina := range []int{0, 4, 5} {
		if dutyInScope(p, razina, &sektor, &podrucje, poSektoru) {
			t.Errorf("razina %d pokriva dužnost", razina)
		}
	}
	if !dutyInScope(p, 3, &sektor, &podrucje, poSektoru) {
		t.Error("uprava područja ne pokriva dužnost svog područja")
	}
}

// Satno čišćenje drugog koraka briše istekla zaključavanja unosa koda, a
// ona koja još traju ostavlja
func TestCiscenjeZakljucavanjaDrugogKoraka(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "drugi-korak.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "cvor-a")
	d := NewDrugiKorak(repository.NewDrugiKorakRepository(baza), repository.NewRacuniSustavaRepository(baza),
		repository.NewUserRepository(baza, rec), repository.NewAktiRepository(baza, rec))
	sad := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	d.SetSat(func() time.Time { return sad })
	d.blokDo["isteklo"] = sad.Add(-time.Minute)
	d.blokDo["upravo"] = sad
	d.blokDo["traje"] = sad.Add(time.Minute)
	if _, err := d.Ocisti(context.Background()); err != nil {
		t.Fatal(err)
	}
	ostalo := map[string]bool{}
	for k := range d.blokDo {
		ostalo[k] = true
	}
	if !reflect.DeepEqual(ostalo, map[string]bool{"traje": true}) {
		t.Errorf("zaključavanja nakon čišćenja: %v", ostalo)
	}
}

// Letve koje osoba obično očitava u isto doba idu po nazivu
func TestTerenskiPogledIstoDobaPoNazivu(t *testing.T) {
	o := novaOkolinaCitanja(t)
	pp, ovl := o.pperic(t, "pperic", dionicaDuznost("P.1.1", 1))
	d := time.Now().In(models.Zagreb).AddDate(0, 0, -2)
	sedam := time.Date(d.Year(), d.Month(), d.Day(), 7, 0, 0, 0, models.Zagreb)
	o.upisi(t, models.Reading{StationID: o.primjerovo.ID.String(), MeasuredAt: sedam, LevelCm: cmP(1), UserID: pp.ID.String()})
	o.upisi(t, models.Reading{StructureID: o.csProbni.ID.String(), MeasuredAt: sedam, LevelCm: cmP(1), UserID: pp.ID.String()})
	fo, err := o.rs.FieldOverview(context.Background(), ovl, pp, 0)
	if err != nil || len(fo.Mine) != 2 {
		t.Fatalf("%+v %v", fo, err)
	}
	if fo.Mine[0].Name != "CS Probni" || fo.Mine[1].Name != "Primjerovo" || fo.Mine[0].UsualMin != fo.Mine[1].UsualMin {
		t.Errorf("poredak %s (%s), %s (%s)", fo.Mine[0].Name, fo.Mine[0].UsualTime(), fo.Mine[1].Name, fo.Mine[1].UsualTime())
	}
}

// Pregled letvi javlja grešku kad se dionice letvi ne daju pročitati,
// umjesto da letve ostanu bez sektora i područja
func TestPregledLetviBezDionica(t *testing.T) {
	o := novaOkolinaCitanja(t)
	if _, err := o.baza.Exec(`ALTER TABLE sections RENAME TO sections_negdje`); err != nil {
		t.Fatal(err)
	}
	if pregled, err := o.rs.Overview(context.Background()); err == nil {
		t.Errorf("pregled bez dionica bez greške: %d letvi", len(pregled))
	}
}
