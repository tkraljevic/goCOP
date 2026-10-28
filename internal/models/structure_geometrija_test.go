package models

import "testing"

func TestStructureHasGeometry(t *testing.T) {
	for geo, htio := range map[string]bool{"": false, "  \n": false, `{"type":"FeatureCollection","features":[]}`: true} {
		if got := (Structure{Geometry: geo}).HasGeometry(); got != htio {
			t.Errorf("HasGeometry(%q) = %v, htio %v", geo, got, htio)
		}
	}
}

// Svaka vrsta ima jedan registar s vlastitom adresom
func TestRegistriObjekata(t *testing.T) {
	putovi := map[string]bool{}
	for _, k := range StructureKinds {
		r, ok := StructureRegisterFor(k)
		if !ok || r.Path == "" || putovi[r.Path] {
			t.Errorf("vrsta %s nema svoj registar (%+v)", k, r)
		}
		putovi[r.Path] = true
	}
	if _, ok := StructureRegisterFor("NEPOZNATO"); ok {
		t.Error("nepoznata vrsta dobila je registar")
	}
}
