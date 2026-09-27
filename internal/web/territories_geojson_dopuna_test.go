package web

import (
	"encoding/json"
	"testing"
)

// Osobni podaci ne stoje u geometriji u repozitoriju: poligon ih dobiva iz
// registra pri isporuci, a geometrija prolazi netaknuta.
func TestDopuniGeoJSON(t *testing.T) {
	raw := []byte(`{"type":"FeatureCollection","name":"z","features":[
		{"type":"Feature","properties":{"id":1,"name":"Osječko-baranjska"},"geometry":{"type":"Point","coordinates":[18.7,45.55]}},
		{"type":"Feature","properties":{"id":"2","name":"Druga"},"geometry":null}]}`)
	out := dopuniGeoJSON(raw, map[string]map[string]any{
		"1": {"prefect": "Župan", "email": ""},
		"2": {"prefect": "Drugi"},
	})
	var fc struct {
		Name     string `json:"name"`
		Features []struct {
			Properties map[string]any  `json:"properties"`
			Geometry   json.RawMessage `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(out, &fc); err != nil {
		t.Fatal(err)
	}
	if fc.Name != "z" || fc.Features[0].Properties["prefect"] != "Župan" || fc.Features[1].Properties["prefect"] != "Drugi" {
		t.Errorf("dopuna: %s", out)
	}
	if _, ima := fc.Features[0].Properties["email"]; ima {
		t.Error("prazna vrijednost iz registra ne smije ući")
	}
	if string(fc.Features[0].Geometry) != `{"type":"Point","coordinates":[18.7,45.55]}` {
		t.Errorf("geometrija promijenjena: %s", fc.Features[0].Geometry)
	}
	if string(dopuniGeoJSON([]byte("nije json"), map[string]map[string]any{"1": {"a": "b"}})) != "nije json" {
		t.Error("nečitljiva zbirka mora proći kakva jest")
	}
}
