package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Novi čvor prima registar u paketima po 5000 verzija, redom nastanka: naselje
// zna stići prije svoje općine. Ono što ne prođe mora proći čim oslonac
// stigne — u istom paketu ili u sljedećem.
func TestPovrsinaCekaOslonac(t *testing.T) {
	ctx := context.Background()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "primatelj")

	verzija := func(entity string, id int, payload any, vid string) ledger.Version {
		v := ledger.Version{VersionID: vid, Entity: entity, EntityID: strconv.Itoa(id), NodeID: "ured", SchemaVersion: ledger.SchemaVersion}
		v.Payload, _ = json.Marshal(payload)
		return v
	}
	zupanija := verzija(EntityCounties, 901, models.County{ID: 901, Code: "PRB", Name: "Probna"}, "01a00000-0000-7000-8000-000000000001")
	naselje := verzija(EntitySettlements, 9001, models.Settlement{ID: 9001, MunicipalityID: 801, CountyID: 901, Name: "Probno Selo"}, "01a00000-0000-7000-8000-000000000002")
	opcina := verzija(EntityMunicipalities, 801, models.Municipality{ID: 801, CountyID: 901, Name: "Probna Općina", Type: "OPĆINA"}, "01a00000-0000-7000-8000-000000000003")
	broj := func() int {
		var n int
		baza.QueryRow(`SELECT count(*) FROM settlements WHERE id = 9001`).Scan(&n)
		return n
	}

	// isti paket: naselje je starije od općine, a ipak mora proći
	prvi := []ledger.Version{zupanija, naselje, opcina}
	if _, err := rec.Apply(ctx, prvi); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, baza, rec, prvi); err != nil {
		t.Fatalf("isti paket: %v", err)
	}
	if broj() != 1 {
		t.Fatal("naselje nije primijenjeno iako je općina stigla u istom paketu")
	}

	// sljedeći paket: naselje stiže samo, općina tek poslije
	naselje2 := verzija(EntitySettlements, 9002, models.Settlement{ID: 9002, MunicipalityID: 802, CountyID: 901, Name: "Drugo Selo"}, "01a00000-0000-7000-8000-000000000004")
	opcina2 := verzija(EntityMunicipalities, 802, models.Municipality{ID: 802, CountyID: 901, Name: "Druga Općina", Type: "OPĆINA"}, "01a00000-0000-7000-8000-000000000005")
	rec.Apply(ctx, []ledger.Version{naselje2})
	if err := ApplyVersions(ctx, baza, rec, []ledger.Version{naselje2}); err == nil {
		t.Fatal("naselje bez općine prošlo je strani ključ")
	}
	rec.Apply(ctx, []ledger.Version{opcina2})
	if err := ApplyVersions(ctx, baza, rec, []ledger.Version{opcina2}); err != nil {
		t.Fatalf("sljedeći paket: %v", err)
	}
	var n int
	baza.QueryRow(`SELECT count(*) FROM settlements WHERE id = 9002`).Scan(&n)
	if n != 1 {
		t.Error("naselje nije primijenjeno kad je njegova općina stigla sljedećom razmjenom")
	}
}
