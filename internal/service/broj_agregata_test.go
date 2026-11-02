package service_test

import (
	"context"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Broj agregata crpne stanice sprema se, mijenja i vraća iz knjige promjena
// kad se objekt ponovno složi; broj izvan granica se odbija
func TestBrojAgregataCrpneStanice(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ctx := context.Background()
	objekti := service.NewStructureService(repository.NewStructureRepository(o.db, o.rec))
	cs := &models.Structure{Name: "CS Proba", Kind: models.StructureKindPumpingStation, AreaID: 16, CapacityText: "1,5", BrojAgregata: 4}
	if err := objekti.Create(ctx, o.admin, cs); err != nil {
		t.Fatal(err)
	}
	cs.BrojAgregata = 2
	if err := objekti.Update(ctx, o.admin, cs); err != nil {
		t.Fatal(err)
	}
	// površina se složi iznova iz knjige promjena, kao na drugom računalu
	if _, err := o.db.Exec(`DELETE FROM structures`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReplaySurface(ctx, o.db, o.rec); err != nil {
		t.Fatal(err)
	}
	if got, err := objekti.Get(ctx, cs.ID); err != nil || got == nil || got.BrojAgregata != 2 || got.BrojCrpki() != 2 {
		t.Errorf("broj agregata nakon slaganja iz knjige: %+v, %v", got, err)
	}
	cs.BrojAgregata = -1
	if err := objekti.Update(ctx, o.admin, cs); err == nil || !strings.Contains(err.Error(), "broj agregata") {
		t.Errorf("neispravan broj agregata je prihvaćen: %v", err)
	}
}
