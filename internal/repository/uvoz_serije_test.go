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

// Serija je jedna transakcija: kad padne usred niza, poništena je cijela, pa
// ImportBatch ne smije javiti da je išta upisao. Prije je vraćao broj redaka
// obrađenih do greške, a pozivatelji su ih brojali kao upisane.
func TestImportBatchGreskaUsredSerijeNeBrojiNista(t *testing.T) {
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "primjerica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := NewReadingRepository(database, ledger.New(database, "cvor-p"))
	ctx := context.Background()

	cm := func(v int) *int { return &v }
	ocitanje := func(sati int) models.Reading {
		return models.Reading{
			ID: uuid.New(), StationID: "postaja-primjerica", MeasuredAt: time.Now().Add(-time.Duration(sati) * time.Hour),
			LevelCm: cm(100 + sati), Source: models.ReadingSourceImport, Origin: models.ReadingOriginGoCOP,
		}
	}
	bezIdentifikatora := ocitanje(3)
	bezIdentifikatora.ID = uuid.Nil
	serija := []models.Reading{ocitanje(1), ocitanje(2), bezIdentifikatora, ocitanje(4)}

	n, err := repo.ImportBatch(ctx, serija)
	if err == nil {
		t.Fatal("očitanje bez identifikatora mora srušiti seriju")
	}
	if n != 0 {
		t.Fatalf("poništena serija javlja %d upisanih, a nije upisano ništa", n)
	}
	var ima int
	if err := database.QueryRow(`SELECT COUNT(*) FROM readings`).Scan(&ima); err != nil {
		t.Fatal(err)
	}
	if ima != 0 {
		t.Fatalf("u bazi %d očitanja nakon poništene serije", ima)
	}
}
