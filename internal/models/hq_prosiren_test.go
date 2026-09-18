package models

import (
	"math"
	"testing"
)

// Krivulja se smije produljiti do 20 cm preko krajeva odsječaka, uz oznaku
// da je vodostaj izvan umjerenog raspona; dalje od toga protoka nema.
func TestProtokProsirenIdeMaloPrekoRubaKrivulje(t *testing.T) {
	k := HQKrivulja{Odsjecci: []HQOdsjecak{
		{OdCm: -160, DoCm: 300, Oblik: OblikPolinom, P1: 0.1, P2: 512.754, P3: 1210.186},
		{OdCm: 300, DoCm: 560, Oblik: OblikPolinom, P1: 66.063, P2: 158.727, P3: 1678.604},
		{OdCm: 560, DoCm: 800, Oblik: OblikPolinom, P1: 218.145, P2: -1441.644, P3: 5871.3921},
	}}
	slucajevi := []struct {
		cm      int
		q       float64
		izvan   bool
		ok      bool
		opisano string
	}{
		{-160, 390, false, true, "donji rub, unutar"},
		{-163, 375, true, true, "3 cm ispod ruba"},
		{-180, 288, true, true, "20 cm ispod ruba, još ide"},
		{-181, 0, false, false, "21 cm ispod ruba, ne ide"},
		{810, 8507, true, true, "10 cm iznad vrha"},
		{821, 0, false, false, "21 cm iznad vrha, ne ide"},
	}
	for _, s := range slucajevi {
		q, izvan, ok := k.ProtokProsiren(s.cm)
		if ok != s.ok || izvan != s.izvan || (ok && math.Abs(q-s.q) > 1) {
			t.Errorf("%s (%d cm): q=%.0f izvan=%v ok=%v, očekivano q≈%.0f izvan=%v ok=%v", s.opisano, s.cm, q, izvan, ok, s.q, s.izvan, s.ok)
		}
	}
	// strogi Protok ostaje strog: pragovi i izvješća ne produljuju krivulju
	if _, ok := k.Protok(-163); ok {
		t.Error("Protok ne smije ići izvan raspona")
	}
}
