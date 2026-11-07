package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Sažetak gleda samo letve s uključenim preuzimanjem i samo preuzeta mjerenja u
// razdoblju, a vodostaj i protok broji odvojeno.
func TestMjerenjaPreuzimanih(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "preuzimane.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	stations := NewStationRepository(baza, rec)
	readings := NewReadingRepository(baza, rec)
	ctx := context.Background()

	javna := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", JavniURL: "https://izvor.example/1", JavniUvoz: true}
	radar := &models.Station{ID: uuid.New(), Code: "radarovo", Name: "Radarovo", TelemetrijaSite: "42", TelemetrijaUvoz: true}
	iskljucena := &models.Station{ID: uuid.New(), Code: "tisovo", Name: "Tisovo", JavniURL: "https://izvor.example/2"}
	bezAdrese := &models.Station{ID: uuid.New(), Code: "pusto", Name: "Pusto", JavniUvoz: true}
	samoRucna := &models.Station{ID: uuid.New(), Code: "rucnovo", Name: "Ručnovo", JavniURL: "https://izvor.example/3", JavniUvoz: true}
	for _, st := range []*models.Station{javna, radar, iskljucena, bezAdrese, samoRucna} {
		if err := stations.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	od := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	do := od.AddDate(0, 0, 14)
	upisi := func(st *models.Station, kad time.Time, cm *int, q *float64) {
		t.Helper()
		rd := models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: kad, LevelCm: cm, FlowM3s: q, Source: models.ReadingSourceImport}
		if err := readings.Create(ctx, &rd); err != nil {
			t.Fatal(err)
		}
	}
	cm, q := 100, 50.0
	upisi(javna, od.Add(-time.Hour), &cm, &q)                       // prije razdoblja
	upisi(javna, od, &cm, nil)                                      // na samoj granici
	upisi(javna, od.Add(2*time.Hour), &cm, &q)                      // oboje
	upisi(javna, od.Add(3*time.Hour), &cm, nil)                     // protoka više nema
	upisi(javna, do.Add(time.Hour), &cm, &q)                        // poslije razdoblja
	upisi(radar, od.Add(time.Hour), nil, &q)                        // samo protok
	upisi(iskljucena, od.Add(time.Hour), &cm, &q)                   // preuzimanje isključeno
	upisi(bezAdrese, od.Add(time.Hour), &cm, &q)                    // uključeno, ali bez adrese
	for _, st := range []*models.Station{javna, radar, samoRucna} { // ručni upis ne govori ništa o izvoru
		rd := models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: od.Add(5 * time.Hour), LevelCm: &cm, FlowM3s: &q, Source: models.ReadingSourceManual}
		if err := readings.Create(ctx, &rd); err != nil {
			t.Fatal(err)
		}
	}

	letve, err := readings.MjerenjaPreuzimanih(ctx, od, do)
	if err != nil {
		t.Fatal(err)
	}
	if len(letve) != 2 || letve[0].Naziv != "Primjerovo" || letve[1].Naziv != "Radarovo" {
		t.Fatalf("letve: %+v", letve)
	}
	v, p := letve[0].Vodostaj, letve[0].Protok
	if v.Broj != 3 || !v.Prvo.Equal(od) || !v.Zadnje.Equal(od.Add(3*time.Hour)) {
		t.Errorf("vodostaj: %+v", v)
	}
	if p.Broj != 1 || !p.Prvo.Equal(od.Add(2*time.Hour)) || !p.Zadnje.Equal(od.Add(2*time.Hour)) {
		t.Errorf("protok: %+v", p)
	}
	if v := letve[1].Vodostaj; v.Broj != 0 || !v.Zadnje.IsZero() {
		t.Errorf("letva bez vodostaja: %+v", v)
	}
	if p := letve[1].Protok; p.Broj != 1 {
		t.Errorf("letva samo s protokom: %+v", p)
	}
}

// Greške čitanja idu pozivatelju: zatvorena baza i redak koji se ne da pročitati.
func TestMjerenjaPreuzimanihGreske(t *testing.T) {
	baza, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "rucno.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	for _, naredba := range []string{
		`CREATE TABLE stations (id TEXT, name TEXT, javni_uvoz INTEGER, javni_url TEXT, telemetrija_uvoz INTEGER, telemetrija_site TEXT)`,
		`CREATE TABLE readings (station_id TEXT, measured_at TEXT, level_cm INTEGER, flow_m3s REAL, source TEXT)`,
		`INSERT INTO stations VALUES ('s', NULL, 1, 'https://izvor.example/1', 0, '')`,
		`INSERT INTO readings VALUES ('s', '2026-03-10 12:00:00 +0000 UTC', 100, NULL, 'UVOZ')`,
	} {
		if _, err := baza.Exec(naredba); err != nil {
			t.Fatal(err)
		}
	}
	readings := &ReadingRepository{db: baza}
	ctx := context.Background()
	od := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if _, err := readings.MjerenjaPreuzimanih(ctx, od, od.AddDate(0, 1, 0)); err == nil {
		t.Error("letva bez imena: nema greške")
	}
	baza.Close()
	if _, err := readings.MjerenjaPreuzimanih(ctx, od, od.AddDate(0, 1, 0)); err == nil {
		t.Error("zatvorena baza: nema greške")
	}
}

// Napomena o podacima putuje razmjenom kao i ostale opće postavke, a prazna
// vrijednost je miče i s drugog računala.
func TestNapomenaPodatakaStizeRazmjenom(t *testing.T) {
	ctx := context.Background()
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	repoA, repoB := NewAktiRepository(a.db, a.rec), NewAktiRepository(b.db, b.rec)

	if err := repoA.SavePostavka(ctx, PostavkaNapomenaPodataka, "Izvor mijenja adrese."); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if v, err := repoB.GetPostavka(ctx, PostavkaNapomenaPodataka); err != nil || v != "Izvor mijenja adrese." {
		t.Fatalf("napomena nije stigla: %q %v", v, err)
	}
	if err := repoA.SavePostavka(ctx, PostavkaNapomenaPodataka, ""); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if v, err := repoB.GetPostavka(ctx, PostavkaNapomenaPodataka); err != nil || v != "" {
		t.Fatalf("obrisana napomena ostala: %q %v", v, err)
	}
}
