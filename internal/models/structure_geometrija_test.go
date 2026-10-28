package models

import "testing"

func TestStructureHasGeometry(t *testing.T) {
	for geo, htio := range map[string]bool{"": false, "  \n": false, `{"type":"FeatureCollection","features":[]}`: true} {
		if got := (Structure{Geometry: geo}).HasGeometry(); got != htio {
			t.Errorf("HasGeometry(%q) = %v, htio %v", geo, got, htio)
		}
	}
}
