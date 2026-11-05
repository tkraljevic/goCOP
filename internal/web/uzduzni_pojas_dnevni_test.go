package web

import (
	"strings"
	"testing"
)

// Peti i šesti dan daje dnevni model: oko plave crte i tada stoji pojas od
// 70 %, ali samo ondje gdje letve imaju dnevni model, pa se lomi kao i crta.
// Do 96. sata pojas je jedan, preko svih letvi s granicama.
func TestPojasDnevnogModelaUDijelovima(t *testing.T) {
	letva := func(naziv string, rkm float64, cm map[int]float64, granice map[int][2]float64) LetvaProfila {
		return LetvaProfila{Letva: naziv, Naziv: naziv, Rkm: rkm, Cm: cm, Granice: granice}
	}
	puno := func(v float64) (map[int]float64, map[int][2]float64) {
		return map[int]float64{24: v, 120: v, 144: v}, map[int][2]float64{24: {v - 5, v + 5}, 120: {v - 20, v + 20}, 144: {v - 25, v + 25}}
	}
	c1, g1 := puno(100)
	c2, g2 := puno(110)
	c4, g4 := puno(90)
	c5, g5 := puno(95)
	// treća letva nema dnevni model (samo 24 h), pa se 5. dan lomi na njoj
	c3 := map[int]float64{24: 105}
	g3 := map[int][2]float64{24: {100, 110}}
	letve := []LetvaProfila{
		letva("A", 100, c1, g1), letva("B", 90, c2, g2), letva("C", 80, c3, g3),
		letva("D", 70, c4, g4), letva("E", 60, c5, g5),
	}
	xOf := func(rkm float64) float64 { return 1000 - rkm*5 }
	yOf := func(cm float64) float64 { return 500 - cm }
	po := map[int]ProfilCrta{}
	for _, c := range crteDosega(letve, xOf, yOf) {
		po[c.Doseg] = c
	}
	if p := po[24].Pojas; strings.Count(p, " Z") != 1 {
		t.Errorf("24 h: jedan pojas preko svih letvi, a dobiveno %q", p)
	}
	for _, d := range []int{120, 144} {
		p := po[d].Pojas
		if strings.Count(p, " Z") != 2 || !strings.HasPrefix(p, "M") {
			t.Errorf("%d h: dva dijela pojasa (A–B i D–E), a dobiveno %q", d, p)
		}
	}
	// dio s jednom letvom pojasa nema
	sama := []LetvaProfila{letva("A", 100, c1, g1), letva("C", 80, c3, g3)}
	if p := plohaGranicaUDijelovima(sama, 120, xOf, yOf); p != "" {
		t.Errorf("jedna letva s dnevnim modelom ne daje pojas: %q", p)
	}
	// ušće bez granica ne prekida pojas, akumulacija se preskače
	usce := letva("usce:D", 65, nil, nil)
	usce.Usce = true
	akum := letva("Akumulacija", 85, c1, g1)
	akum.Akumulacija = true
	s := []LetvaProfila{letva("A", 100, c1, g1), akum, letva("B", 90, c2, g2), usce, letva("D", 70, c4, g4)}
	if p := plohaGranicaUDijelovima(s, 120, xOf, yOf); strings.Count(p, " Z") != 1 {
		t.Errorf("ušće i akumulacija ne lome pojas: %q", p)
	}
}
