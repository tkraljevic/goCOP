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

func TestShowStationsCountryFilter(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "web_stations.db"))
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
	stationRepo := repository.NewStationRepository(baza, rec)
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())
	ctx := context.Background()

	_ = stationRepo.CreateStation(ctx, &models.Station{Code: "batina", Name: "Batina", Watercourse: "Dunav"})
	_ = stationRepo.CreateStation(ctx, &models.Station{Code: "bezdan", Name: "Bezdan (Srbija)", Watercourse: "Dunav"})
	_ = stationRepo.CreateStation(ctx, &models.Station{Code: "baja", Name: "Baja (Mađarska)", Watercourse: "Dunav"})

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("stations.html")...)
	if err != nil {
		t.Fatal(err)
	}

	h := NewStationsHandler(stations, tp)

	// 1. Bez filtra: vide se sve postaje i dropdown s državama
	req := httptest.NewRequest("GET", "/stations", nil)
	w := httptest.NewRecorder()
	h.ShowStations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `name="country"`) {
		t.Error("nedostaje filter za državu (name=country)")
	}
	if !strings.Contains(body, "Hrvatska") || !strings.Contains(body, "Srbija") || !strings.Contains(body, "Mađarska") {
		t.Error("dropdown država ne sadrži očekivane države")
	}
	if !strings.Contains(body, "Batina") || !strings.Contains(body, "Bezdan") || !strings.Contains(body, "Baja") {
		t.Error("popis ne sadrži sve postaje bez filtra")
	}

	// 2. Filtar za Srbiju: samo Bezdan
	reqSRB := httptest.NewRequest("GET", "/stations?country=Srbija", nil)
	wSRB := httptest.NewRecorder()
	h.ShowStations(wSRB, reqSRB)

	if wSRB.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", wSRB.Code)
	}
	bodySRB := wSRB.Body.String()
	if !strings.Contains(bodySRB, "Bezdan") {
		t.Error("Srbija filtar mora prikazati Bezdan")
	}
	if strings.Contains(bodySRB, "<h3 class=\"reg-card-title\">Batina</h3>") || strings.Contains(bodySRB, "Baja") {
		t.Error("Srbija filtar ne smije prikazati Batina ili Baja")
	}

	// 3. Filtar za Hrvatsku: samo Batina
	reqHR := httptest.NewRequest("GET", "/stations?country=Hrvatska", nil)
	wHR := httptest.NewRecorder()
	h.ShowStations(wHR, reqHR)

	if wHR.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", wHR.Code)
	}
	bodyHR := wHR.Body.String()
	if !strings.Contains(bodyHR, "<h3 class=\"reg-card-title\">Batina</h3>") {
		t.Error("Hrvatska filtar mora prikazati Batina")
	}
	if strings.Contains(bodyHR, "Bezdan") || strings.Contains(bodyHR, "Baja") {
		t.Error("Hrvatska filtar ne smije prikazati strane postaje")
	}

	// 4. API /api/stations?country=Srbija
	reqAPI := httptest.NewRequest("GET", "/api/stations?country=Srbija", nil)
	wAPI := httptest.NewRecorder()
	h.HandleListStationsAPI(wAPI, reqAPI)
	if wAPI.Code != http.StatusOK {
		t.Fatalf("API status = %d, want 200", wAPI.Code)
	}
	bodyAPI := wAPI.Body.String()
	if !strings.Contains(bodyAPI, "bezdan") || strings.Contains(bodyAPI, "batina") {
		t.Errorf("API country filter greška: %s", bodyAPI)
	}
}
