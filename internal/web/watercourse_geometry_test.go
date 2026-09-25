package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func TestWatercourseGeometryAPI(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "web_watercourse_geo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}

	rec := ledger.New(baza, "test-node")
	waterRepo := repository.NewWatercourseRepository(baza, rec)
	waterSvc := service.NewWatercourseService(waterRepo)
	secRepo := repository.NewSectionRepository(baza, rec)
	secSvc := service.NewSectionService(secRepo, service.NewSSEBroker())
	ctx := context.Background()

	// Kreiraj vodotok
	initialGeo := `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[18.8,45.8],[18.9,45.5]]}}]}`
	w := &models.Watercourse{
		Code:         "rijeka-dunav",
		OfficialName: "rijeka Dunav",
		Name:         "Dunav",
		Geometry:     initialGeo,
	}
	if err := waterRepo.CreateWatercourse(ctx, w); err != nil {
		t.Fatalf("CreateWatercourse: %v", err)
	}

	h := NewWatercoursesHandler(waterSvc, secSvc, nil)

	// 1. Običan korisnik bez globalAdmin ovlasti ne može ažurirati geometriju
	nonAdminPerms := &models.UserPermissions{IsGlobalAdmin: false}
	reqBody := `{"code":"rijeka-dunav","geojson":"{\"type\":\"FeatureCollection\",\"features\":[]}"}`
	req := httptest.NewRequest("POST", "/api/watercourses/geometry", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(ctx, contextKeyPerms, nonAdminPerms))
	recResp := httptest.NewRecorder()

	h.HandleUpdateWatercourseGeometryAPI(recResp, req)
	if recResp.Code != http.StatusBadRequest {
		t.Errorf("Očekivao status 400 za korisnika bez globalAdmin, dobio %d", recResp.Code)
	}

	// 2. GlobalAdmin uspješno sprema geometriju
	adminPerms := &models.UserPermissions{IsGlobalAdmin: true}
	updatedGeo := `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[18.85,45.84],[18.95,45.53]]}}]}`
	reqBody = `{"code":"rijeka-dunav","geojson":"` + strings.ReplaceAll(updatedGeo, `"`, `\"`) + `"}`
	req = httptest.NewRequest("POST", "/api/watercourses/geometry", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(ctx, contextKeyPerms, adminPerms))
	recResp = httptest.NewRecorder()

	h.HandleUpdateWatercourseGeometryAPI(recResp, req)
	if recResp.Code != http.StatusOK {
		t.Fatalf("HandleUpdateWatercourseGeometryAPI status = %d, want 200: %s", recResp.Code, recResp.Body.String())
	}

	// Provjeri u bazi
	savedWater, err := waterSvc.GetWatercourse(ctx, "rijeka-dunav")
	if err != nil {
		t.Fatalf("GetWatercourseGeometry: %v", err)
	}
	if savedWater.Geometry != updatedGeo {
		t.Errorf("Geometrija u bazi = %s, want %s", savedWater.Geometry, updatedGeo)
	}
	// Izvedeni prikaz smije dodati metapodatke, ali ne smije odbiti kratku
	// ručno uređenu liniju koja ne pokriva cijeli raspon ENC sidara.
	rendered, err := waterSvc.GetWatercourseGeometry(ctx, "rijeka-dunav")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "nije_kalibrirano") {
		t.Fatal("nepotpuna geometrija mora imati upozorenje")
	}

	// 3. Provjeri da HandleUpdateWatercourseAPI čuva postojeću geometriju
	formBody := strings.NewReader("code=rijeka-dunav&official_name=rijeka+Dunav+izmijenjeno&name=Dunav")
	reqUpdate := httptest.NewRequest("POST", "/api/watercourses/update", formBody)
	reqUpdate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqUpdate = reqUpdate.WithContext(context.WithValue(ctx, contextKeyPerms, adminPerms))
	recUpdate := httptest.NewRecorder()

	h.HandleUpdateWatercourseAPI(recUpdate, reqUpdate)
	if recUpdate.Code != http.StatusOK && recUpdate.Code != http.StatusSeeOther {
		t.Fatalf("HandleUpdateWatercourseAPI status = %d, want 200 or 303: %s", recUpdate.Code, recUpdate.Body.String())
	}

	wAfter, err := waterSvc.GetWatercourse(ctx, "rijeka-dunav")
	if err != nil {
		t.Fatalf("GetWatercourse: %v", err)
	}
	if wAfter.Geometry != updatedGeo {
		t.Errorf("Geometrija nakon HandleUpdateWatercourseAPI je prebrisana! Imamo: %s", wAfter.Geometry)
	}
}
