package models

import (
	"testing"

	"github.com/google/uuid"
)

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

// Čep, propust i prag su u Ostalim objektima, imaju svoj naziv i bez letve su
func TestNoveVrsteObjekata(t *testing.T) {
	for kind, naziv := range map[string]string{StructureKindPlug: "čep", StructureKindCulvert: "propust", StructureKindSill: "prag"} {
		if StructureKindLabel(kind) != naziv {
			t.Errorf("%s: naziv %q", kind, StructureKindLabel(kind))
		}
		if r, ok := StructureRegisterFor(kind); !ok || r.Path != "/ostali-objekti" {
			t.Errorf("%s nije u ostalim objektima", kind)
		}
		if (Structure{Kind: kind}).TakesReadings() {
			t.Errorf("%s nudi očitanje", kind)
		}
	}
}

func TestAdresaObjektaURegistru(t *testing.T) {
	s := Structure{ID: uuid.MustParse("11111111-2222-3333-4444-555555555555"), Kind: StructureKindWeir}
	if s.Path() != "/ostali-objekti/11111111-2222-3333-4444-555555555555" {
		t.Errorf("pregrada: %s", s.Path())
	}
	s.Kind = "NEPOZNATO"
	if s.Path() != "/structures/11111111-2222-3333-4444-555555555555" {
		t.Errorf("nepoznata vrsta: %s", s.Path())
	}
}
