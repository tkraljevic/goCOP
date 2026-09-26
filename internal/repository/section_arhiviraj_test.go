package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Arhivirana dionica nestaje s površine zajedno s vezama na letve, a u knjizi
// ostaje arhivirana verzija, da brisanje stigne i na druge čvorove.
func TestArhivirajSekcijuMičeDionicuIVeze(t *testing.T) {
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
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'BP 16', 'VGI')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(database, "test")
	st := NewStationRepository(database, rec)
	letva := &models.Station{Code: "batina", Name: "Batina", Watercourse: "Dunav"}
	if err := st.CreateStation(context.Background(), letva); err != nil {
		t.Fatal(err)
	}
	repo := NewSectionRepository(database, rec)
	sec := &models.Section{Code: "B.16.1", AreaID: 16, SectorID: "B", Description: "probna",
		Parts: []models.SectionPart{{Seq: 1, Description: "probna", StationIDs: []string{letva.ID.String()}}}}
	if err := repo.SaveSection(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	var n int
	database.QueryRow(`SELECT count(*) FROM section_stations WHERE section_code = 'B.16.1'`).Scan(&n)
	if n != 1 {
		t.Fatalf("prije arhiviranja veza na letvu: %d", n)
	}

	if err := repo.ArhivirajSekciju(context.Background(), "B.16.1"); err != nil {
		t.Fatal(err)
	}
	if s, _ := repo.GetSectionByCode("B.16.1"); s != nil {
		t.Error("dionica je ostala na površini")
	}
	database.QueryRow(`SELECT count(*) FROM section_stations WHERE section_code = 'B.16.1'`).Scan(&n)
	if n != 0 {
		t.Errorf("ostalo veza na letvu: %d", n)
	}
	povijest, err := rec.History(context.Background(), EntitySections, "B.16.1")
	if err != nil || len(povijest) == 0 || !povijest[0].Archived {
		t.Errorf("u knjizi nema arhivirane verzije: %v %+v", err, povijest)
	}
	// dionica koje nema nije greška
	if err := repo.ArhivirajSekciju(context.Background(), "B.99.9"); err != nil {
		t.Errorf("nepostojeća dionica: %v", err)
	}
}
