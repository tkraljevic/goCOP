package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

func TestWatercourseRepository_Geometry(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "test_watercourses.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()

	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}

	rec := ledger.New(baza, "test-node")
	repo := NewWatercourseRepository(baza, rec)
	ctx := context.Background()

	// 1. CreateWatercourse s geometrijom
	w := &models.Watercourse{
		Code:         "rijeka-test",
		OfficialName: "rijeka Test",
		Name:         "Test",
		Kind:         "rijeka",
		Geometry:     `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"tip":"vodotok"}}]}`,
	}
	if err := repo.CreateWatercourse(ctx, w); err != nil {
		t.Fatalf("CreateWatercourse: %v", err)
	}

	// 2. GetWatercourse čita geometriju
	read, err := repo.GetWatercourse(ctx, "rijeka-test")
	if err != nil {
		t.Fatalf("GetWatercourse: %v", err)
	}
	if read == nil {
		t.Fatal("GetWatercourse vratio nil")
	}
	if read.Geometry != w.Geometry {
		t.Errorf("Geometrija se ne podudara: dobio %q, htio %q", read.Geometry, w.Geometry)
	}
	if !read.HasGeometry() {
		t.Errorf("HasGeometry() vratio false")
	}

	// 3. UpdateWatercourse mijenja geometriju i bilježi u ledger
	w.Geometry = `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"tip":"rkm","rkm":10}}]}`
	if err := repo.UpdateWatercourse(ctx, w); err != nil {
		t.Fatalf("UpdateWatercourse: %v", err)
	}

	updated, err := repo.GetWatercourse(ctx, "rijeka-test")
	if err != nil {
		t.Fatalf("GetWatercourse after update: %v", err)
	}
	if updated.Geometry != w.Geometry {
		t.Errorf("Ažurirana geometrija se ne podudara: dobio %q", updated.Geometry)
	}

	// 4. Provjeri da je promjena zapisana u ledger verzijama
	var payload string
	err = baza.QueryRowContext(ctx,
		`SELECT payload FROM record_versions WHERE entity = 'watercourses' AND entity_id = 'rijeka-test' ORDER BY version_id DESC LIMIT 1`,
	).Scan(&payload)
	if err != nil {
		t.Fatalf("ledger zapis nije pronađen: %v", err)
	}
	if payload == "" {
		t.Errorf("ledger payload je prazan")
	}
}
