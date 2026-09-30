package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Novu vodu u registar upisuje globalni administrator; tko smije urediti
// dionicu, smije iz retka upisati novi objekt, ali ne i vodu.
func TestNovaVodaIzRetkaTraziGlobalnogAdministratora(t *testing.T) {
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Međudržavne rijeke Drava i Dunav', 'VGI')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	svc := service.NewSectionService(repository.NewSectionRepository(database, ledger.New(database, "test")), service.NewSSEBroker())
	ctx := context.Background()
	admin := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AdminAreas: map[int]bool{}}

	sec := &models.Section{Code: "B.34.1", AreaID: 34, Parts: []models.SectionPart{{
		Description: "r. Dunav, d.o.",
		Objects: []models.PartObject{{Name: "ušće p. Karašice", NewRecord: &models.NewRegistryRecord{
			Registry: models.RegistryWatercourse, Kind: "potok", Name: "Karašica"}}},
	}}}
	if err := svc.SaveSection(ctx, admin, sec, true); err == nil {
		t.Fatal("nova voda upisana bez globalnog administratora")
	}
	sec.Parts[0].Objects[0] = models.PartObject{Name: "CS Gomboš, Q=0,25 m3/s", NewRecord: &models.NewRegistryRecord{
		Registry: models.RegistryStructure, Kind: models.StructureKindPumpingStation, Name: "CS Gomboš"}}
	if err := svc.SaveSection(ctx, admin, sec, true); err != nil {
		t.Fatalf("novi objekt iz retka: %v", err)
	}
	sec2 := &models.Section{Code: "B.34.2", AreaID: 34, Parts: []models.SectionPart{{
		Description: "r. Dunav, d.o.",
		Objects: []models.PartObject{{Name: "ušće p. Karašice", NewRecord: &models.NewRegistryRecord{
			Registry: models.RegistryWatercourse, Kind: "potok", Name: "Karašica"}}},
	}}}
	if err := svc.SaveSection(ctx, &models.UserPermissions{IsGlobalAdmin: true}, sec2, true); err != nil {
		t.Fatalf("globalni administrator upisuje vodu: %v", err)
	}
}
