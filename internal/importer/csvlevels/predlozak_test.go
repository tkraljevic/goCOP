package csvlevels

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/repository"
)

// Predložak tablice dnevnih vodostaja iz docs/predlosci/ocitanja uvozi se na
// bazu napunjenu iz predložaka prvog pokretanja: stupci su postaja s dionice
// i crpna stanica iz registra objekata.
func TestPredlozakTablice(t *testing.T) {
	predlosci := filepath.Join("..", "..", "..", "docs", "predlosci")
	staraMapa, stariImenik := db.DataDir, db.ImenikPath
	t.Cleanup(func() { db.DataDir, db.ImenikPath = staraMapa, stariImenik })
	db.DataDir = filepath.Join(predlosci, "prvo-pokretanje")
	db.ImenikPath = filepath.Join(db.DataDir, "imenik.json")

	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "tablica.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test-node")
	deps := Deps{
		Readings:   repository.NewReadingRepository(baza, rec),
		Stations:   repository.NewStationRepository(baza, rec),
		Structures: repository.NewStructureRepository(baza, rec),
	}
	o := Options{Path: filepath.Join(predlosci, "ocitanja", "tablica-dnevnih-vodostaja.csv"),
		Origin: "predložak", DryRun: true, Deps: deps}

	rep, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows != 4 || len(rep.Matched) != 2 || len(rep.Unmatched) != 0 || len(rep.Ambiguous) != 0 {
		t.Fatalf("redaka %d, stupaca %d, neprepoznatih %v, dvoznačnih %v", rep.Rows, len(rep.Matched), rep.Unmatched, rep.Ambiguous)
	}
	// prazna ćelija i crtica nisu greška nego izostanak očitanja
	if rep.BadDates != 0 || rep.BadValues != 0 || rep.Inserted != 6 {
		t.Errorf("loših datuma %d, loših vrijednosti %d, novih %d (očekivano 0, 0, 6)", rep.BadDates, rep.BadValues, rep.Inserted)
	}

	o.DryRun = false
	if rep, err = Run(context.Background(), o); err != nil || rep.Inserted != 6 {
		t.Fatalf("upis: %d (%v)", rep.Inserted, err)
	}
}
