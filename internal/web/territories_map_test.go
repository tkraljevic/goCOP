package web

import (
	"context"
	"html/template"
	"io/fs"
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
	webassets "gocop/web"
)

func TestShowTerritoriesMapView(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "web_territories_map.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}

	rec := ledger.New(baza, "test-node")
	secRepo := repository.NewSectionRepository(baza, rec)
	sections := service.NewSectionService(secRepo, service.NewSSEBroker())
	terrRepo := repository.NewTerritoryRepository(baza, rec)
	terrService := service.NewTerritoryService(terrRepo, sections)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("territories.html")...)
	if err != nil {
		t.Fatalf("ParseFS territories.html error: %v", err)
	}

	h := NewTerritoriesHandler(terrService, tp)
	h.SetKarta(func() KartaPostavke {
		return KartaPostavke{
			Plocice:  "https://test.tiles/{z}/{x}/{y}.png",
			Zasluge:  "Test",
			NajviseZ: 18,
		}
	})

	user := &models.User{Username: "admin"}
	perms := &models.UserPermissions{IsGlobalAdmin: true}

	// 1. Popis (list mode) — ne smije sadržavati kartu
	reqList := httptest.NewRequest(http.MethodGet, "/territories?view=list", nil)
	reqList = reqList.WithContext(context.WithValue(reqList.Context(), contextKeyUser, user))
	reqList = reqList.WithContext(context.WithValue(reqList.Context(), contextKeyPerms, perms))
	recList := httptest.NewRecorder()
	h.ShowTerritories(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("list mode HTTP %d: %s", recList.Code, recList.Body.String())
	}
	bodyList := recList.Body.String()
	if strings.Contains(bodyList, "karta-teritorij") {
		t.Errorf("list mode ne smije sadržavati .karta-teritorij")
	}
	if !strings.Contains(bodyList, "prikaz-preklopnik") {
		t.Errorf("list mode mora sadržavati prikaz-preklopnik")
	}

	// 2. Karta (map mode) — mora sadržavati kartu, zupanije GeoJSON i 21 županiju
	reqMap := httptest.NewRequest(http.MethodGet, "/territories?view=map", nil)
	reqMap = reqMap.WithContext(context.WithValue(reqMap.Context(), contextKeyUser, user))
	reqMap = reqMap.WithContext(context.WithValue(reqMap.Context(), contextKeyPerms, perms))
	recMap := httptest.NewRecorder()
	h.ShowTerritories(recMap, reqMap)

	if recMap.Code != http.StatusOK {
		t.Fatalf("map mode HTTP %d: %s", recMap.Code, recMap.Body.String())
	}
	bodyMap := recMap.Body.String()
	if !strings.Contains(bodyMap, "karta-teritorij") {
		t.Errorf("map mode mora sadržavati .karta-teritorij")
	}
	if !strings.Contains(bodyMap, "karta-zupanije-podaci") {
		t.Errorf("map mode mora sadržavati element .karta-zupanije-podaci s GeoJSON-om")
	}
	if !strings.Contains(bodyMap, "Osječko-baranjska županija") {
		t.Errorf("map mode mora sadržavati Osječko-baranjska županija u podacima")
	}
	if !strings.Contains(bodyMap, "sloj-chk-zupanije") || !strings.Contains(bodyMap, "sloj-chk-gradovi") || !strings.Contains(bodyMap, "sloj-chk-opcine") || !strings.Contains(bodyMap, "sloj-chk-naselja") {
		t.Errorf("map mode mora sadržavati preklopnike za sve slojeve (županije, gradovi, općine, naselja)")
	}
	if !strings.Contains(bodyMap, "data-opcine-url=\"/territories/opcine.geojson\"") {
		t.Errorf("map mode mora referencirati /territories/opcine.geojson")
	}
	if !strings.Contains(bodyMap, "data-naselja-url=\"/territories/naselja.geojson?county_id=\"") {
		t.Errorf("map mode mora referencirati /territories/naselja.geojson?county_id=")
	}
	if !strings.Contains(bodyMap, "ploca-select-opcina") {
		t.Errorf("map mode mora sadržavati filter po općini/gradu (ploca-select-opcina)")
	}
	if !strings.Contains(bodyMap, "ploca-chk-naselja") {
		t.Errorf("map mode mora sadržavati preklopnik za naselja u desnom izborniku (ploca-chk-naselja)")
	}
	if !strings.Contains(bodyMap, "ploca-gumb-samo-naselja") {
		t.Errorf("map mode mora sadržavati gumb za filtriranje samo naselja (ploca-gumb-samo-naselja)")
	}

	// 3. Test HTTP GeoJSON rute
	reqZupGeo := httptest.NewRequest(http.MethodGet, "/territories/zupanije.geojson", nil)
	recZupGeo := httptest.NewRecorder()
	h.HandleGetCountiesGeoJSON(recZupGeo, reqZupGeo)
	if recZupGeo.Code != http.StatusOK {
		t.Fatalf("HandleGetCountiesGeoJSON HTTP %d", recZupGeo.Code)
	}
	if !strings.Contains(recZupGeo.Header().Get("Content-Type"), "application/geo+json") {
		t.Errorf("krivi Content-Type za zupanije.geojson: %s", recZupGeo.Header().Get("Content-Type"))
	}

	reqMuniGeo := httptest.NewRequest(http.MethodGet, "/territories/opcine.geojson", nil)
	recMuniGeo := httptest.NewRecorder()
	h.HandleGetMunicipalitiesGeoJSON(recMuniGeo, reqMuniGeo)
	if recMuniGeo.Code != http.StatusOK {
		t.Fatalf("HandleGetMunicipalitiesGeoJSON HTTP %d", recMuniGeo.Code)
	}
	if !strings.Contains(recMuniGeo.Header().Get("Content-Type"), "application/geo+json") {
		t.Errorf("krivi Content-Type za opcine.geojson: %s", recMuniGeo.Header().Get("Content-Type"))
	}
	if !strings.Contains(recMuniGeo.Body.String(), "gradovi-i-opcine-republike-hrvatske") {
		t.Errorf("opcine.geojson mora sadržavati naziv zbirke")
	}

	// 4. Test HTTP Naselja GeoJSON rute
	reqNaseljaGeo := httptest.NewRequest(http.MethodGet, "/territories/naselja.geojson?county_id=21", nil)
	recNaseljaGeo := httptest.NewRecorder()
	h.HandleGetSettlementsGeoJSON(recNaseljaGeo, reqNaseljaGeo)
	if recNaseljaGeo.Code != http.StatusOK {
		t.Fatalf("HandleGetSettlementsGeoJSON HTTP %d: %s", recNaseljaGeo.Code, recNaseljaGeo.Body.String())
	}
	if !strings.Contains(recNaseljaGeo.Header().Get("Content-Type"), "application/geo+json") {
		t.Errorf("krivi Content-Type za naselja.geojson: %s", recNaseljaGeo.Header().Get("Content-Type"))
	}
	if !strings.Contains(recNaseljaGeo.Body.String(), "Grad Zagreb") {
		t.Errorf("naselja za county_id=21 moraju sadržavati Grad Zagreb")
	}
}
