package service

import (
	"os"
	"path/filepath"
	"testing"
)

// Predložak toka iz docs/predlosci/geometrija prolazi provjeru kojom karta
// vodotoka prima GeoJSON, i zbirka se upisuje kakva je stigla.
func TestPredlozakToka(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "predlosci", "geometrija", "tok-vodotoka.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	geo, err := PripremiGeometrijuVode(string(b))
	if err != nil {
		t.Fatalf("predložak toka odbijen: %v", err)
	}
	if geo != string(b) {
		t.Error("zbirka značajki mora se upisati nepromijenjena")
	}
	if NapomenaToka(geo) == "" {
		t.Error("napomena toka iz svojstva stacionaza_napomena nije pročitana")
	}
}
