package bp16

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava kako uvoz očitanja iz evidencije Baranje stvara objekte i
// veže ih na letve: letva istog imena, zajednička letva crpne stanice i
// ustave, glavni dovodni kanal istoimene ustave, par uzvodno i nizvodno;
// postojećem objektu dopunjuje samo prazne veze. Uz to zapise s krivim
// datumom, serije od tisuću očitanja i greške izvora i baze.

// registarLetvi diže bazu s tri postojeća objekta i letvama po imenu
func registarLetvi(t *testing.T) (*sql.DB, Deps) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "letve.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Područje 16', 'VGI')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cvor-b")
	deps := Deps{
		Readings:   repository.NewReadingRepository(baza, rec),
		Stations:   repository.NewStationRepository(baza, rec),
		Structures: repository.NewStructureRepository(baza, rec),
	}
	ctx := context.Background()
	for sifra, ime := range map[string]string{
		"cs-probni-letva": "CS Probni",
		"probna-uzv":      "Ustava Probna (uzvodno)",
		"probna-niz":      "Ustava Probna (nizvodno)",
		"vezana-rucno":    "Vezana ručno",
		"vezana-druga":    "CS Vezana",
		"zmajevac":        "CS i ustava Zmajevac",
		"puskas-gdk":      "Ustava Puškaš (uzvodno)",
		"kopacevo-uzv":    "Ustava Kopačevo (uzvodno)",
		"kopacevo-niz":    " Ustava Kopačevo (nizvodno) ",
	} {
		if err := deps.Stations.CreateStation(ctx, &models.Station{ID: db.StableID("station", sifra), Code: sifra, Name: ime}); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range []models.Structure{
		{Code: "bp16-cs-probni", Name: "CS Probni", Kind: models.StructureKindPumpingStation},
		{Code: "bp16-ustava-probna", Name: "Ustava Probna", Kind: models.StructureKindSluice},
		// ručno postavljena letva se ne dira
		{Code: "bp16-cs-vezana", Name: "CS Vezana", Kind: models.StructureKindPumpingStation, StationID: db.StableID("station", "vezana-rucno").String()},
	} {
		o.ID, o.SectorID, o.AreaID, o.Origin = db.StableID("structure", o.Code), "B", 16, models.ReadingOriginBP16
		if err := deps.Structures.CreateStructure(ctx, &o); err != nil {
			t.Fatal(err)
		}
	}
	return baza, deps
}

// evidencijaLetvi su šifrarnici i očitanja za registarLetvi
func evidencijaLetvi() probniIzvor {
	return probniIzvor{zbirke: map[string][]json.RawMessage{
		"crpne_stanice": raw(
			`{"id":1,"naziv":"CS Probni","status":"published"}`,
			`{"id":2,"naziv":"CS Vezana","status":"published"}`,
			`{"id":3,"naziv":"CS Zmajevac","status":"published"}`,
			`{"id":4,"naziv":" CS Puškaš ","status":"published"}`,
			`{"id":5,"naziv":"CS Sama","status":"published"}`,
			`{"id":6,"naziv":"CS Arhivska","status":"archived"}`,
		),
		"ustave": raw(
			`{"id":1,"naziv":"Ustava Probna","status":"published"}`,
			`{"id":2,"naziv":"Ustava Kopačevo","status":"published"}`,
			`{"id":3,"naziv":"Ustava Zmajevac","status":"published"}`,
		),
		"vodomjerna_letva": raw(
			`{"id":1,"naziv":"Dunav - Tikveš"}`,
			`{"id":2,"naziv":"Dunav - Budapest (HU)"}`,
		),
		"vodostaji_na_crpnim_stanicama": raw(
			`{"id":1,"crpna_stanica":3,"datum":"2025-06-01","vrijeme":"07:00:00","vodostaj":120}`,
			`{"id":2,"crpna_stanica":1,"datum":"01.06.2025.","vrijeme":"07:00:00","vodostaj":121}`,
		),
		"vodostaji_na_ustavama": raw(
			`{"id":1,"ustava":2,"datum":"2025-06-01","vrijeme":"08:00:00","vodostaj_uzvodni":150,"vodostaj_nizvodni":90,"zapornica":"z"}`,
			`{"id":2,"ustava":77,"datum":"2025-06-01","vrijeme":"08:00:00","vodostaj_uzvodni":1}`,
			`{"id":3,"ustava":2,"datum":"2025-6-1","vrijeme":"08:00:00","vodostaj_uzvodni":2}`,
		),
		"ostali_vodostaji": raw(
			`{"id":1,"vodomjer":2,"datum":"2025-06-01","vrijeme":"06:00:00","vodostaj":400}`,
			`{"id":2,"vodomjer":2,"datum":"x","vrijeme":"06:00:00","vodostaj":401}`,
			`{"id":3,"vodomjer":1,"datum":"2025-06-01","vrijeme":"06:00:00","vodostaj":402}`,
		),
	}}
}

func letvaID(sifra string) string { return db.StableID("station", sifra).String() }

func TestUvozBP16StvaraObjekteNaLetvama(t *testing.T) {
	_, deps := registarLetvi(t)
	ctx := context.Background()
	var zapisnik []string
	deps.Log = func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) }
	deps.StvoriObjekte = true

	dopunjene := []string{"bp16-cs-probni → cs-probni-letva", "bp16-ustava-probna → probna-uzv / nizvodno probna-niz"}
	novi := []string{
		"bp16-cs-zmajevac → zmajevac", "bp16-cs-puskas → puskas-gdk", "bp16-cs-sama (bez letve)",
		"bp16-ustava-kopacevo → kopacevo-uzv / nizvodno kopacevo-niz", "bp16-ustava-zmajevac → zmajevac",
	}
	provjeri := func(rep Report) {
		t.Helper()
		if rep.Fetched != 3 || rep.Inserted != 3 || rep.Skipped != 0 {
			t.Errorf("izvješće: %+v", rep)
		}
		if rep.Unmapped["neispravan datum"] != 3 || rep.Unmapped["ustava:77"] != 1 || rep.Unmapped["vodomjer:1"] != 1 || len(rep.Unmapped) != 3 {
			t.Errorf("nepreslikano: %v", rep.Unmapped)
		}
		if strings.Join(rep.DopunjeneVeze, "; ") != strings.Join(dopunjene, "; ") {
			t.Errorf("dopunjene veze: %q", rep.DopunjeneVeze)
		}
		if strings.Join(rep.NoviObjekti, "; ") != strings.Join(novi, "; ") {
			t.Errorf("novi objekti: %q", rep.NoviObjekti)
		}
		if len(rep.NewStations) != 1 || rep.NewStations[0] != "Budapest (HU)" {
			t.Errorf("nove postaje: %v", rep.NewStations)
		}
	}

	// pregled broji isto, a registar ostaje kakav je bio
	deps.DryRun = true
	rep, err := Run(ctx, evidencijaLetvi(), deps)
	if err != nil {
		t.Fatal(err)
	}
	provjeri(rep)
	if o, _ := deps.Structures.GetByCode(ctx, "bp16-cs-probni"); o == nil || o.StationID != "" {
		t.Errorf("pregled dopunio letvu: %+v", o)
	}
	if o, _ := deps.Structures.GetByCode(ctx, "bp16-cs-zmajevac"); o != nil {
		t.Error("pregled stvorio objekt")
	}
	if st, _ := deps.Stations.GetStationByCode(ctx, "budapest"); st != nil {
		t.Error("pregled stvorio postaju")
	}

	deps.DryRun = false
	zapisnik = nil
	rep, err = Run(ctx, evidencijaLetvi(), deps)
	if err != nil {
		t.Fatal(err)
	}
	provjeri(rep)
	z := strings.Join(zapisnik, "\n")
	for _, poruka := range []string{
		"Uvoz BP16: objekt „CS Probni“ dobiva letve (bp16-cs-probni → cs-probni-letva)",
		"Uvoz BP16: stvoren objekt „CS Sama“ (bp16-cs-sama (bez letve))",
		"Uvoz BP16: postaja „Dunav - Tikveš“ (tikves) ne postoji u registru — očitanja se preskaču",
		"Uvoz BP16: stvorena postaja „Budapest (HU)“ (budapest)",
	} {
		if !strings.Contains(z, poruka) {
			t.Errorf("u zapisniku nema %q:\n%s", poruka, z)
		}
	}

	for sifra, letve := range map[string][2]string{
		"bp16-cs-probni":       {letvaID("cs-probni-letva"), ""},
		"bp16-ustava-probna":   {letvaID("probna-uzv"), letvaID("probna-niz")},
		"bp16-cs-vezana":       {letvaID("vezana-rucno"), ""},
		"bp16-cs-zmajevac":     {letvaID("zmajevac"), ""},
		"bp16-cs-puskas":       {letvaID("puskas-gdk"), ""},
		"bp16-cs-sama":         {"", ""},
		"bp16-ustava-kopacevo": {letvaID("kopacevo-uzv"), letvaID("kopacevo-niz")},
		"bp16-ustava-zmajevac": {letvaID("zmajevac"), ""},
	} {
		o, err := deps.Structures.GetByCode(ctx, sifra)
		if err != nil || o == nil || o.StationID != letve[0] || o.StationDownID != letve[1] {
			t.Errorf("%s: %+v, %v", sifra, o, err)
		}
	}
	if o, _ := deps.Structures.GetByCode(ctx, "bp16-cs-arhivska"); o != nil {
		t.Error("arhivirana crpna stanica ne stvara objekt")
	}
	kopacevo, _ := deps.Structures.GetByCode(ctx, "bp16-ustava-kopacevo")
	if kopacevo.Name != "Ustava Kopačevo" || kopacevo.Kind != models.StructureKindSluice || kopacevo.SectorID != "B" || kopacevo.AreaID != 16 || kopacevo.Origin != models.ReadingOriginBP16 {
		t.Errorf("nova ustava: %+v", kopacevo)
	}
	us, _ := deps.Readings.List(ctx, repository.ReadingFilter{StructureID: kopacevo.ID.String()})
	if len(us) != 1 || *us[0].LevelCm != 150 || *us[0].Level2Cm != 90 || us[0].Gate != models.GateClosed {
		t.Errorf("očitanje ustave: %+v", us)
	}
	budapest, _ := deps.Stations.GetStationByCode(ctx, "budapest")
	if budapest == nil || !budapest.NeedsReview || budapest.SourceName != "Dunav - Budapest (HU)" || budapest.ZeroDatumSystem != "TRST" ||
		budapest.ZeroDatumNewSystem != "HVRS71" || budapest.Watercourse != "Dunav" {
		t.Errorf("nova postaja: %+v", budapest)
	}

	// ponovni uvoz: veze su već tu, objekti i očitanja također
	rep, err = Run(ctx, evidencijaLetvi(), deps)
	if err != nil || len(rep.DopunjeneVeze) != 0 || len(rep.NoviObjekti) != 0 || len(rep.NewStations) != 0 || rep.Skipped != 3 || rep.Inserted != 0 {
		t.Errorf("ponovni uvoz: %+v, %v", rep, err)
	}
}

// Bez stvaranja objekata očitanja objekata kojih nema se preskaču i to se
// zapiše; bez zapisnika uvoz radi isto.
func TestUvozBP16BezStvaranjaObjekata(t *testing.T) {
	_, deps := registarLetvi(t)
	ctx := context.Background()
	deps.DryRun = true
	var zapisnik []string
	deps.Log = func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) }
	rep, err := Run(ctx, evidencijaLetvi(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Inserted != 1 || len(rep.NoviObjekti) != 0 || len(rep.DopunjeneVeze) != 0 || rep.Unmapped["crpna_stanica:3"] != 1 || rep.Unmapped["ustava:2"] != 2 {
		t.Errorf("izvješće: %+v", rep)
	}
	if z := strings.Join(zapisnik, "\n"); !strings.Contains(z, "Uvoz BP16: crpna stanica „ CS Puškaš “ nema objekt u registru — očitanja se preskaču") ||
		!strings.Contains(z, "Uvoz BP16: ustava „Ustava Kopačevo“ nema objekt u registru") || strings.Contains(z, "CS Arhivska") {
		t.Errorf("zapisnik:\n%s", z)
	}
	deps.Log = nil
	if bez, err := Run(ctx, evidencijaLetvi(), deps); err != nil || bez.Inserted != rep.Inserted || len(bez.Unmapped) != len(rep.Unmapped) {
		t.Errorf("bez zapisnika: %+v, %v", bez, err)
	}
}

// Očitanja se upisuju u serijama od tisuću: puna serija se upiše odmah, a
// ostatak na kraju.
func TestUvozBP16PoTisucu(t *testing.T) {
	_, deps := registarLetvi(t)
	src := evidencijaLetvi()
	var redci []string
	for i := 1; i <= 1001; i++ {
		redci = append(redci, fmt.Sprintf(`{"id":%d,"crpna_stanica":1,"datum":"2025-05-%02d","vrijeme":"07:00:00","vodostaj":%d}`, i, 1+i%28, 100+i%50))
	}
	src.zbirke["vodostaji_na_crpnim_stanicama"] = raw(redci...)
	deps.DryRun = true
	rep, err := Run(context.Background(), src, deps)
	if err != nil || rep.Fetched != 1002 || rep.Inserted != 1002 {
		t.Errorf("pregled: %+v, %v", rep, err)
	}
}

// Greška izvora ili baze prekida uvoz.
func TestUvozBP16Greske(t *testing.T) {
	ctx := context.Background()
	for _, zbirka := range []string{"crpne_stanice", "ustave", "vodomjerna_letva", "vodostaji_na_crpnim_stanicama", "vodostaji_na_ustavama", "ostali_vodostaji"} {
		_, deps := registarLetvi(t)
		_, err := Run(ctx, pokvareniIzvor{probniIzvor: evidencijaLetvi(), greske: map[string]error{zbirka: errors.New("Directus odgovorio 403")}}, deps)
		if err == nil || err.Error() != "Directus odgovorio 403" {
			t.Errorf("%s: %v", zbirka, err)
		}
	}
	// zapis koji nije JSON ne da se ni prepisati
	_, deps := registarLetvi(t)
	src := evidencijaLetvi()
	src.zbirke["ustave"] = raw(`{"id":`)
	if _, err := Run(ctx, src, deps); err == nil || !strings.Contains(err.Error(), "MarshalJSON") {
		t.Errorf("nečitljiv zapis: %v", err)
	}

	// baza se ne da čitati
	zatvorena, deps := registarLetvi(t)
	zatvorena.Close()
	if _, err := Run(ctx, evidencijaLetvi(), deps); err == nil {
		t.Error("zatvorena baza je prošla")
	}
	for _, tablica := range []string{"stations", "readings"} {
		baza, deps := registarLetvi(t)
		if _, err := baza.Exec(`PRAGMA foreign_keys = OFF; DROP TABLE ` + tablica); err != nil {
			t.Fatal(err)
		}
		if _, err := Run(ctx, evidencijaLetvi(), deps); err == nil || !strings.Contains(err.Error(), "no such table: "+tablica) {
			t.Errorf("bez tablice %s: %v", tablica, err)
		}
	}

	// baza odbija upis: okidač glumi pun disk
	puno := evidencijaLetvi()
	var redci []string
	for i := 1; i <= 1000; i++ {
		redci = append(redci, fmt.Sprintf(`{"id":%d,"crpna_stanica":1,"datum":"2025-05-%02d","vrijeme":"07:00:00","vodostaj":100}`, i, 1+i%28))
	}
	puno.zbirke["vodostaji_na_crpnim_stanicama"] = raw(redci...)
	skoroPuno := evidencijaLetvi()
	skoroPuno.zbirke["vodostaji_na_crpnim_stanicama"] = raw(redci[:999]...)
	doVodomjera := evidencijaLetvi()
	doVodomjera.zbirke["vodostaji_na_crpnim_stanicama"] = raw(redci[:998]...)
	for _, s := range []struct {
		ime, okidac, poruka string
		src                 probniIzvor
	}{
		{"letve objekta", `BEFORE UPDATE ON structures`, "letve objekta CS Probni: ", evidencijaLetvi()},
		{"novi objekt", `BEFORE INSERT ON structures`, "stvaranje objekta CS Zmajevac: ", evidencijaLetvi()},
		{"nova ustava", `BEFORE INSERT ON structures WHEN NEW.kind = 'USTAVA'`, "stvaranje objekta Ustava Kopačevo: ", evidencijaLetvi()},
		{"nova postaja", `BEFORE INSERT ON stations`, "stvaranje postaje budapest: ", evidencijaLetvi()},
		{"zadnja serija", `BEFORE INSERT ON readings`, "greška pri uvozu očitanja", evidencijaLetvi()},
		{"puna serija crpnih stanica", `BEFORE INSERT ON readings`, "directus:vodostaji_na_crpnim_stanicama:1:", puno},
		{"puna serija s ustavom", `BEFORE INSERT ON readings`, "directus:vodostaji_na_crpnim_stanicama:1:", skoroPuno},
		{"puna serija s vodomjerom", `BEFORE INSERT ON readings`, "directus:vodostaji_na_crpnim_stanicama:1:", doVodomjera},
	} {
		baza, deps := registarLetvi(t)
		deps.StvoriObjekte = true
		if _, err := baza.Exec(`CREATE TRIGGER pun_disk ` + s.okidac + ` BEGIN SELECT RAISE(ABORT, 'disk pun'); END`); err != nil {
			t.Fatal(err)
		}
		rep, err := Run(ctx, s.src, deps)
		if err == nil || !strings.Contains(err.Error(), s.poruka) || !strings.Contains(err.Error(), "disk pun") {
			t.Errorf("%s: %v", s.ime, err)
		}
		if rep.Inserted != 0 {
			t.Errorf("%s: upisano %d", s.ime, rep.Inserted)
		}
	}
}
