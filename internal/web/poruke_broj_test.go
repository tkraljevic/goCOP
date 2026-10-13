package web

import "testing"

// Poruka nakon upisa ispravaka i očitanja iz tablice pisala je „21 ispravaka”,
// „0 ispravka” i „21 očitanja”: oblik se birao samo za 1 i za manje od pet.
func TestPorukeUzBrojIspravakaIOcitanja(t *testing.T) {
	for _, s := range []struct {
		n                   int
		ispravaka, ocitanja string
	}{
		{0, "0 ispravaka", "0 očitanja"},
		{1, "1 ispravak", "1 očitanje"},
		{2, "2 ispravka", "2 očitanja"},
		{5, "5 ispravaka", "5 očitanja"},
		{12, "12 ispravaka", "12 očitanja"},
		{21, "21 ispravak", "21 očitanje"},
		{22, "22 ispravka", "22 očitanja"},
		{111, "111 ispravaka", "111 očitanja"},
	} {
		if got := ispravaka(s.n); got != s.ispravaka {
			t.Errorf("ispravaka(%d) = %q, očekivano %q", s.n, got, s.ispravaka)
		}
		if got := ocitanja(s.n); got != s.ocitanja {
			t.Errorf("ocitanja(%d) = %q, očekivano %q", s.n, got, s.ocitanja)
		}
	}
}

// Opis prognoze prenesene na srpsku letvu imao je stalno „dana”, pa je za
// odnos izmjeren na 21 danu pisalo „(21 dana)”.
func TestOpisPrenesenogUzBrojDana(t *testing.T) {
	for zapis, zeli := range map[string]string{
		"batina|7|21|0":   "prognoza prenesena s naše za batinu · odnos ±7 cm (21 dan)",
		"batina|7|1461|0": "prognoza prenesena s naše za batinu · odnos ±7 cm (1.461 dan)",
		"batina|7|24|0":   "prognoza prenesena s naše za batinu · odnos ±7 cm (24 dana)",
		"batina|7|3650|0": "prognoza prenesena s naše za batinu · odnos ±7 cm (3.650 dana)",
	} {
		if got := opisPrenesenog(zapis, nil); got != zeli {
			t.Errorf("%s: %q, očekivano %q", zapis, got, zeli)
		}
	}
}
