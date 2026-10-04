package ugovor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/importer/xlsx"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Predložak ugovora mora ostati izmišljen: stavke nose oznake primjera, a
// radna knjiga napomenu da nije ugovor ni troškovnik Hrvatskih voda.
func TestPredlozakUgovoraJeIzmisljen(t *testing.T) {
	wb, err := xlsx.Open(filepath.Join("..", "..", "..", "docs", "predlosci", "odrzavanje", "ugovor-a02.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(wb)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range append(c.Items, c.Catalogue...) {
		if !strings.HasPrefix(it.Number, "PR-") || !strings.Contains(it.Description, "izmišljeno") {
			t.Errorf("stavka %q %q nije označena kao izmišljeni primjer", it.Number, it.Description)
		}
	}
	if napomena := wb.Sheet("PPI_POSTAVKE").Cell(2, 1); !strings.Contains(napomena, "Izmišljeni primjer") {
		t.Errorf("PPI_POSTAVKE nema napomenu da je radna knjiga izmišljena: %q", napomena)
	}
}

// Predložak ugovora u docs/predlosci/odrzavanje čita se na bazi napunjenoj iz
// predložaka prvog pokretanja: obje lokacije moraju pasti na vode iz registra.
func TestPredlozakUgovora(t *testing.T) {
	predlosci := filepath.Join("..", "..", "..", "docs", "predlosci")
	staraMapa, stariImenik := db.DataDir, db.ImenikPath
	t.Cleanup(func() { db.DataDir, db.ImenikPath = staraMapa, stariImenik })
	db.DataDir = filepath.Join(predlosci, "prvo-pokretanje")
	db.ImenikPath = filepath.Join(db.DataDir, "imenik.json")

	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "ugovor.db"))
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
	areas, err := repository.NewUserRepository(baza, rec).ListAreas("")
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{
		Waters:      repository.NewWatercourseRepository(baza, rec),
		Structures:  repository.NewStructureRepository(baza, rec),
		Maintenance: repository.NewMaintenanceRepository(baza, rec),
		Areas:       areas,
	}

	rep, err := Run(context.Background(), Options{Path: filepath.Join(predlosci, "odrzavanje", "ugovor-a02.xlsx"), DryRun: true, Deps: deps})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Area != 1 || len(rep.Locations) != 2 || rep.Blocks != 2 || rep.ItemsTotal != 3 {
		t.Fatalf("područje %d, lokacija %d, blokova %d, stavki %d", rep.Area, len(rep.Locations), rep.Blocks, rep.ItemsTotal)
	}
	zeli := map[string]struct {
		sifra, red, vrsta string
	}{
		"Rijeka Primjerica": {"rijeka-primjerica", models.WaterOrderFirst, models.MaintenanceKindWatercourse},
		"Kanal Probni":      {"kanal-probni", models.WaterOrderSecond, models.MaintenanceKindDrainage},
	}
	for _, m := range rep.Locations {
		z, ok := zeli[m.Location.Name]
		if !ok {
			t.Errorf("neočekivana lokacija %q", m.Location.Name)
			continue
		}
		if m.Status != StatusExisting || m.Code != z.sifra {
			t.Errorf("%s: %s %q, očekivano postojeća %q", m.Location.Name, m.Status, m.Code, z.sifra)
		}
		if m.Location.Order != z.red || m.Location.Kind != z.vrsta {
			t.Errorf("%s: razvrstano %q/%q", m.Location.Name, m.Location.Order, m.Location.Kind)
		}
	}
}
