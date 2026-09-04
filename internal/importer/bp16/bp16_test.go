package bp16

import (
	"context"
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

func TestUvozBP16(t *testing.T) {
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "uvoz.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedInitialData(database); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(database, "test-node")
	deps := Deps{
		Readings:   repository.NewReadingRepository(database, rec),
		Stations:   repository.NewStationRepository(database, rec),
		Structures: repository.NewStructureRepository(database, rec),
		Log:        t.Logf,
	}

	dir := t.TempDir()
	writeJSON(t, dir, "crpne_stanice", []map[string]any{{"id": 5, "naziv": "CS Draž", "status": "published"}, {"id": 13, "naziv": "Neka CS 2", "status": "archived"}})
	writeJSON(t, dir, "ustave", []map[string]any{{"id": 4, "naziv": "Ustava Bilje", "status": "published"}})
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
	draz, err := deps.Structures.GetByCode(ctx, "bp16-cs-draz")
	if err != nil || draz == nil {
		t.Fatalf("CS Draž nije u registru: %v", err)
	}
	rs, err := deps.Readings.List(ctx, repository.ReadingFilter{StructureID: draz.ID.String()})
	if err != nil || len(rs) != 2 {
		t.Fatalf("očekivana 2 očitanja na CS Draž, dobiveno %d (%v)", len(rs), err)
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

	bilje, _ := deps.Structures.GetByCode(ctx, "bp16-ustava-bilje")
	us, _ := deps.Readings.List(ctx, repository.ReadingFilter{StructureID: bilje.ID.String()})
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
		{"Očitala Mara Marić", "Mara Marić", ""},
		{"Očitao i radio Luka Lukić.", "Luka Lukić", "i radio"},
		{"Radio Josip Josić u noćnoj tarifi.", "", "Radio Josip Josić u noćnoj tarifi."},
		{"Očitao Pero Perić (kanal je prazan)", "Pero Perić", "kanal je prazan"},
		{"Očitao Ana Anić. Zapornica otvorena 10 cm", "Ana Anić", "Zapornica otvorena 10 cm"},
		{"Očitao Karlo Karlić - otvorene obje zapornice 10-ak cm", "Karlo Karlić", "otvorene obje zapornice 10-ak cm"},
		{"Očitao Kazimir", "Kazimir", ""},
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
