package bp16

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/repository"
)

// Serija koja padne poništena je cijela, pa izvješće uvoza ne smije njezina
// očitanja brojati kao upisana; broji se tek nakon uspjelog upisa.
func TestPalaSerijaSeNeBrojiKaoUpisana(t *testing.T) {
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "primjerica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	// uvoz BP16 stvara objekte u sektoru B, području 16
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Područje 16', 'VGI')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(database, "cvor-p")
	deps := Deps{
		Readings:      repository.NewReadingRepository(database, rec),
		Stations:      repository.NewStationRepository(database, rec),
		Structures:    repository.NewStructureRepository(database, rec),
		Log:           t.Logf,
		StvoriObjekte: true,
	}
	dir := t.TempDir()
	writeJSON(t, dir, "crpne_stanice", []map[string]any{{"id": 1, "naziv": "CS Primjerica", "status": "published"}})
	writeJSON(t, dir, "ustave", []map[string]any{})
	writeJSON(t, dir, "vodomjerna_letva", []map[string]any{})
	writeJSON(t, dir, "vodostaji_na_crpnim_stanicama", []map[string]any{
		{"id": 10, "crpna_stanica": 1, "datum": "2026-09-04", "vrijeme": "07:00:00", "vodostaj": 120, "napomena": "Očitao Pero Perić."},
		{"id": 11, "crpna_stanica": 1, "datum": "2026-09-05", "vrijeme": "07:00:00", "vodostaj": 125, "napomena": "Očitao Pero Perić."},
	})
	writeJSON(t, dir, "vodostaji_na_ustavama", []map[string]any{})
	writeJSON(t, dir, "ostali_vodostaji", []map[string]any{})

	// baza odbija drugo očitanje, pa serija pada kad je prvo već obrađeno
	if _, err := database.Exec(`CREATE TRIGGER odbij_ocitanja BEFORE INSERT ON readings WHEN NEW.level_cm = 125
		BEGIN SELECT RAISE(ABORT, 'upis odbijen'); END`); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(context.Background(), DirSource{Dir: dir}, deps)
	if err == nil {
		t.Fatal("pala serija mora vratiti grešku")
	}
	if rep.Fetched != 2 || rep.Inserted != 0 {
		t.Fatalf("očekivano 2 dohvaćena i 0 upisanih, dobiveno %+v", rep)
	}

	// probni prolaz samo broji, baza se ne dira
	probni := deps
	probni.DryRun = true
	if rep, err := Run(context.Background(), DirSource{Dir: dir}, probni); err != nil || rep.Inserted != 2 {
		t.Fatalf("probni prolaz: %+v, %v", rep, err)
	}
	// kad baza primi, upisano je oboje, a ponovni uvoz nema što upisati
	if _, err := database.Exec(`DROP TRIGGER odbij_ocitanja`); err != nil {
		t.Fatal(err)
	}
	if rep, err := Run(context.Background(), DirSource{Dir: dir}, deps); err != nil || rep.Inserted != 2 {
		t.Fatalf("uvoz: %+v, %v", rep, err)
	}
	if rep, err := Run(context.Background(), DirSource{Dir: dir}, deps); err != nil || rep.Inserted != 0 || rep.Skipped != 2 {
		t.Fatalf("ponovni uvoz: %+v, %v", rep, err)
	}
}
