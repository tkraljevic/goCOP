package bp16

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func writeJSON(t *testing.T, dir, name string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// registarBP16 diže bazu s onim što uvoz iz evidencije VGI Baranja traži u
// registru, bez stvarnih podataka: sektor B i područje 16 (uvoz objekte traži
// i stvara ondje), izmišljenu crpnu stanicu i ustavu sa šiframa kakve daje
// sjeme (bp16-…) i letvu batina, na koju uvoz preslikava „Dunav - Batina”.
func registarBP16(t *testing.T) (*sql.DB, Deps) {
	t.Helper()
	database, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "uvoz.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Područje 16', 'VGI')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(database, "test-node")
	deps := Deps{
		Readings:   repository.NewReadingRepository(database, rec),
		Stations:   repository.NewStationRepository(database, rec),
		Structures: repository.NewStructureRepository(database, rec),
		Log:        t.Logf,
	}
	ctx := context.Background()
	for _, o := range []models.Structure{
		{Code: "bp16-cs-probni", Name: "CS Probni", Kind: models.StructureKindPumpingStation},
		{Code: "bp16-ustava-probna", Name: "Ustava Probna", Kind: models.StructureKindSluice},
	} {
		o.ID, o.SectorID, o.AreaID, o.Origin = db.StableID("structure", o.Code), "B", 16, models.ReadingOriginBP16
		if err := deps.Structures.CreateStructure(ctx, &o); err != nil {
			t.Fatal(err)
		}
	}
	if err := deps.Stations.CreateStation(ctx, &models.Station{ID: db.StableID("station", "batina"), Code: "batina", Name: "Batina", Watercourse: "Dunav"}); err != nil {
		t.Fatal(err)
	}
	return database, deps
}

// Odluke (8. 10. 2026.), pitanje 13: uvoz BP16 provjerava se na
// izmišljenim objektima, pa se ne preskače bez stvarnih registara.
func TestUvozBP16(t *testing.T) {
	database, deps := registarBP16(t)

	dir := t.TempDir()
	writeJSON(t, dir, "crpne_stanice", []map[string]any{{"id": 5, "naziv": "CS Probni", "status": "published"}, {"id": 13, "naziv": "Neka CS 2", "status": "archived"}})
	writeJSON(t, dir, "ustave", []map[string]any{{"id": 4, "naziv": "Ustava Probna", "status": "published"}})
	writeJSON(t, dir, "vodomjerna_letva", []map[string]any{{"id": 1, "naziv": "Dunav - Batina"}, {"id": 8, "naziv": "Dunav - Mohacs (HU)"}, {"id": 99, "naziv": "Nepoznata letva"}})
	writeJSON(t, dir, "vodostaji_na_crpnim_stanicama", []map[string]any{
		{"id": 100, "crpna_stanica": 5, "datum": "2026-09-04", "vrijeme": "07:25:00", "stanje_cs": "pokretanje cs", "vodostaj": 123, "ag_1": 10, "napomena": "Očitao Ivo Ivić.", "date_created": "2026-09-04T05:46:05.000Z"},
		{"id": 101, "crpna_stanica": 5, "datum": "2026-09-03", "vrijeme": "07:00:00", "stanje_cs": "mirovanje", "vodostaj": nil, "napomena": "Nema letve", "date_created": "2026-09-03T05:46:05.000Z"},
		{"id": 102, "crpna_stanica": 13, "datum": "2026-09-03", "vrijeme": "07:00:00", "stanje_cs": "mirovanje", "vodostaj": 5},
	})
	writeJSON(t, dir, "vodostaji_na_ustavama", []map[string]any{
		{"id": 200, "ustava": 4, "datum": "2026-09-04", "vrijeme": "09:57:00", "vodostaj_uzvodni": 28, "vodostaj_nizvodni": 7, "zapornica": "do", "napomena": "Očitao i radio Pero Perić.", "date_created": "2026-09-04T07:59:21.000Z"},
	})
	writeJSON(t, dir, "ostali_vodostaji", []map[string]any{
		{"id": 300, "vodomjer": 1, "datum": "2026-09-04", "vrijeme": "07:00:00", "vodostaj": 310, "napomena": "Telemetrija"},
		{"id": 301, "vodomjer": 8, "datum": "2026-09-04", "vrijeme": "06:00:00", "vodostaj": 420, "napomena": "Očitao Ana Anić."},
		{"id": 302, "vodomjer": 99, "datum": "2026-09-04", "vrijeme": "06:00:00", "vodostaj": 1},
	})

	rep, err := Run(context.Background(), DirSource{Dir: dir}, deps)
	if err != nil {
		t.Fatalf("uvoz: %v", err)
	}
	if rep.Inserted != 5 || rep.Fetched != 5 {
		t.Fatalf("očekivano 5 upisanih, dobiveno %+v", rep)
	}
	if rep.Unmapped["crpna_stanica:13"] != 1 || rep.Unmapped["vodomjer:99"] != 1 {
		t.Errorf("nepreslikani zapisi nisu prijavljeni: %v", rep.Unmapped)
	}
	if len(rep.NewStations) != 1 || rep.NewStations[0] != "Mohács (HU)" {
		t.Errorf("očekivana nova postaja Mohács, dobiveno %v", rep.NewStations)
	}

	ctx := context.Background()
	probni, err := deps.Structures.GetByCode(ctx, "bp16-cs-probni")
	if err != nil || probni == nil {
		t.Fatalf("CS Probni nije u registru: %v", err)
	}
	rs, err := deps.Readings.List(ctx, repository.ReadingFilter{StructureID: probni.ID.String()})
	if err != nil || len(rs) != 2 {
		t.Fatalf("očekivana 2 očitanja na CS Probni, dobiveno %d (%v)", len(rs), err)
	}
	first := rs[0]
	if first.LevelCm == nil || *first.LevelCm != 123 || first.Observer != "Ivo Ivić" || first.Note != "" ||
		first.StructureState != models.StructureStateStarting || first.AgHours1 == nil || *first.AgHours1 != 10 {
		t.Errorf("očitanje CS krivo preslikano: %+v", first)
	}
	want := time.Date(2026, 9, 4, 7, 25, 0, 0, models.Zagreb)
	if !first.MeasuredAt.Equal(want) {
		t.Errorf("vrijeme: dobiveno %s, očekivano %s", first.MeasuredAt, want)
	}
	if first.Source != models.ReadingSourceManual || first.Origin != models.ReadingOriginBP16 {
		t.Errorf("uvezeno ručno očitanje mora ostati RUČNO s podrijetlom BP16: %+v", first)
	}
	if rs[1].LevelCm != nil || rs[1].Note != "Nema letve" {
		t.Errorf("očitanje bez vodostaja mora čuvati napomenu: %+v", rs[1])
	}

	ustava, _ := deps.Structures.GetByCode(ctx, "bp16-ustava-probna")
	us, _ := deps.Readings.List(ctx, repository.ReadingFilter{StructureID: ustava.ID.String()})
	if len(us) != 1 || us[0].Gate != models.GatePartial || us[0].Level2Cm == nil || *us[0].Level2Cm != 7 ||
		us[0].Observer != "Pero Perić" || us[0].Note != "i radio" {
		t.Errorf("očitanje ustave krivo preslikano: %+v", us)
	}

	batina, _ := deps.Stations.GetStationByCode(ctx, "batina")
	bs, _ := deps.Readings.List(ctx, repository.ReadingFilter{StationID: batina.ID.String()})
	if len(bs) != 1 || bs[0].Source != models.ReadingSourceAutomatic {
		t.Errorf("napomena „Telemetrija“ mora dati automatski izvor: %+v", bs)
	}
	mohacs, _ := deps.Stations.GetStationByCode(ctx, "mohacs")
	if mohacs == nil || !mohacs.NeedsReview {
		t.Fatalf("Mohács mora biti stvoren kao postaja koja traži pregled")
	}

	// Ponovni uvoz ne smije duplicirati ni ostaviti nove verzije
	var before int
	database.QueryRow("SELECT COUNT(*) FROM record_versions WHERE entity = 'readings'").Scan(&before)
	rep2, err := Run(ctx, DirSource{Dir: dir}, deps)
	if err != nil || rep2.Inserted != 0 || rep2.Skipped != 5 {
		t.Fatalf("ponovni uvoz: %+v, %v", rep2, err)
	}
	var after int
	database.QueryRow("SELECT COUNT(*) FROM record_versions WHERE entity = 'readings'").Scan(&after)
	if before != 5 || after != before {
		t.Errorf("verzije: prije %d, poslije %d", before, after)
	}
}

func TestSplitNote(t *testing.T) {
	cases := []struct{ in, observer, note string }{
		{"Očitao Ivo Ivić.", "Ivo Ivić", ""},
		{"Očitala Iva Ivić", "Iva Ivić", ""},
		{"Očitao i radio Marko Markić.", "Marko Markić", "i radio"},
		{"Radio Ivo Ivić u noćnoj tarifi.", "", "Radio Ivo Ivić u noćnoj tarifi."},
		{"Očitao Pero Perić (kanal je prazan)", "Pero Perić", "kanal je prazan"},
		{"Očitao Ana Anić. Zapornica otvorena 10 cm", "Ana Anić", "Zapornica otvorena 10 cm"},
		{"Očitao Marko Markić - otvorene obje zapornice 10-ak cm", "Marko Markić", "otvorene obje zapornice 10-ak cm"},
		{"Očitao Marko", "Marko", ""},
		{"0", "", ""},
	}
	for _, c := range cases {
		s := c.in
		o, n := splitNote(&s)
		if o != c.observer || n != c.note {
			t.Errorf("%q → (%q, %q), očekivano (%q, %q)", c.in, o, n, c.observer, c.note)
		}
	}
}
