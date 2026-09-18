package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Veza letve s javnom stranicom prolazi kroz bazu, a uvoznik vidi samo
// letve s uključenim preuzimanjem i zna što na njima već ima
func TestJavnaVezaLetveIPostojecaOcitanja(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "javni.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	stations := NewStationRepository(baza, rec)
	readings := NewReadingRepository(baza, rec)
	ctx := context.Background()

	batina := &models.Station{ID: uuid.New(), Code: "batina", Name: "Batina", JavniURL: "https://vodostaji.voda.hr/Home/PregledVodostajaPostaje?postajaID=424", JavniUvoz: true}
	vukovar := &models.Station{ID: uuid.New(), Code: "vukovar", Name: "Vukovar", JavniURL: "https://vodostaji.voda.hr/Home/PregledVodostajaPostaje?postajaID=426"}
	for _, st := range []*models.Station{batina, vukovar} {
		if err := stations.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	nazad, err := stations.GetStationByID(ctx, batina.ID)
	if err != nil || nazad == nil || nazad.JavniURL == "" || !nazad.JavniUvoz {
		t.Fatalf("veza nije spremljena: %+v (%v)", nazad, err)
	}
	// isključivanje kroz izmjenu
	vukovar.JavniUvoz = true
	if err := stations.UpdateStation(ctx, vukovar); err != nil {
		t.Fatal(err)
	}

	sp := NewJavniSpremiste(baza, readings)
	letve, err := sp.LetveZaPreuzimanje(ctx)
	if err != nil || len(letve) != 2 {
		t.Fatalf("letve za preuzimanje: %d (%v)", len(letve), err)
	}
	vukovar.JavniUvoz = false
	_ = stations.UpdateStation(ctx, vukovar)
	if letve, _ = sp.LetveZaPreuzimanje(ctx); len(letve) != 1 || letve[0].Code != "batina" {
		t.Fatalf("nakon isključivanja: %+v", letve)
	}

	kad := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	cm := -119
	n, err := sp.Upisi(ctx, []models.Reading{{ID: uuid.New(), StationID: batina.ID.String(), MeasuredAt: kad, LevelCm: &cm, Source: models.ReadingSourceImport}})
	if err != nil || n != 1 {
		t.Fatalf("upis: %d %v", n, err)
	}
	ima, err := sp.Postojeca(ctx, batina.ID.String(), kad.Add(-time.Hour), kad.Add(time.Hour))
	if err != nil || !ima[kad.Unix()] || len(ima) != 1 {
		t.Errorf("postojeća: %v (%v)", ima, err)
	}
}
