package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func TestWatercourseService_Geometry(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "test_service_geo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()

	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}

	repo := repository.NewWatercourseRepository(baza, ledger.New(baza, "test-node"))
	svc := NewWatercourseService(repo)
	ctx := context.Background()

	adminPerms := &models.UserPermissions{IsGlobalAdmin: true}
	userPerms := &models.UserPermissions{IsGlobalAdmin: false}

	// Kreiraj rijeku Dunav
	if err := repo.CreateWatercourse(ctx, &models.Watercourse{
		Code:         "rijeka-dunav",
		OfficialName: "rijeka Dunav",
		Name:         "Dunav",
	}); err != nil {
		t.Fatalf("CreateWatercourse: %v", err)
	}

	// 1. Dunav: inicijalno dohvaća fallback geometriju iz paketa geometrija
	geomDunav, err := svc.GetWatercourseGeometry(ctx, "rijeka-dunav")
	if err != nil {
		t.Fatalf("GetWatercourseGeometry rijeka-dunav: %v", err)
	}
	if len(geomDunav) == 0 {
		t.Fatalf("Očekivao popunjenu geometriju za Dunav iz baze, dobio prazno")
	}

	// 2. Dozvola za izmjenu geometrije: običan korisnik ne može
	testGeo := `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[18.85,45.84],[18.95,45.53]]}}]}`
	if err := svc.SetWatercourseGeometry(ctx, userPerms, "rijeka-dunav", testGeo); err == nil {
		t.Errorf("Očekivao grešku ovlasti za korisnika bez globalAdmin ovlasti")
	}

	// 3. Admin može ažurirati geometriju
	if err := svc.SetWatercourseGeometry(ctx, adminPerms, "rijeka-dunav", testGeo); err != nil {
		t.Fatalf("SetWatercourseGeometry sa admin ovlastima: %v", err)
	}

	// 4. U bazi stoji upisana geometrija; čitanje je vraća sa stacionažom
	w, err := svc.GetWatercourse(ctx, "rijeka-dunav")
	if err != nil || w == nil {
		t.Fatalf("GetWatercourse after update: %v", err)
	}
	if w.Geometry != testGeo {
		t.Errorf("u bazi je %s, htio %s", w.Geometry, testGeo)
	}
	updatedGeom, err := svc.GetWatercourseGeometry(ctx, "rijeka-dunav")
	if err != nil || !strings.Contains(string(updatedGeom), "[18.95,45.53]") {
		t.Errorf("GetWatercourseGeometry nakon upisa: %v %s", err, updatedGeom)
	}
}
