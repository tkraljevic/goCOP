package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
)

// Sektor područja potvrđuje šifru nove dionice; područje kojeg nema i
// nečitljiva tablica javljaju se kao greška, ne kao prazan sektor.
func TestSektorPodrucja(t *testing.T) {
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewSectionRepository(database, ledger.New(database, "test"))
	ctx := context.Background()
	if s, err := repo.SektorPodrucja(ctx, 1); err != nil || s != "P" {
		t.Errorf("sektor područja 1: %q %v", s, err)
	}
	if _, err := repo.SektorPodrucja(ctx, 2); err == nil || !strings.Contains(err.Error(), "područje 2 ne postoji") {
		t.Errorf("nepostojeće područje: %v", err)
	}
	if _, err := database.Exec(`ALTER TABLE areas RENAME TO nema_areas`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SektorPodrucja(ctx, 1); err == nil || !strings.Contains(err.Error(), "greška pri čitanju područja 1") {
		t.Errorf("bez tablice područja: %v", err)
	}
}
