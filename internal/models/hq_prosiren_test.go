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

// Potencija nosi zbrojni član. Bez njega protok pri malom vodostaju pada
// prema nuli, a rijeka teče i tada — na Novom Virju je 1979. pri nuli
// centimetara tekla 139 m³/s, od čega 131 dolazi upravo iz pomaka.
//
// Da su koeficijenti na mjestu, vidi se po tome što se dva susjedna odsječka
// na granici spajaju: potencija odozdo i polinom odozgo moraju na 120 cm dati
// isti protok. Zamijene li se eksponent i pomak, ovo se razilazi.
func TestPotencijaIPolinomSeSpajajuNaGranici(t *testing.T) {
	// stvarna krivulja Novog Virja za 1979./1980.
	pot := HQOdsjecak{OdCm: 0, DoCm: 120, Oblik: OblikPotencija,
		P1: 71.088, P2: 2.463, P3: 0.4, P4: 131.3}
	pol := HQOdsjecak{OdCm: 120, DoCm: 200, Oblik: OblikPolinom,
		P1: 0, P2: 336.33, P3: -46.46}

	a, ok := pot.Protok(120)
	if !ok {
		t.Fatal("potencija ne daje protok na 120 cm")
	}
	b, ok := pol.Protok(120)
	if !ok {
		t.Fatal("polinom ne daje protok na 120 cm")
	}
	if razlika := a - b; razlika > 1 || razlika < -1 {
		t.Errorf("na granici se odsječci razilaze: potencija %.1f, polinom %.1f — %.1f m³/s razlike", a, b, razlika)
	}

	// Pri nuli centimetara pomak drži gotovo sav protok; bez njega bi ostalo
	// sedam i pol kubika umjesto sto trideset devet.
	q, _ := pot.Protok(0)
	if q < 135 || q > 143 {
		t.Errorf("pri 0 cm dobiveno %.1f m³/s, očekivano oko 139 — je li p4 izgubljen?", q)
	}
	bez := HQOdsjecak{OdCm: 0, DoCm: 120, Oblik: OblikPotencija,
		P1: 71.088, P2: 2.463, P3: 0.4}
	if q2, _ := bez.Protok(0); q2 > 20 {
		t.Errorf("bez pomaka očekivano ispod 20 m³/s, dobiveno %.1f — test ne mjeri ono što misli", q2)
	}
}
