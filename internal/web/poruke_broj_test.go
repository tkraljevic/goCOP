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
