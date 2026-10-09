package bp16

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Odluke (8. 10. 2026.), pitanje 21: uvoz iz BP16 prima isti raspon kao
// ručni upis. Tipfeler u staroj evidenciji (21 500 umjesto 215) ne ulazi,
// nego se bilježi za stranicu letve. Kao ni ručnim upisom, ne upisuje se ni
// stanje crpne stanice iz istog zapisa (dorada, Odluke (8. 10. 2026.),
// pitanje 21; prije je stanje ostajalo bez vodostaja).
func TestUvozBP16NeUpisujeVodostajIzvanRaspona(t *testing.T) {
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "primjerica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Područje 16', 'VGI')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(database, "cvor-p")
	readings := repository.NewReadingRepository(database, rec)
	deps := Deps{
		Readings:      readings,
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
		{"id": 10, "crpna_stanica": 1, "datum": "2026-09-04", "vrijeme": "07:00:00", "vodostaj": 215, "napomena": "Očitao Pero Perić."},
		{"id": 11, "crpna_stanica": 1, "datum": "2026-09-05", "vrijeme": "07:00:00", "vodostaj": 21500, "napomena": "Očitao Pero Perić."},
		{"id": 12, "crpna_stanica": 1, "datum": "2026-09-06", "vrijeme": "07:00:00", "vodostaj": 21600, "stanje_cs": "kvar"},
	})
	writeJSON(t, dir, "vodostaji_na_ustavama", []map[string]any{})
	writeJSON(t, dir, "ostali_vodostaji", []map[string]any{})

	rep, err := Run(context.Background(), DirSource{Dir: dir}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Fetched != 3 || rep.Inserted != 1 {
		t.Fatalf("pročitano %d, upisano %d; očekivano 3 i 1 (zapisi s tipfelerom otpadaju cijeli)", rep.Fetched, rep.Inserted)
	}
	var izvan, bezVodostaja int
	if err := database.QueryRow(`SELECT COUNT(*) FROM readings WHERE level_cm > 3000`).Scan(&izvan); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM readings WHERE level_cm IS NULL AND structure_state = ?`,
		models.StructureStateFault).Scan(&bezVodostaja); err != nil {
		t.Fatal(err)
	}
	if izvan != 0 || bezVodostaja != 0 {
		t.Errorf("izvan raspona upisano %d, stanje bez vodostaja %d", izvan, bezVodostaja)
	}
	var letva string
	if err := database.QueryRow(`SELECT 'structure:' || id FROM structures WHERE name = 'CS Primjerica'`).Scan(&letva); err != nil {
		t.Fatal(err)
	}
	s, err := readings.SazetakOdbacenih(context.Background(), letva)
	if err != nil || s == nil || s.Broj != 2 || s.Zadnja.Opis != "21600 cm" || s.IzvorLabel() != "evidencija VGI Baranja" {
		t.Errorf("sažetak: %+v, %v", s, err)
	}
}
