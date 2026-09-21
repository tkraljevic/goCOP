package geometrija

import (
	"encoding/json"
	"testing"
)

func TestUcitajEmbedded(t *testing.T) {
	cases := []string{"rijeka-dunav", "rijeka-drava", "rijeka-mura"}
	for _, code := range cases {
		t.Run(code, func(t *testing.T) {
			data, err := Ucitaj("", code)
			if err != nil {
				t.Fatalf("Ucitaj(%q) greška: %v", code, err)
			}
			if len(data) == 0 {
				t.Fatalf("Ucitaj(%q) vratio prazne podatke", code)
			}
			var parsed struct {
				Type     string `json:"type"`
				Features []any  `json:"features"`
			}
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("Unmarshal GeoJSON za %q: %v", code, err)
			}
			if parsed.Type != "FeatureCollection" {
				t.Errorf("Očekivan FeatureCollection, dobio %s", parsed.Type)
			}
			if len(parsed.Features) == 0 {
				t.Errorf("FeatureCollection za %q nema featurea", code)
			}
		})
	}
}

func TestUcitajNepostojeci(t *testing.T) {
	data, err := Ucitaj("", "nepostojeca-voda")
	if err != nil {
		t.Fatalf("Neočekivana greška za nepostojeću vodu: %v", err)
	}
	if data != nil {
		t.Errorf("Očekivao nil podatke za nepostojeću vodu, dobio %d bajtova", len(data))
	}
}

func TestUcitajZastitaPutanja(t *testing.T) {
	if _, err := Ucitaj("", "../../../etc/passwd"); err == nil {
		t.Errorf("Očekivana greška za path traversal")
	}
}
