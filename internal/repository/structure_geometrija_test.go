package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

func bazaSPodrucjem(t *testing.T) (*sql.DB, *ledger.Recorder) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'B', 'VGO', 'COP');
		INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'Drava i Dunav', 'VGI');`); err != nil {
		t.Fatal(err)
	}
	return baza, ledger.New(baza, "cvor")
}

const crtaNasipa = `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"tip":"nasip"},"geometry":{"type":"LineString","coordinates":[[18.80,45.80],[18.81,45.79]]}},{"type":"Feature","properties":{"tip":"km","km":"0+000"},"geometry":{"type":"Point","coordinates":[18.80,45.80]}}]}`

// Ucrtani nasip mora preživjeti spremanje obrasca, koji geometriju ne nosi,
// i otići u knjigu da ga razmjena odnese drugim čvorovima.
func TestGeometrijaNasipaOstajeIPutuje(t *testing.T) {
	baza, rec := bazaSPodrucjem(t)
	repo := NewStructureRepository(baza, rec)
	ctx := context.Background()
	n := &models.Structure{Code: "nasip-proba", Name: "Nasip Proba", Kind: models.StructureKindEmbankment, SectorID: "B", AreaID: 34}
	if err := repo.CreateStructure(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetStructureGeometry(ctx, n.ID, crtaNasipa); err != nil {
		t.Fatal(err)
	}
	obrazac := &models.Structure{ID: n.ID, Code: n.Code, Name: "Nasip Proba (novo ime)", Kind: n.Kind, SectorID: "B", AreaID: 34}
	if err := repo.UpdateStructure(ctx, obrazac); err != nil {
		t.Fatal(err)
	}
	natrag, err := repo.GetStructure(ctx, n.ID)
	if err != nil || natrag == nil {
		t.Fatal(err)
	}
	if natrag.Geometry != crtaNasipa || natrag.Name != "Nasip Proba (novo ime)" {
		t.Fatalf("spremanje obrasca izgubilo je crtu ili ime: %q %q", natrag.Name, natrag.Geometry)
	}

	// verzija objekta nosi crtu; drugi čvor je primjenjuje na površinu
	payload, err := json.Marshal(natrag)
	if err != nil {
		t.Fatal(err)
	}
	drugaBaza, drugi := bazaSPodrucjem(t)
	tx, err := drugaBaza.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := primijeniObjekt(ctx, tx, ledger.Version{Entity: EntityStructures, EntityID: n.ID.String(), Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if na, err := NewStructureRepository(drugaBaza, drugi).GetStructure(ctx, n.ID); err != nil || na == nil || na.Geometry != crtaNasipa {
		t.Errorf("drugi čvor nema crtu nasipa: %+v", na)
	}

	if err := repo.SetStructureGeometry(ctx, uuid.New(), crtaNasipa); err == nil {
		t.Error("nepostojeći objekt primio je crtu")
	}

	// prazno briše crtu
	if err := repo.SetStructureGeometry(ctx, n.ID, ""); err != nil {
		t.Fatal(err)
	}
	if opet, err := repo.GetStructure(ctx, n.ID); err != nil || opet == nil || opet.HasGeometry() {
		t.Errorf("crta nije obrisana")
	}
}

// Baza bez stupca (čvor još nije nadograđen) ili zatvorena baza: upis javlja
// grešku i ništa ne bilježi u knjigu
func TestGeometrijaObjektaGreskeBaze(t *testing.T) {
	baza, rec := bazaSPodrucjem(t)
	repo := NewStructureRepository(baza, rec)
	ctx := context.Background()
	n := &models.Structure{Code: "nasip-proba", Name: "Nasip Proba", Kind: models.StructureKindEmbankment, SectorID: "B", AreaID: 34}
	if err := repo.CreateStructure(ctx, n); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`ALTER TABLE structures DROP COLUMN geometry`); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetStructureGeometry(ctx, n.ID, crtaNasipa); err == nil {
		t.Error("baza bez stupca geometrije primila je crtu")
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetStructureGeometry(ctx, n.ID, crtaNasipa); err == nil {
		t.Error("zatvorena baza primila je crtu")
	}
}
