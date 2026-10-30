package service_test

import (
	"context"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Sektor objekta je uvijek sektor njegova branjenog područja. Zadani sektor
// koji to nije odbija se pri upisu i izmjeni, za svakoga; bez zadanog
// sektora objekt dobiva sektor područja.
func TestObjektDobivaSektorPodrucja(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ctx := context.Background()
	objekti := service.NewStructureService(repository.NewStructureRepository(o.db, o.rec))
	rukA := o.osoba(t, "ruka", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("A")})
	permA := o.ovlasti(t, rukA.ID)
	ruk16 := o.osoba(t, "ruk16", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	perm16 := o.ovlasti(t, ruk16.ID)
	objekt := func(naziv, sektor string, podrucje int) *models.Structure {
		return &models.Structure{Name: naziv, Kind: models.StructureKindPumpingStation, SectorID: sektor, AreaID: podrucje}
	}

	// uprava sektora A ne upisuje objekt u područje 16 sektora B
	if err := objekti.Create(ctx, permA, objekt("CS Tuđa", "A", 16)); err == nil {
		t.Error("uprava sektora A upisala je objekt u područje 16 sa sektorom A")
	}
	if err := objekti.Create(ctx, permA, objekt("CS Tuđa bez sektora", "", 16)); err == nil {
		t.Error("uprava sektora A upisala je objekt u područje 16")
	}
	if err := objekti.Create(ctx, o.admin, objekt("CS Krivi sektor", "A", 16)); err == nil {
		t.Error("objekt područja 16 upisan je sa sektorom A")
	}
	if err := objekti.Create(ctx, permA, objekt("CS Nepostojeće", "", 99)); err == nil {
		t.Error("objekt je upisan u nepostojeće područje")
	}
	// u svom sektoru i dalje upisuje, a sektor dolazi iz područja
	svoj := objekt("CS Pet", "", 5)
	if err := objekti.Create(ctx, permA, svoj); err != nil {
		t.Fatalf("uprava sektora A, objekt u području 5: %v", err)
	}
	if svoj.SectorID != "A" {
		t.Errorf("objekt područja 5 ima sektor %q", svoj.SectorID)
	}

	cs16 := objekt("CS Šesnaest", "B", 16)
	if err := objekti.Create(ctx, o.admin, cs16); err != nil {
		t.Fatal(err)
	}
	// rukovoditelj područja 16 ne prebacuje svoj objekt u drugi sektor
	premjesten := *cs16
	premjesten.SectorID = "A"
	if err := objekti.Update(ctx, perm16, &premjesten); err == nil {
		t.Error("rukovoditelj područja 16 promijenio je objektu sektor u A")
	}
	if st, err := objekti.Get(ctx, cs16.ID); err != nil || st == nil || st.SectorID != "B" {
		t.Errorf("objekt područja 16 nakon odbijene izmjene: %+v %v", st, err)
	}
	// a objekt svog područja i dalje uređuje
	izmjena := *cs16
	izmjena.Name, izmjena.SectorID = "CS Šesnaest nova", ""
	if err := objekti.Update(ctx, perm16, &izmjena); err != nil {
		t.Errorf("rukovoditelj područja 16, izmjena svog objekta: %v", err)
	}
	if st, err := objekti.Get(ctx, cs16.ID); err != nil || st == nil || st.Name != "CS Šesnaest nova" || st.SectorID != "B" {
		t.Errorf("objekt područja 16 nakon izmjene: %+v %v", st, err)
	}
}
