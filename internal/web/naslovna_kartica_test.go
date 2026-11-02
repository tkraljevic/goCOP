package web

import "testing"

// Kartica pokazuje najviše pet redaka, na mobitelu tri, a redak „Više”
// kaže koliko je skriveno i koliko je od toga hitno (Odluke (1. 11. 2026.))
func TestKarticaNaslovnePetITriRetka(t *testing.T) {
	redovi := func(n int, hitni ...int) []RedakKartice {
		out := make([]RedakKartice, n)
		for _, i := range hitni {
			out[i].Hitno = true
		}
		return out
	}
	for _, s := range []struct {
		opis              string
		k                 KarticaNaslovne
		vidljivih         int
		vise, viseMobitel string
	}{
		{"prazna", KarticaNaslovne{}, 0, "", ""},
		{"tri retka stanu svugdje", KarticaNaslovne{Redovi: redovi(3)}, 3, "", ""},
		{"pet redaka: na mobitelu dva skrivena", KarticaNaslovne{Redovi: redovi(5)}, 5, "", "Više: još 2"},
		{"dvanaest redaka", KarticaNaslovne{Redovi: redovi(12)}, 5, "Više: još 7", "Više: još 9"},
		{"hitni među skrivenima", KarticaNaslovne{Redovi: redovi(12, 1, 4, 9), HitnoNaziv: "iznad praga"}, 5,
			"Više: još 7, od toga 1 iznad praga", "Više: još 9, od toga 2 iznad praga"},
		{"hitno bez naziva se ne broji", KarticaNaslovne{Redovi: redovi(7, 6)}, 5, "Više: još 2", "Više: još 4"},
	} {
		k := s.k
		if got := len(k.Vidljivi()); got != s.vidljivih {
			t.Errorf("%s: vidljivih %d, želi %d", s.opis, got, s.vidljivih)
		}
		if got := k.Vise(); got != s.vise {
			t.Errorf("%s: Više %q, želi %q", s.opis, got, s.vise)
		}
		if got := k.ViseMobitel(); got != s.viseMobitel {
			t.Errorf("%s: Više na mobitelu %q, želi %q", s.opis, got, s.viseMobitel)
		}
	}
	k := KarticaNaslovne{Redovi: redovi(5)}
	if k.SamoSiroko(2) || !k.SamoSiroko(3) {
		t.Error("na mobitelu se ne skrivaju redovi iza trećeg")
	}
}
