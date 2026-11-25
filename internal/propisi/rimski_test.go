package propisi

import (
	"strings"
	"testing"
)

// Odluke (25. 11. 2026.): točke Državnog plana imaju rimske brojeve, a
// pretraga „XXIII.” i stalna adresa „xxiii” vode na točku 23. Svaki broj od
// 1 do 3999 mora se pročitati natrag u isti broj, velikim i malim slovima.
func TestRimskiTamoIObratno(t *testing.T) {
	for n := 1; n <= 3999; n++ {
		r := Rimski(n)
		if got, ok := IzRimskog(r); !ok || got != n {
			t.Fatalf("%d → %q → %d, %v", n, r, got, ok)
		}
		if got, ok := IzRimskog(strings.ToLower(r)); !ok || got != n {
			t.Fatalf("%d → %q malim slovima → %d, %v", n, r, got, ok)
		}
	}
	for n, zeli := range map[int]string{4: "IV", 9: "IX", 23: "XXIII", 45: "XLV", 1994: "MCMXCIV", 0: "", 4000: "", -3: ""} {
		if got := Rimski(n); got != zeli {
			t.Errorf("Rimski(%d) = %q, želi %q", n, got, zeli)
		}
	}
}

// Odluke (25. 11. 2026.): samo pravilan zapis je rimski broj, da se riječ ili
// krivo napisan broj ne pročitaju kao točka Plana.
func TestNijeRimskiBroj(t *testing.T) {
	for _, s := range []string{"IIII", "VX", "IC", "", "X1", "XXIII.", "Xx", "MMMM", "IIV", "ČI", "voda"} {
		if n, ok := IzRimskog(s); ok {
			t.Errorf("IzRimskog(%q) = %d, a nije rimski broj", s, n)
		}
	}
}
