package service

import (
	"context"
	"path/filepath"
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
	testGeo := `{"type":"FeatureCollection","features":[]}`
	if err := svc.SetWatercourseGeometry(ctx, userPerms, "rijeka-dunav", testGeo); err == nil {
		t.Errorf("Očekivao grešku ovlasti za korisnika bez globalAdmin ovlasti")
	}

	// 3. Admin može ažurirati geometriju
	if err := svc.SetWatercourseGeometry(ctx, adminPerms, "rijeka-dunav", testGeo); err != nil {
		t.Fatalf("SetWatercourseGeometry sa admin ovlastima: %v", err)
	}

	// 4. Sada GetWatercourseGeometry vraća novu geometriju iz baze
	updatedGeom, err := svc.GetWatercourseGeometry(ctx, "rijeka-dunav")
	if err != nil {
		t.Fatalf("GetWatercourseGeometry after update: %v", err)
	}
	if string(updatedGeom) != testGeo {
		t.Errorf("GetWatercourseGeometry vratio %s, htio %s", string(updatedGeom), testGeo)
	}
}
