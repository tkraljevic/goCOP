package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// Oznaka izdanja govori da je očitanje uloženo u arhivu: od tada se ne mora
// sinkronizirati ni čuvati, jer isto stoji u arhivi u 48 bajta umjesto 1.360.
// Mehanizam ulaganja dolazi kasnije, ali zapis ga mora moći izraziti — dodati
// stupac naknadno znači prepisati svako očitanje na svakom čvoru.
func TestIzdanjeOcitanjaPrezivljavaUpisICitanje(t *testing.T) {
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "proba.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := NewReadingRepository(database, ledger.New(database, "cvor"))
	ctx := context.Background()

	cm := func(v int) *int { return &v }
	ulozeno := models.Reading{
		ID: uuid.New(), StationID: "postaja-1", MeasuredAt: time.Now().AddDate(-2, 0, 0),
		LevelCm: cm(300), Source: models.ReadingSourceImport, Origin: models.ReadingOriginArchive,
		Izdanje: "2027",
	}
	tekuce := models.Reading{
		ID: uuid.New(), StationID: "postaja-1", MeasuredAt: time.Now().Add(-time.Hour),
		LevelCm: cm(120), Source: models.ReadingSourceManual, Origin: models.ReadingOriginGoCOP,
	}
	if _, err := repo.ImportBatch(ctx, []models.Reading{ulozeno, tekuce}); err != nil {
		t.Fatal(err)
	}

	vraceno, err := repo.Get(ctx, ulozeno.ID)
	if err != nil {
		t.Fatal(err)
	}
	if vraceno.Izdanje != "2027" {
		t.Errorf("oznaka izdanja je %q, upisano je %q", vraceno.Izdanje, "2027")
	}
	svjeze, err := repo.Get(ctx, tekuce.ID)
	if err != nil {
		t.Fatal(err)
	}
	if svjeze.Izdanje != "" {
		t.Errorf("očitanje koje nije uloženo ima oznaku %q", svjeze.Izdanje)
	}
}
