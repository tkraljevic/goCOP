package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Ustava ima letvu s obje strane. Nizvodna se mora spremiti pri upisu i
// izmjeni i vratiti s imenom, jer je objekt ono što dvije letve spaja.
func TestUstavaCuvaNizvodnuLetvu(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'B', 'VGO', 'COP');
		INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Baranja', 'VGI');`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "cvor")
	postaje := NewStationRepository(baza, rec)
	ctx := context.Background()
	uzv := &models.Station{Code: "uzvodna", Name: "Ustava uzvodno"}
	niz := &models.Station{Code: "nizvodna", Name: "Ustava nizvodno"}
	for _, s := range []*models.Station{uzv, niz} {
		if err := postaje.CreateStation(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewStructureRepository(baza, rec)
	u := &models.Structure{Code: "ustava", Name: "Ustava", Kind: models.StructureKindSluice, SectorID: "B", AreaID: 16,
		StationID: uzv.ID.String(), StationDownID: niz.ID.String()}
	if err := repo.CreateStructure(ctx, u); err != nil {
		t.Fatal(err)
	}
	natrag, err := repo.GetStructure(ctx, u.ID)
	if err != nil || natrag == nil {
		t.Fatal(err)
	}
	if natrag.StationDownID != niz.ID.String() || natrag.StationDownName != "Ustava nizvodno" {
		t.Errorf("nizvodna letva %q (%q) nije sačuvana", natrag.StationDownID, natrag.StationDownName)
	}
	natrag.StationDownID = ""
	if err := repo.UpdateStructure(ctx, natrag); err != nil {
		t.Fatal(err)
	}
	if opet, _ := repo.GetStructure(ctx, u.ID); opet == nil || opet.StationDownID != "" || opet.StationID != uzv.ID.String() {
		t.Errorf("izmjena nije maknula nizvodnu ili je dirala uzvodnu: %+v", opet)
	}
}
