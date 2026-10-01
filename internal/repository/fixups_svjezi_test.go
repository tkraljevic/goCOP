package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"

	_ "modernc.org/sqlite"
)

// Novi čvor (Docker, Unraid) kreće s praznom bazom: registar stiže tek
// razmjenom, pa popravci podataka moraju proći i bez njega.
func TestPopravciNaPraznojBazi(t *testing.T) {
	baza, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if err := RunFixups(context.Background(), baza, ledger.New(baza, "svjezi")); err != nil {
		t.Fatalf("popravci na praznoj bazi: %v", err)
	}
}
