package db

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/models"
)

// Objekt nosi stacionažu dvaput: kao zapis iz Privitka ("rkm 1428+010") i kao
// broj po kojem se raspoređuje po nasipima. Klijent koji pošalje samo zapis ne
// smije broj izbrisati — upis ga izvodi natrag.
func TestUpisIzvodiStacionazuObjektaIzZapisa(t *testing.T) {
	database, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'BP 34', 'VGI Baranja')`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	sec := &models.Section{
		Code: "B.34.1", AreaID: 34, SectorID: "B", Description: "r. Dunav",
		Parts: []models.SectionPart{{
			Seq: 1,
			Objects: []models.PartObject{
				{Name: "ušće Šarkanjskog Dun.", StationingKind: "rkm", StationingText: "rkm 1428+010"},
				{Name: "CS Budžak", StationingKind: "nkm", StationingText: "km 2+700"},
				{Name: "bez stacionaže"},
			},
		}},
	}
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSection(context.Background(), tx, sec); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	o := sec.Parts[0].Objects
	if o[0].Stationing == nil || *o[0].Stationing != 1428.01 {
		t.Errorf("rkm 1428+010 nije izveden u broj: %v", o[0].Stationing)
	}
	if o[1].Stationing == nil || *o[1].Stationing != 2.7 {
		t.Errorf("km 2+700 nije izveden u broj: %v", o[1].Stationing)
	}
	if o[2].Stationing != nil {
		t.Error("objekt bez zapisa ne smije dobiti izmišljen broj")
	}
}
