package models

import "testing"

func TestStructureHasGeometry(t *testing.T) {
	for geo, htio := range map[string]bool{"": false, "  \n": false, `{"type":"FeatureCollection","features":[]}`: true} {
		if got := (Structure{Geometry: geo}).HasGeometry(); got != htio {
			t.Errorf("HasGeometry(%q) = %v, htio %v", geo, got, htio)
		}
	}
}

// Svaka vrsta je u točno jednom registru, a registri imaju različite adrese
func TestRegistriObjekata(t *testing.T) {
	putovi := map[string]bool{}
	for _, r := range StructureRegisters {
		if putovi[r.Path] || !r.Ima(r.Kind) {
			t.Errorf("registar %s: dvostruka adresa ili vrsta novog zapisa nije u registru", r.Path)
		}
		putovi[r.Path] = true
	}
	for _, k := range StructureKinds {
		n := 0
		for _, r := range StructureRegisters {
			if r.Ima(k) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("vrsta %s je u %d registara", k, n)
		}
	}
	if r, ok := StructureRegisterFor(StructureKindWeir); !ok || r.Path != "/ostali-objekti" {
		t.Errorf("pregrada nije u ostalim objektima: %+v", r)
	}
	if _, ok := StructureRegisterFor("NEPOZNATO"); ok {
		t.Error("nepoznata vrsta dobila je registar")
	}
}
