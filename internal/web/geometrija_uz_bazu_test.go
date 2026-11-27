package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Postava čvor pokreće iz same mape podataka, pa granice sektora i branjenih
// područja moraju se naći uz bazu, a ne u data/geometrija u odnosu na radnu
// mapu. Bez mape uz bazu vrijedi stari put.
func TestGraniceSeTrazeUzBazu(t *testing.T) {
	podaci := t.TempDir()
	geo := filepath.Join(podaci, "geometrija")
	if err := os.MkdirAll(geo, 0o755); err != nil {
		t.Fatal(err)
	}
	granice := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"oznaka":"probno-podrucje"},"geometry":null}]}`
	if err := os.WriteFile(filepath.Join(geo, "branjena-podrucja.geojson"), []byte(granice), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &TerritoriesHandler{}
	if got := h.mapaGeometrije(); got != geometrijaVodaDir {
		t.Errorf("bez mape podataka: %q, očekivano %q", got, geometrijaVodaDir)
	}
	h.SetGeometrija(func() string { return "" })
	if got := h.mapaGeometrije(); got != geometrijaVodaDir {
		t.Errorf("prazan put do baze: %q, očekivano %q", got, geometrijaVodaDir)
	}
	bezGeometrije := filepath.Join(t.TempDir(), "gocop.db")
	h.SetGeometrija(func() string { return bezGeometrije })
	if got := h.mapaGeometrije(); got != geometrijaVodaDir {
		t.Errorf("mapa podataka bez geometrije: %q, očekivano %q", got, geometrijaVodaDir)
	}
	h.SetGeometrija(func() string { return filepath.Join(podaci, "gocop.db") })
	if got := h.mapaGeometrije(); got != geo {
		t.Errorf("mapa geometrije uz bazu: %q, očekivano %q", got, geo)
	}

	rr := httptest.NewRecorder()
	h.HandleGetBranjenaPodrucjaGeoJSON(rr, httptest.NewRequest(http.MethodGet, "/api/branjena-podrucja.geojson", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "probno-podrucje") {
		t.Errorf("granice područja uz bazu: %d %s", rr.Code, rr.Body.String())
	}
}

// Poslužitelj put do baze dobije tek nakon što složi rute, kao pri pravom
// pokretanju čvora. Granice se i tada moraju naći uz bazu: dok se put čitao
// pri slaganju ruta, uvijek je bio prazan i Karta je ostajala bez granica.
func TestGraniceUzBazuZadanuNakonRuta(t *testing.T) {
	podaci := t.TempDir()
	geo := filepath.Join(podaci, "geometrija")
	if err := os.MkdirAll(geo, 0o755); err != nil {
		t.Fatal(err)
	}
	granice := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"oznaka":"probno-podrucje"},"geometry":null}]}`
	if err := os.WriteFile(filepath.Join(geo, "branjena-podrucja.geojson"), []byte(granice), 0o644); err != nil {
		t.Fatal(err)
	}

	p := noviPosluziteljOcitanja(t)
	p.s.dbPath = filepath.Join(podaci, "gocop.db")
	w := p.posalji("uprava", http.MethodGet, "/territories/branjena-podrucja.geojson", nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "probno-podrucje") {
		t.Errorf("granice uz bazu zadanu nakon ruta: %d %s", w.Code, w.Body.String())
	}
}
