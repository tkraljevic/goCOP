package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Greška baze pri čitanju registara uz dionicu vraća se. Prije je dionica
// stigla bez vrste objekta, pa bi brana u izvozu dionica izašla kao nasip.
// Objekt kojeg u registru nema i dalje nije greška: ostaje bez naziva.
func TestDionicaJavljaGreskuRegistra(t *testing.T) {
	database, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Međudržavne rijeke Drava i Dunav', 'VGI')`,
		`INSERT INTO structures (id, code, name, kind, sector_id, area_id, origin, created_at, updated_at)
			VALUES ('brana-dalj', 'brana-dalj', 'Brana Dalj', 'BRANA', 'B', 34, 'RUCNO', datetime('now'), datetime('now'))`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewSectionRepository(database, ledger.New(database, "test"))
	sec := &models.Section{Code: "B.34.9", AreaID: 34, SectorID: "B", Description: "probna",
		Parts: []models.SectionPart{{Seq: 1, Description: "r. Dunav, d.o.",
			Embankments: []models.PartEmbankment{{StructureID: "brana-dalj", Name: "brana Dalj"}}}}}
	if err := repo.SaveSection(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSectionByCode("B.34.9")
	if err != nil || got.Parts[0].Embankments[0].StructureKind != models.StructureKindDam {
		t.Fatalf("brana: %+v, %v", got, err)
	}

	// brana koje u registru nema ostaje bez vrste, bez greške
	if _, err := database.Exec(`UPDATE sections SET parts = replace(parts, 'brana-dalj', 'brana-nema') WHERE code = 'B.34.9'`); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetSectionByCode("B.34.9")
	if err != nil || got.Parts[0].Embankments[0].StructureKind != "" {
		t.Fatalf("brana koje nema: %+v, %v", got, err)
	}

	// registar objekata se ne da pročitati
	if _, err := database.Exec(`ALTER TABLE structures RENAME TO structures_kvar`); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetSectionByCode("B.34.9")
	if err == nil || got != nil || !strings.Contains(err.Error(), "B.34.9: nazivi iz registara") {
		t.Errorf("greška registra: %+v, %v", got, err)
	}
}
