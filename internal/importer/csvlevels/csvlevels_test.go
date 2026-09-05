package csvlevels

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func pripremi(t *testing.T) (Deps, *repository.ReadingRepository) {
	t.Helper()
	if !db.UseRepoData() {
		t.Skip("data/ s registrima nije dostupan — registri stoje izvan repozitorija")
	}
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "tablica.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedInitialData(database); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(database, "test-node")
	readings := repository.NewReadingRepository(database, rec)
	return Deps{
		Readings:   readings,
		Stations:   repository.NewStationRepository(database, rec),
		Structures: repository.NewStructureRepository(database, rec),
	}, readings
}

func napisi(t *testing.T, redci []string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "vodostaji.csv")
	var text string
	for _, r := range redci {
		text += r + "\r\n"
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTablicaDnevnihVodostaja(t *testing.T) {
	deps, readings := pripremi(t)
	ctx := context.Background()

	path := napisi(t, []string{
		"Datum;Dunav - Batina;Drava - Osijek;CS Draž;Nepoznata letva",
		"01.09.2026.;-118;-176;158;5",
		"02.09.2026.;-114;;159;5",
		"03.09.2026.;-117;-168,4;160;5",
		"04.09.2026.;-120;-;161;5",
		"nešto krivo;1;1;1;1",
	})

	o := Options{Path: path, Origin: "COP Osijek — dnevna tablica", DryRun: true, Deps: deps}
	rep, err := Run(ctx, o)
	if err != nil {
		t.Fatalf("izvješće: %v", err)
	}
	if rep.Rows != 4 || rep.BadDates != 1 {
		t.Errorf("redaka %d, nečitljivih datuma %d; očekivano 4 i 1", rep.Rows, rep.BadDates)
	}
	if len(rep.Matched) != 3 {
		t.Fatalf("prepoznatih stupaca %d, očekivana 3: %+v", len(rep.Matched), rep.Matched)
	}
	if len(rep.Unmatched) != 1 || rep.Unmatched[0] != "Nepoznata letva" {
		t.Errorf("neprepoznati stupci: %v", rep.Unmatched)
	}
	if rep.BadValues != 0 {
		t.Errorf("prazna ćelija i crtica nisu greška, a prijavljeno ih je %d", rep.BadValues)
	}
	// Batina 4, Osijek 2 (jedno prazno, jedna crtica), CS Draž 4
	if rep.Inserted != 10 {
		t.Errorf("novih očitanja %d, očekivano 10", rep.Inserted)
	}

	// probni prolaz ne smije ništa upisati
	if n, _, _, _ := statistika(t, readings); n != 0 {
		t.Fatalf("probni prolaz je upisao %d očitanja", n)
	}

	// Objekt i njegov vodomjer nose isto ime; očitanje ide na objekt, jer
	// ondje stoje i očitanja iz Baranje
	var draz *Column
	for i := range rep.Matched {
		if rep.Matched[i].Header == "CS Draž" {
			draz = &rep.Matched[i]
		}
	}
	if draz == nil || draz.Key[:10] != "structure:" {
		t.Errorf("CS Draž nije vezan na objekt: %+v", draz)
	}

	// pravi uvoz
	o.DryRun = false
	rep, err = Run(ctx, o)
	if err != nil {
		t.Fatalf("uvoz: %v", err)
	}
	if rep.Inserted != 10 {
		t.Fatalf("upisano %d, očekivano 10", rep.Inserted)
	}
	n, _, prvi, zadnji := statistika(t, readings)
	if n != 10 {
		t.Fatalf("u bazi je %d očitanja, očekivano 10", n)
	}
	if prvi.In(models.Zagreb).Hour() != 7 {
		t.Errorf("jutarnje očitanje nije u 7 sati nego u %s", prvi.In(models.Zagreb).Format("15:04"))
	}
	if zadnji.In(models.Zagreb).Format("02.01.2006.") != "04.09.2026." {
		t.Errorf("zadnji dan je %s", zadnji.In(models.Zagreb).Format("02.01.2006."))
	}

	// ponovni prolaz ne udvostručuje
	rep, err = Run(ctx, Options{Path: path, DryRun: true, Deps: deps})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Inserted != 0 || rep.Skipped != 10 || rep.Conflicts != 0 {
		t.Errorf("ponovni prolaz: novih %d, već zapisanih %d, razlika %d", rep.Inserted, rep.Skipped, rep.Conflicts)
	}
}

// Isto jutro iz dva izvora nosi različit sat: tablica centra fiksnih sedam,
// očitanje s terena vrijeme kad je čovjek bio na letvi. Usporedba mora ići
// po danu, inače bi se isti podatak zapisao dvaput.
func TestPreklapanjePoDanuBezObziraNaSat(t *testing.T) {
	deps, readings := pripremi(t)
	ctx := context.Background()

	batina, err := deps.Stations.GetStationByCode(ctx, "batina")
	if err != nil || batina == nil {
		t.Fatal("nema postaje batina")
	}
	cm := 120
	teren := models.Reading{
		StationID: batina.ID.String(), Source: models.ReadingSourceManual, Origin: models.ReadingOriginGoCOP,
		MeasuredAt: time.Date(2026, 9, 1, 7, 43, 0, 0, models.Zagreb).UTC(), LevelCm: &cm,
	}
	if err := readings.Create(ctx, &teren); err != nil {
		t.Fatal(err)
	}

	path := napisi(t, []string{
		"Datum;Dunav - Batina",
		"01.09.2026.;125",
		"02.09.2026.;126",
	})
	rep, err := Run(ctx, Options{Path: path, DryRun: true, Deps: deps})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 1 || rep.Conflicts != 1 || rep.Inserted != 1 {
		t.Fatalf("očekivano: jedan dan preklapa i razlikuje se, jedan je nov; dobiveno %+v", rep)
	}
	if len(rep.Differs) != 1 || rep.Differs[0].Have != 120 || rep.Differs[0].New != 125 {
		t.Fatalf("razlika nije prijavljena kako treba: %+v", rep.Differs)
	}
	if rep.Differs[0].HaveAt.In(models.Zagreb).Format("15:04") != "07:43" {
		t.Errorf("izvješće ne pokazuje vrijeme zatečenog očitanja: %+v", rep.Differs[0])
	}
}

func TestCitanjeCelije(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		status levelStatus
	}{
		{"120", 120, levelOK},
		{"-118", -118, levelOK},
		{"+95", 95, levelOK},
		{"168,4", 168, levelOK},
		{"120 cm", 120, levelOK},
		{"", 0, levelBlank},
		{"-", 0, levelBlank},
		{"x", 0, levelBlank},
		{"led", 0, levelBlank},
		{"nije očitano", 0, levelBad},
	}
	for _, c := range cases {
		got, st := parseLevel(c.in)
		if st != c.status || (st == levelOK && got != c.want) {
			t.Errorf("%q → (%d, %v), očekivano (%d, %v)", c.in, got, st, c.want, c.status)
		}
	}
}

func TestCitanjeDatuma(t *testing.T) {
	for _, s := range []string{"01.09.2026.", "1.9.2026", "2026-09-01", "46266"} {
		d, ok := parseDate(s)
		if !ok || d.Format("2006-01-02") != "2026-09-01" {
			t.Errorf("%q → %v (%v)", s, d.Format("2006-01-02"), ok)
		}
	}
	if _, ok := parseDate("nešto"); ok {
		t.Error("besmislen datum je prošao")
	}
}

// Tablica izvezena iz Excela na Windowsima nije UTF-8
func TestKodiranjeWindows1250(t *testing.T) {
	raw := []byte{'C', 'S', ' ', 'D', 'r', 'a', 0x9E} // "CS Draž" u windows-1250
	if got := decode(raw); got != "CS Draž" {
		t.Errorf("dobiveno %q", got)
	}
	if got := decode([]byte("CS Draž")); got != "CS Draž" {
		t.Errorf("UTF-8 se ne smije dirati: %q", got)
	}
}

func statistika(t *testing.T, r *repository.ReadingRepository) (int, time.Time, time.Time, time.Time) {
	t.Helper()
	n, prvi, zadnji, err := r.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n, time.Time{}, prvi, zadnji
}
