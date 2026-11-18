package models

import "testing"

// Protok iz krivulje nije izmjeren i ne smije se tako zvati (očitanja,
// 18. 11. 2026.: „izmjereno, iz krivulje (izračunat …)”)
func TestOpisProtoka(t *testing.T) {
	for _, c := range []struct{ metoda, biljeska, zelim string }{
		{"", "", "izmjereno"},
		{FlowMethodADCP, "", "izmjereno, ADCP"},
		{FlowMethodKrilo, "mjerila ekipa", "izmjereno, hidrometrijsko krilo (mjerila ekipa)"},
		{FlowMethodProcjena, "", "procjena"},
		{FlowMethodKrivulja, "", "iz krivulje"},
		{FlowMethodKrivulja, "krivulja HV", "iz krivulje (krivulja HV)"},
		{FlowMethodKrivulja, BiljeskaNaseKrivulje, "iz krivulje goCOP-a, izvor nije dao protok"},
	} {
		if got := OpisProtoka(c.metoda, c.biljeska); got != c.zelim {
			t.Errorf("OpisProtoka(%q, %q) = %q, želim %q", c.metoda, c.biljeska, got, c.zelim)
		}
	}
}
