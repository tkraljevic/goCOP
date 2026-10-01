package web

import (
	"strings"
	"testing"
)

func pf(v float64) *float64 { return &v }

// Rečenica rijeke: pada/raste/stabilna, jedna točka na kraju, novi zabilježeni
// vodostaj samo kad se razlikuje i zaokružen.
func TestRecenicaRijeke(t *testing.T) {
	naslovi := []string{"pet 2.10.", "sub 3.10.", "ned 4.10.", "pon 5.10.", "uto 6.10."}
	osijek := postajaSazetka{Ime: "Osijek", Sada: pf(-193), Naslovi: naslovi,
		Dani: []*float64{pf(-196), pf(-197), pf(-199), pf(-200), pf(-200.6)}, Min: &Krajnost{Cm: -201, Godina: 2026}}
	s := recenicaRijeke("Drava", []postajaSazetka{osijek})
	if !strings.HasPrefix(s, "Drava pada: Osijek s −193 na −201 cm do 6. 10.") || strings.Contains(s, "..") {
		t.Errorf("rečenica: %q", s)
	}
	if strings.Contains(s, "novi zabilježeni") {
		t.Errorf("−200,6 zaokruženo je −201, isto kao dosadašnji minimum: %q", s)
	}
	batina := postajaSazetka{Ime: "Batina", Sada: pf(-131), SadaQ: pf(539), Naslovi: naslovi,
		Dani: []*float64{pf(-143), pf(-145), pf(-148), pf(-149), pf(-148)}, DaniQ: []*float64{nil, nil, nil, nil, pf(454)},
		Min: &Krajnost{Cm: -147, Godina: 2026}}
	s = recenicaRijeke("Dunav", []postajaSazetka{batina})
	for _, zeli := range []string{"Dunav pada", "protok s 539 na 454 m³/s", "novi zabilježeni vodostaj: Batina (−149 cm 5. 10., dosad najniže −147 cm, 2026.)"} {
		if !strings.Contains(s, zeli) {
			t.Errorf("nema %q u %q", zeli, s)
		}
	}
	if s := recenicaRijeke("Mura", []postajaSazetka{{Ime: "Goričan", Sada: pf(73), Naslovi: naslovi, Dani: []*float64{pf(72)}}}); s != "Mura je stabilna." {
		t.Errorf("mirna Mura: %q", s)
	}
}
