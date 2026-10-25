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
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "javni.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
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
	// vodostaji koje letva ima, od kojih se mjeri skok; očitanje bez vodostaja ne ulazi
	temp := 12.0
	if _, err := sp.Upisi(ctx, []models.Reading{{ID: uuid.New(), StationID: batina.ID.String(), MeasuredAt: kad.Add(time.Hour), TempC: &temp, Source: models.ReadingSourceImport}}); err != nil {
		t.Fatal(err)
	}
	razine, err := sp.Razine(ctx, batina.ID.String(), kad.Add(-time.Hour), kad.Add(2*time.Hour))
	if err != nil || len(razine) != 1 || razine[kad.Unix()] != -119 {
		t.Errorf("razine: %v (%v)", razine, err)
	}
	// uz razdoblje i zadnja razina prije njega, kako god davno bila (rupa)
	staro, prije := kad.Add(-30*time.Hour), -123
	if _, err := sp.Upisi(ctx, []models.Reading{
		{ID: uuid.New(), StationID: batina.ID.String(), MeasuredAt: staro, LevelCm: &prije, Source: models.ReadingSourceImport},
		{ID: uuid.New(), StationID: batina.ID.String(), MeasuredAt: staro.Add(-time.Hour), LevelCm: &prije, Source: models.ReadingSourceImport},
	}); err != nil {
		t.Fatal(err)
	}
	razine, err = sp.Razine(ctx, batina.ID.String(), kad.Add(3*time.Hour), kad.Add(4*time.Hour))
	if err != nil || len(razine) != 1 || razine[kad.Unix()] != -119 {
		t.Errorf("zadnja prije razdoblja: %v (%v)", razine, err)
	}
	razine, err = sp.Razine(ctx, batina.ID.String(), kad.Add(-time.Hour), kad)
	if err != nil || len(razine) != 2 || razine[staro.Unix()] != -123 {
		t.Errorf("rupa prije razdoblja: %v (%v)", razine, err)
	}
	// skok se bilježi za stranicu letve i broji posebno
	skok := models.OdbacenSkok(models.Reading{StationID: batina.ID.String(), MeasuredAt: kad.Add(2 * time.Hour), LevelCm: &cm, Origin: "vodostaji.voda.hr"})
	if err := sp.ZabiljeziOdbacene(ctx, []models.OdbacenaVrijednost{skok}); err != nil {
		t.Fatal(err)
	}
	s, err := readings.SazetakOdbacenih(ctx, "station:"+batina.ID.String())
	if err != nil || s == nil || s.Broj != 1 || s.Skokova != 1 || s.Zadnja.Polje != models.PoljeSkok {
		t.Errorf("sažetak: %+v (%v)", s, err)
	}
	if _, err := sp.Razine(ctx, batina.ID.String(), kad, kad); err != nil {
		t.Fatal(err)
	}
	baza.Close()
	if _, err := sp.Razine(ctx, batina.ID.String(), kad, kad); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}
