package service_test

import (
	"context"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Ustava smije biti u sklopu crpne stanice (Ustava Zmajevac u CS Zmajevac);
// u sklopu drugog objekta, ili kad nije ustava, odbija se. Stanica tada zna
// svoje ustave, pa se zapornica upisuje u njezinu očitanju.
func TestUstavaUSklopuCrpneStanice(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ctx := context.Background()
	objekti := service.NewStructureService(repository.NewStructureRepository(o.db, o.rec))
	novi := func(naziv, vrsta, uSklopu string) *models.Structure {
		return &models.Structure{Name: naziv, Kind: vrsta, AreaID: 16, USklopuID: uSklopu}
	}
	cs := novi("CS Proba", models.StructureKindPumpingStation, "")
	nasip := novi("Nasip Proba", models.StructureKindEmbankment, "")
	for _, s := range []*models.Structure{cs, nasip} {
		if err := objekti.Create(ctx, o.admin, s); err != nil {
			t.Fatal(err)
		}
	}
	for ime, s := range map[string]struct {
		st     *models.Structure
		greska string
	}{
		"u sklopu nasipa":        {novi("Ustava Uz Nasip", models.StructureKindSluice, nasip.ID.String()), "samo crpne stanice"},
		"nepostojeća stanica":    {novi("Ustava Bez Stanice", models.StructureKindSluice, "nije-uuid"), "nije pronađena"},
		"crpna stanica u sklopu": {novi("CS U Sklopu", models.StructureKindPumpingStation, cs.ID.String()), "samo ustava"},
	} {
		if err := objekti.Create(ctx, o.admin, s.st); err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: %v", ime, err)
		}
	}
	ustava := novi("Ustava Proba", models.StructureKindSluice, cs.ID.String())
	if err := objekti.Create(ctx, o.admin, ustava); err != nil {
		t.Fatal(err)
	}
	if got, err := objekti.Get(ctx, ustava.ID); err != nil || got == nil || got.USklopuID != cs.ID.String() || got.ImaZapornicu() {
		t.Errorf("spremljena ustava u sklopu: %+v, %v", got, err)
	}
	if u, err := objekti.UstaveUSklopu(ctx, cs); err != nil || len(u) != 1 || u[0].ID != ustava.ID {
		t.Errorf("ustave u sklopu stanice: %+v, %v", u, err)
	}
	if u, err := objekti.UstaveUSklopu(ctx, nasip); err != nil || len(u) != 0 {
		t.Errorf("nasip ima ustave u sklopu: %+v, %v", u, err)
	}
	// stranica ustave pokazuje stanicu, a stanica ustavu; zapornica ide u
	// očitanje stanice
	if cs2, u := objekti.Sklop(ctx, ustava); cs2 == nil || cs2.ID != cs.ID || len(u) != 0 {
		t.Errorf("sklop ustave: %+v, %+v", cs2, u)
	}
	if cs2, u := objekti.Sklop(ctx, cs); cs2 != nil || len(u) != 1 {
		t.Errorf("sklop stanice: %+v, %+v", cs2, u)
	}
	if ima, na := objekti.ZapornicaObjekta(ctx, ustava); ima || na == nil || na.ID != cs.ID {
		t.Errorf("zapornica ustave u sklopu: %v, %+v", ima, na)
	}
	if ima, na := objekti.ZapornicaObjekta(ctx, cs); !ima || na != nil {
		t.Errorf("zapornica stanice s ustavom u sklopu: %v, %+v", ima, na)
	}
	if ima, _ := objekti.ZapornicaObjekta(ctx, nasip); ima {
		t.Error("nasip ima zapornicu")
	}
	samostalna := novi("Ustava Samostalna", models.StructureKindSluice, "")
	if ima, na := objekti.ZapornicaObjekta(ctx, samostalna); !ima || na != nil {
		t.Errorf("samostalna ustava: %v, %+v", ima, na)
	}
	// ustava se može vratiti u samostalnu
	ustava.USklopuID = ""
	if err := objekti.Update(ctx, o.admin, ustava); err != nil {
		t.Fatal(err)
	}
	if u, err := objekti.UstaveUSklopu(ctx, cs); err != nil || len(u) != 0 {
		t.Errorf("samostalna ustava ostala u sklopu: %+v, %v", u, err)
	}
}
