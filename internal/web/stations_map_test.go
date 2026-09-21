package web

import (
	"context"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

func TestStationMapThreshold(t *testing.T) {
	zero, positive := 0, 500
	for _, tc := range []struct {
		threshold models.Threshold
		want      string
	}{
		{models.Threshold{}, ""},
		{models.Threshold{Raw: "  "}, ""},
		{models.Threshold{Raw: "—"}, ""},
		{models.Threshold{Raw: "-"}, ""},
		{models.Threshold{Cm: &zero}, "+0 cm"},
		{models.Threshold{Cm: &positive}, "+500 cm"},
		{models.Threshold{Raw: "prema procjeni"}, "prema procjeni"},
	} {
		if got := stationMapThreshold(tc.threshold); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.threshold, got, tc.want)
		}
	}
}

func TestShowStationsMapView(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "web_stations_map.db"))
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
	waterRepo := repository.NewWatercourseRepository(baza, rec)
	waters := service.NewWatercourseService(waterRepo)

	ctx := context.Background()

	latBatina, lonBatina := 45.845, 18.847
	latBezdan, lonBezdan := 45.850, 18.867
	prepCm, regCm := 500, 600

	_ = stationRepo.CreateStation(ctx, &models.Station{
		Code:        "batina",
		Name:        "Batina",
		Watercourse: "Dunav",
		Stationing:  "rkm 1424.60",
		Latitude:    &latBatina,
		Longitude:   &lonBatina,
		Prep:        models.Threshold{Cm: &prepCm},
		Regular:     models.Threshold{Cm: &regCm},
	})
	_ = stationRepo.CreateStation(ctx, &models.Station{
		Code:        "bezdan",
		Name:        "Bezdan (Srbija)",
		Watercourse: "Dunav",
		Stationing:  "rkm 1425.50",
		Latitude:    &latBezdan,
		Longitude:   &lonBezdan,
	})
	_ = stationRepo.CreateStation(ctx, &models.Station{
		Code:        "nepoznata",
		Name:        "Postaja Bez Koordinata",
		Watercourse: "Kupa",
	})

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("stations.html")...)
	if err != nil {
		t.Fatal(err)
	}

	h := NewStationsHandler(stations, tp)
	h.SetPageTemplates(nil, nil, nil, nil, sections, waters)
	h.SetKarta(func() KartaPostavke {
		return KartaPostavke{
			Plocice:  "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
			Zasluge:  "© OpenStreetMap",
			NajviseZ: 18,
		}
	})

	// 1. Zadani prikaz: popis (list view)
	req := httptest.NewRequest("GET", "/stations", nil)
	w := httptest.NewRecorder()
	h.ShowStations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "prikaz-preklopnik") {
		t.Error("nedostaje prikaz-preklopnik")
	}
	if !strings.Contains(body, "view=map") {
		t.Error("nedostaje poveznica na kartu (view=map)")
	}
	if strings.Contains(body, "karta-sve-postaje") {
		t.Error("karta ne bi trebala biti prikazana u popisu")
	}

	// 2. Prikaz karte: view=map
	reqMap := httptest.NewRequest("GET", "/stations?view=map", nil)
	wMap := httptest.NewRecorder()
	h.ShowStations(wMap, reqMap)

	if wMap.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", wMap.Code)
	}
	bodyMap := wMap.Body.String()
	if !strings.Contains(bodyMap, "karta-sve-postaje") {
		t.Error("karta-sve-postaje mora biti prikazana kad je view=map")
	}
	if !strings.Contains(bodyMap, `name="view" value="map"`) {
		t.Error("filter forma mora sadržavati skriveno polje name=view value=map")
	}
	if !strings.Contains(bodyMap, "karta-geometrija-podaci") {
		t.Error("nedostaje script tag karta-geometrija-podaci")
	}
	if !strings.Contains(bodyMap, "karta-postaje-podaci") {
		t.Error("nedostaje script tag karta-postaje-podaci")
	}
	if !strings.Contains(bodyMap, "Batina") || !strings.Contains(bodyMap, "Bezdan") {
		t.Error("podaci o postajama moraju sadržavati Batinu i Bezdan")
	}
	if strings.Contains(bodyMap, "Postaja Bez Koordinata") {
		t.Error("postaja bez koordinata ne smije biti u podacima karte")
	}

	// 3. Provjera filtriranja na karti (npr. samo Srbija)
	reqFilter := httptest.NewRequest("GET", "/stations?view=map&country=Srbija", nil)
	wFilter := httptest.NewRecorder()
	h.ShowStations(wFilter, reqFilter)

	if wFilter.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", wFilter.Code)
	}
	bodyFilter := wFilter.Body.String()
	if !strings.Contains(bodyFilter, "Bezdan") {
		t.Error("filtrirana karta mora sadržavati Bezdan")
	}

	// Provjera JSON strukture postaja
	idxStart := strings.Index(bodyFilter, `class="karta-postaje-podaci">`)
	if idxStart == -1 {
		t.Fatal("nije pronađen tag karta-postaje-podaci")
	}
	idxEnd := strings.Index(bodyFilter[idxStart:], `</script>`)
	jsonStr := bodyFilter[idxStart+len(`class="karta-postaje-podaci">`) : idxStart+idxEnd]

	var mapItems []StationMapItem
	if err := json.Unmarshal([]byte(jsonStr), &mapItems); err != nil {
		t.Fatalf("neispravan JSON postaja na karti: %v", err)
	}
	if len(mapItems) != 1 || mapItems[0].Name != "Bezdan (Srbija)" {
		t.Fatalf("očekivana samo 1 postaja (Bezdan), dobiveno: %+v", mapItems)
	}
	if mapItems[0].Prep != "" || mapItems[0].Regular != "" || mapItems[0].Emergency != "" || mapItems[0].State != "" {
		t.Fatal("nedefinirani pragovi ne smiju biti prikazani na karti")
	}

	// Bez arhive krivulja operativna očitanja i dalje moraju raditi.
	h.arhiva = func() *repository.ArhivaRepository {
		return nil
	}
	rr := repository.NewReadingRepository(baza, rec)
	h.SetReadingService(service.NewReadingService(rr, stationRepo, nil, sections, nil))
	stationID := mapItems[0].ID
	check := func(level, when string, flow ...string) {
		t.Helper()
		w := httptest.NewRecorder()
		h.ShowStations(w, reqFilter)
		body := w.Body.String()
		marker := `class="karta-postaje-podaci">`
		start := strings.Index(body, marker)
		if start < 0 {
			t.Fatal("nedostaju podaci karte")
		}
		payload := body[start+len(marker):]
		var items []StationMapItem
		if err := json.Unmarshal([]byte(payload[:strings.Index(payload, "</script>")]), &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].LatestLevel != level || items[0].LatestTime != when {
			t.Fatalf("očekivano %q / %q, dobiveno %+v", level, when, items)
		}
		wantFlow := ""
		if len(flow) > 0 {
			wantFlow = flow[0]
		}
		if items[0].LatestFlow != wantFlow {
			t.Fatalf("protok = %q, očekivano %q", items[0].LatestFlow, wantFlow)
		}
	}
	check("", "") // bez očitanja nema arhivskog nadomjestka
	for _, row := range []struct{ hour, level int }{{12, 42}, {10, 36}} {
		rd := models.Reading{StationID: stationID, MeasuredAt: time.Date(2026, 9, 21, row.hour, 0, 0, 0, time.UTC), LevelCm: &row.level, Source: models.ReadingSourceManual}
		if err := rr.Create(ctx, &rd); err != nil {
			t.Fatal(err)
		}
	}
	check("+42 cm", "21.09. 14:00") // vrijeme mjerenja, ne redoslijed unosa; ljetno vrijeme
	if err := rr.Create(ctx, &models.Reading{StationID: stationID, MeasuredAt: time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC), Note: "Nije očitano", Source: models.ReadingSourceManual}); err != nil {
		t.Fatal(err)
	}
	check("", "") // zadnje očitanje bez vodostaja ne predstavlja stari broj kao aktualan
	for i, flow := range []float64{123.45, 0} {
		rd := models.Reading{StationID: stationID, MeasuredAt: time.Date(2026, 9, 22+i, 12, 0, 0, 0, time.UTC), FlowM3s: &flow, Source: models.ReadingSourceManual}
		if err := rr.Create(ctx, &rd); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			check("", "22.09. 14:00", "123,45 m³/s")
		} else {
			check("", "23.09. 14:00", "0 m³/s")
		}
	}
}
