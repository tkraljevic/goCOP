package db

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Letve iz dionica: isti vodomjer na dvjema dionicama jedna je letva s
// obje dionice; drukčiji pragovi i vodomjer bez praga u centimetrima
// označavaju se za pregled; upute i kote bez letve ne ulaze.
func TestLetveIzDionica(t *testing.T) {
	sections := []models.Section{
		{Code: "P.61.1", Description: "Dunav, desna obala", Parts: []models.SectionPart{{Gauges: []models.GaugeItem{
			{StationName: "Probni Grad , rkm 1.333,45 (76,19)", PrepCm: "+530", RegularCm: "+580", EmergCm: "+630", RecordCm: "+769", Notes: " mjerodavan "},
			{StationName: "Prema Pravilniku akumulacije i prema"},
			{StationName: "uputa bez ijednog broja"},
		}}}},
		{Code: "P.61.2", Description: "Dunav, desna obala", Parts: []models.SectionPart{{Gauges: []models.GaugeItem{
			{StationName: "Probni Grad , rkm 1.333,45 (76,19)", PrepCm: "+540", RegularCm: "+580", EmergCm: "+630"},
		}}, {Gauges: []models.GaugeItem{
			{StationName: "Novo Selo , km 3,20"},
		}}}},
	}
	letve, dionice := LetveIzDionica(sections)
	if len(letve) != 2 || len(dionice) != 2 {
		t.Fatalf("letve %+v", letve)
	}
	prva := letve[0]
	if prva.Name != "Probni Grad" || prva.Code != "probni-grad" || prva.ZeroDatum == nil || *prva.ZeroDatum != 76.19 ||
		prva.ZeroDatumSystem != "TRST" || prva.ZeroDatumNewSystem != "HVRS71" {
		t.Errorf("prva letva %+v", prva)
	}
	if prva.Prep.Cm == nil || *prva.Prep.Cm != 530 || prva.Prep.Raw != "+530" || prva.Record.Cm == nil || *prva.Record.Cm != 769 || prva.Notes != "mjerodavan" {
		t.Errorf("pragovi prve: %+v %+v %q", prva.Prep, prva.Record, prva.Notes)
	}
	if !prva.NeedsReview || !strings.Contains(prva.ReviewNote, "dionica P.61.2 navodi drukčije pragove") {
		t.Errorf("razlika pragova nije označena: %v %q", prva.NeedsReview, prva.ReviewNote)
	}
	if !slices.Equal(dionice[0], []string{"P.61.1", "P.61.2"}) || !slices.Equal(dionice[1], []string{"P.61.2"}) {
		t.Errorf("dionice %v", dionice)
	}
	druga := letve[1]
	if druga.Name != "Novo Selo" || !druga.NeedsReview || !strings.Contains(druga.ReviewNote, "nijedan prag") || druga.Prep.Cm != nil {
		t.Errorf("druga letva %+v", druga)
	}
	if l, d := LetveIzDionica(nil); len(l) != 0 || len(d) != 0 {
		t.Errorf("bez dionica %v %v", l, d)
	}
}

// Prag je isti kad su isti i broj i zapis (bez obzira na velika slova).
func TestPragSameAs(t *testing.T) {
	slucajevi := []struct {
		a, b string
		isti bool
	}{
		{"+530", "+530", true},
		{"+530", "+540", false},
		{"+530", "", false},
		{"", "", true},
		{"prema pravilniku", "PREMA PRAVILNIKU", true},
		{"prema pravilniku", "po odluci", false},
		{" +530 ", "+530", true},
	}
	for _, s := range slucajevi {
		if got := newThresholdDraft(s.a).sameAs(newThresholdDraft(s.b)); got != s.isti {
			t.Errorf("%q i %q: %v, želim %v", s.a, s.b, got, s.isti)
		}
	}
}

// mapaRegistara usmjerava registre na privremenu mapu do kraja testa
func mapaRegistara(t *testing.T, datoteke map[string]string) string {
	t.Helper()
	mapa := t.TempDir()
	for ime, sadrzaj := range datoteke {
		if err := os.WriteFile(filepath.Join(mapa, ime), []byte(sadrzaj), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stara := DataDir
	t.Cleanup(func() { DataDir = stara })
	DataDir = mapa
	return mapa
}

// Bez prijepisa dionica nema ni greške: dionice stižu razmjenom. Neispravan
// prijepis i mapa umjesto datoteke javljaju grešku s putanjom.
func TestLoadSections(t *testing.T) {
	mapaRegistara(t, nil)
	if s, err := LoadSections(); s != nil || err != nil {
		t.Fatalf("bez datoteke %v %v", s, err)
	}

	mapa := mapaRegistara(t, map[string]string{"sections.json": `[{"code":"P.61.1","area_id":61}]`})
	s, err := LoadSections()
	if err != nil || len(s) != 1 || s[0].Code != "P.61.1" || s[0].AreaID != 61 {
		t.Fatalf("prijepis %+v %v", s, err)
	}

	if err := os.WriteFile(filepath.Join(mapa, "sections.json"), []byte(`{nije niz`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSections(); err == nil || !strings.Contains(err.Error(), filepath.Join(mapa, "sections.json")) {
		t.Errorf("neispravan prijepis: %v", err)
	}

	mapa = mapaRegistara(t, nil)
	if err := os.Mkdir(filepath.Join(mapa, "sections.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSections(); err == nil || !strings.Contains(err.Error(), "greška pri čitanju") {
		t.Errorf("mapa umjesto datoteke: %v", err)
	}
}

// Organizacija se puni samo u praznu bazu; neispravna datoteka ne upiše ništa.
func TestSeedOrganization(t *testing.T) {
	database := praznaBaza(t)
	mapaRegistara(t, map[string]string{"organizacija.json": `{"sectors":[`})
	if err := seedOrganization(database); err == nil || !strings.Contains(err.Error(), "organizacija.json") {
		t.Fatalf("neispravna organizacija: %v", err)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM sectors`); n != 0 {
		t.Errorf("sektora %d", n)
	}

	mapaRegistara(t, map[string]string{"organizacija.json": `{"sectors":[{"id":"P","name":"Sektor P","vgo_name":"VGO P","center_cop":"COP P"}],
		"areas":[{"id":61,"sector_id":"P","name":"Područje 61","vgi_name":"VGI 61","direct_to_sector":true}]}`})
	if err := seedOrganization(database); err != nil {
		t.Fatal(err)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM areas WHERE id = 61 AND sector_id = 'P' AND direct_to_sector = 1`); n != 1 {
		t.Errorf("područje 61 upisano %d puta", n)
	}

	// baza koja već ima sektore ne čita datoteku, ni neispravnu
	mapaRegistara(t, map[string]string{"organizacija.json": `smeće`})
	if err := seedOrganization(database); err != nil {
		t.Errorf("puna baza: %v", err)
	}

	// područje nepostojećeg sektora: ništa se ne upiše
	druga := praznaBaza(t)
	mapaRegistara(t, map[string]string{"organizacija.json": `{"sectors":[{"id":"Q","name":"Sektor Q"}],
		"areas":[{"id":7,"sector_id":"Z","name":"Bez sektora"}]}`})
	if err := seedOrganization(druga); err == nil || !strings.Contains(err.Error(), "branjeno područje 7") {
		t.Errorf("područje bez sektora: %v", err)
	}
	if n := count(t, druga, `SELECT COUNT(*) FROM sectors`); n != 0 {
		t.Errorf("sektor upisan uz grešku područja: %d", n)
	}
}

// Registar voda puni se iz datoteke samo kad je prazan; službeni naziv daje
// šifru, a neispravna datoteka ne upiše ništa.
func TestSeedWatercourses(t *testing.T) {
	database := praznaBaza(t)
	mapaRegistara(t, nil)
	if err := seedWatercourses(database); err != nil {
		t.Fatalf("bez datoteke: %v", err)
	}
	mapaRegistara(t, map[string]string{"watercourses.json": `[{"official_name":"rijeka Proba"`})
	if err := seedWatercourses(database); err == nil || !strings.Contains(err.Error(), "watercourses.json") {
		t.Fatalf("neispravna datoteka: %v", err)
	}
	mapaRegistara(t, map[string]string{"watercourses.json": `[
		{"official_name":"rijeka Proba","name":"Proba","kind":"rijeka","origin":"ODLUKA","length_km":12.5},
		{"official_name":"potok Mali (Baranja)","name":"Mali","kind":"potok"},
		{"official_name":"rijeka Proba","name":"Proba dvaput","kind":"rijeka"}]`})
	if err := seedWatercourses(database); err != nil {
		t.Fatal(err)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM watercourses`); n != 2 {
		t.Errorf("voda %d, želim 2 (ista šifra jednom)", n)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM watercourses WHERE code = 'rijeka-proba' AND name = 'Proba' AND origin = 'ODLUKA' AND length_km = 12.5`); n != 1 {
		t.Error("rijeka Proba nije upisana kako stoji u datoteci")
	}
	if n := count(t, database, `SELECT COUNT(*) FROM watercourses WHERE code = 'potok-mali-baranja'`); n != 1 {
		t.Error("šifra s pojašnjenjem u zagradi")
	}
	// puni registar ne čita datoteku, ni neispravnu
	mapaRegistara(t, map[string]string{"watercourses.json": `smeće`})
	if err := seedWatercourses(database); err != nil {
		t.Errorf("puni registar: %v", err)
	}
}

// Objekti iz evidencije: upisuju se samo novi, s kotom u starom sustavu i
// vodomjerom istog naziva na dionici istog područja.
func TestSeedStructures(t *testing.T) {
	database, _ := bazaKazala(t)
	letva := StableID("station", "P-CS").String()
	izvedi(t, database,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('`+letva+`', 'P-CS', 'CS i ustava Draž', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.61.1', 61, 'P', 'probna', '2026-01-01', '2026-01-01')`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('ss1', 'P.61.1', '`+letva+`', '2026-01-01')`,
		`INSERT INTO structures (id, code, name, kind, sector_id, area_id, created_at, updated_at)
			VALUES ('`+StableID("structure", "P-STARA").String()+`', 'P-STARA', 'Stara ustava', 'USTAVA', 'P', 61, '2026-01-01', '2026-01-01')`,
	)
	mapaRegistara(t, nil)
	if err := seedStructures(database); err != nil {
		t.Fatalf("bez datoteke: %v", err)
	}
	mapaRegistara(t, map[string]string{"objekti_bp16.json": `{`})
	if err := seedStructures(database); err == nil || !strings.Contains(err.Error(), "objekti_bp16.json") {
		t.Fatalf("neispravna datoteka: %v", err)
	}
	mapaRegistara(t, map[string]string{"objekti_bp16.json": `[
		{"code":"P-STARA","name":"Promijenjeni naziv","kind":"USTAVA","sector_id":"P","area_id":61},
		{"code":"P-DRAZ","name":"Ustava Draž","kind":"USTAVA","sector_id":"P","area_id":61,"zero_datum":81.25,"start_cm":120,"start_text":"+120"},
		{"code":"P-BEZ","name":"CS Daleka","kind":"CRPNA_STANICA","sector_id":"P","area_id":61}]`})
	if err := seedStructures(database); err != nil {
		t.Fatal(err)
	}
	if got := tekst(t, database, `SELECT name FROM structures WHERE code = 'P-STARA'`); got != "Stara ustava" {
		t.Errorf("postojeći objekt prepisan: %q", got)
	}
	draz := StableID("structure", "P-DRAZ").String()
	if got := tekst(t, database, `SELECT station_id || '|' || zero_datum_system || '|' || zero_datum || '|' || start_cm FROM structures WHERE id = ?`, draz); got != letva+"|TRST|81.25|120" {
		t.Errorf("ustava Draž %q", got)
	}
	if got := tekst(t, database, `SELECT station_id || '|' || zero_datum_system FROM structures WHERE code = 'P-BEZ'`); got != "|" {
		t.Errorf("objekt bez vodomjera i kote %q", got)
	}

	// objekt područja kojeg nema: ništa iz datoteke ne ostaje upisano
	mapaRegistara(t, map[string]string{"objekti_bp16.json": `[
		{"code":"P-NOVI","name":"CS Nova","kind":"CRPNA_STANICA","sector_id":"P","area_id":61},
		{"code":"X-1","name":"CS Izvan","kind":"CRPNA_STANICA","sector_id":"P","area_id":99}]`})
	if err := seedStructures(database); err == nil || !strings.Contains(err.Error(), "CS Izvan") {
		t.Fatalf("područje kojeg nema: %v", err)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM structures WHERE code = 'P-NOVI'`); n != 0 {
		t.Error("upis nije vraćen nakon greške")
	}
}
