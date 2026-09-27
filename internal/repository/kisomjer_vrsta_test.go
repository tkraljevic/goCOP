package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/models"
)

// Zapis iz knjige otprije vrste (drugi čvor sa starijim programom) na
// površini postaje izvedena točka.
func TestKisomjerIzKnjigeBezVrste(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "vrsta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	sada := time.Now()
	if err := upsertKisomjer(ctx, tx, models.Kisomjer{Code: "k-a", Naziv: "A", Latitude: 46, Longitude: 14,
		Aktivan: true, CreatedAt: sada, UpdatedAt: sada}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	k, err := NewKisomjerRepository(baza, nil).GetKisomjer(ctx, "k-a")
	if err != nil || k == nil || k.Vrsta != models.KisomjerIzvedeni || k.JeStvarni() {
		t.Fatalf("GetKisomjer = %+v, %v", k, err)
	}
}
