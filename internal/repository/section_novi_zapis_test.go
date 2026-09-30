package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Redak objekta smije tražiti novi objekt ili novu vodu u registru: upisuju
// se pri spremanju dionice, u knjigu verzija, a redak ostaje vezan na njih.
// Objekt drugog područja veže se bez premještanja — CS Budžak ostaje u BP 16.
func TestNoviZapisIzRetkaObjekta(t *testing.T) {
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
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Mali sliv Baranja', 'VGI')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Međudržavne rijeke Drava i Dunav', 'VGI')`,
		`INSERT INTO structures (id, code, name, kind, sector_id, area_id, origin, created_at, updated_at)
			VALUES ('budzak', 'bp16-cs-budzak', 'CS Budžak', 'CRPNA_STANICA', 'B', 16, 'DIRECTUS_BP16', datetime('now'), datetime('now'))`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(database, "test")
	repo := NewSectionRepository(database, rec)
	sec := &models.Section{Code: "B.34.1", AreaID: 34, SectorID: "B", Description: "probna",
		Parts: []models.SectionPart{{Seq: 1, Description: "r. Dunav, d.o.", Objects: []models.PartObject{
			{Name: "CS Budžak,Q=0,40 m3/s", StructureID: "budzak"},
			{Name: "CS Gomboš, Q=0,25 m3/s", NewRecord: &models.NewRegistryRecord{Registry: models.RegistryStructure, Kind: models.StructureKindPumpingStation, Name: "CS Gomboš"}},
			{Name: "ušće p. Karašice", NewRecord: &models.NewRegistryRecord{Registry: models.RegistryWatercourse, Kind: "potok", Name: "Karašica"}},
		}}}}
	if err := repo.SaveSection(context.Background(), sec); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetSectionByCode("B.34.1")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	obj := got.Parts[0].Objects
	if obj[0].StructureID != "budzak" || obj[0].StructureName != "CS Budžak" {
		t.Errorf("objekt drugog područja: %+v", obj[0])
	}
	if obj[1].StructureID == "" || obj[1].NewRecord != nil || obj[1].StructureName != "CS Gomboš" || obj[1].Name != "CS Gomboš, Q=0,25 m3/s" {
		t.Errorf("novi objekt: %+v", obj[1])
	}
	if obj[2].WatercourseCode != "potok-karasica" || obj[2].NewRecord != nil || obj[2].WatercourseName != "potok Karašica" {
		t.Errorf("nova voda: %+v", obj[2])
	}
	var podrucje int
	database.QueryRow(`SELECT area_id FROM structures WHERE id = ?`, obj[1].StructureID).Scan(&podrucje)
	if podrucje != 34 {
		t.Errorf("novi objekt u području %d, a dionica je u 34", podrucje)
	}
	database.QueryRow(`SELECT area_id FROM structures WHERE id = 'budzak'`).Scan(&podrucje)
	if podrucje != 16 {
		t.Errorf("CS Budžak premješten u %d", podrucje)
	}
	var veza int
	database.QueryRow(`SELECT count(*) FROM section_structures WHERE section_code = 'B.34.1'`).Scan(&veza)
	if veza != 2 {
		t.Errorf("dionica vezana na %d objekata, a trebaju 2", veza)
	}
	if h, err := rec.History(context.Background(), EntityWatercourses, "potok-karasica"); err != nil || len(h) != 1 {
		t.Errorf("nova voda nije u knjizi verzija: %v %d", err, len(h))
	}
	if h, err := rec.History(context.Background(), EntityStructures, obj[1].StructureID); err != nil || len(h) != 1 {
		t.Errorf("novi objekt nije u knjizi verzija: %v %d", err, len(h))
	}

	// voda koja već postoji ne upisuje se drugi put — redak se veže na nju
	sec.Parts[0].Objects = append(got.Parts[0].Objects, models.PartObject{Name: "ušće Karašice",
		NewRecord: &models.NewRegistryRecord{Registry: models.RegistryWatercourse, Kind: "potok", Name: "Karašica"}})
	if err := repo.SaveSection(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetSectionByCode("B.34.1")
	if o := got.Parts[0].Objects[3]; o.WatercourseCode != "potok-karasica" {
		t.Errorf("postojeća voda: %+v", o)
	}
	if h, _ := rec.History(context.Background(), EntityWatercourses, "potok-karasica"); len(h) != 1 {
		t.Errorf("postojeća voda upisana drugi put: %d verzija", len(h))
	}
}
