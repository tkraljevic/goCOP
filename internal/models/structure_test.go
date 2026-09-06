package models

import "testing"

// Nasip i brana nemaju letvu, pa se na njima nema što očitati; Teren ih je
// nudio uz jedinu pravu letvu s gumbom za upis.
func TestNasipIBranaNemajuLetvu(t *testing.T) {
	for _, k := range []string{StructureKindEmbankment, StructureKindDam} {
		if (Structure{Kind: k}).TakesReadings() {
			t.Errorf("%s ne smije primati očitanja", k)
		}
	}
	for _, k := range []string{StructureKindPumpingStation, StructureKindSluice, StructureKindSiphon, StructureKindWeir} {
		if !(Structure{Kind: k}).TakesReadings() {
			t.Errorf("%s mora primati očitanja", k)
		}
	}
}
