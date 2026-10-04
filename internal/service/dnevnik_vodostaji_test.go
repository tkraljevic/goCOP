package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Vodostaji na listu dnevnika COP-a uzimaju zadnje očitanje kalendarskog
// dana u hrvatskom vremenu, i kad se sat pomiče: u listopadu dan ima 25
// sati, u ožujku 23. Podaci su izmišljeni: letva Primjerovo.
func TestVodostajiListaKadSeSatPomice(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "vodostaji.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	stations := repository.NewStationRepository(baza, rec)
	readings := repository.NewReadingRepository(baza, rec)
	svc := NewJournalService(repository.NewJournalRepository(baza, rec), stations, readings)
	letva := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo"}
	if err := stations.CreateStation(ctx, letva); err != nil {
		t.Fatal(err)
	}
	upisi := func(kad time.Time, cm int) {
		t.Helper()
		if err := readings.Create(ctx, &models.Reading{ID: uuid.New(), StationID: letva.ID.String(), MeasuredAt: kad.UTC(), LevelCm: &cm,
			Source: models.ReadingSourceManual}); err != nil {
			t.Fatal(err)
		}
	}
	j := &models.Journal{Gauges: "primjerovo"}
	sat := func(g int, m time.Month, d, h, min int) time.Time { return time.Date(g, m, d, h, min, 0, 0, models.Zagreb) }

	// 25. 10. 2026. ima 25 sati: očitanje iz 23:30 je zadnje tog dana
	upisi(sat(2026, time.October, 25, 10, 0), 100)
	upisi(sat(2026, time.October, 25, 23, 30), 120)
	if got := svc.waterLevels(ctx, j, sat(2026, time.October, 25, 12, 0)); got != "Primjerovo: 120 cm" {
		t.Errorf("25. 10.: %q, očekivano „Primjerovo: 120 cm”", got)
	}
	// 29. 3. 2026. ima 23 sata: očitanje iz 00:30 sljedećeg dana nije tog dana
	upisi(sat(2026, time.March, 29, 10, 0), 100)
	upisi(sat(2026, time.March, 30, 0, 30), 140)
	if got := svc.waterLevels(ctx, j, sat(2026, time.March, 29, 12, 0)); got != "Primjerovo: 100 cm" {
		t.Errorf("29. 3.: %q, očekivano „Primjerovo: 100 cm”", got)
	}
}
