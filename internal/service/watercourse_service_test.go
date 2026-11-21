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
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "test_service_geo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()

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

// Red vode iz obrasca: upisani red se sprema, kategorija iz Odluke bez reda je
// I. red, a uz drugi red ili nepostojeći red obrazac se odbija.
func TestRedVodeIzObrasca(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "red.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = baza.Close() }() // test
	svc := NewWatercourseService(repository.NewWatercourseRepository(baza, ledger.New(baza, "test-node")))
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}

	w := &models.Watercourse{OfficialName: "kanal Primjerak", Red: "III", Subcategory: models.SkupineVoda[3]}
	if err := svc.CreateWatercourse(ctx, admin, w); err != nil {
		t.Fatal(err)
	}
	spremljen, err := svc.GetWatercourse(ctx, w.Code)
	if err != nil || spremljen == nil || spremljen.Red != "III" || spremljen.Subcategory != models.SkupineVoda[3] {
		t.Fatalf("spremljeno: %+v, %v", spremljen, err)
	}
	prvi := &models.Watercourse{OfficialName: "rijeka Proba", Category: "DRUGE VEĆE VODE I KANALI"}
	if err := svc.CreateWatercourse(ctx, admin, prvi); err != nil || prvi.Red != "I" {
		t.Errorf("kategorija bez reda: %q, %v", prvi.Red, err)
	}
	for _, krivo := range []*models.Watercourse{
		{OfficialName: "potok Krivi", Red: "II", Category: "DRUGE VEĆE VODE I KANALI"},
		{OfficialName: "potok Peti", Red: "V"},
	} {
		if err := svc.CreateWatercourse(ctx, admin, krivo); err == nil {
			t.Errorf("%+v prošao", krivo)
		}
	}
}
