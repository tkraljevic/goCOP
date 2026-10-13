package web

import "testing"

// Napomena na dnu izvoza inventure: s točkom na kraju ili bez nje, iza nje
// stoji jedna točka. Rečenica o otključavanju završava točkom, pa je izvoz
// pisao „obrisano..”.
func TestNaznakaInventureBezDvijeTocke(t *testing.T) {
	for _, c := range []struct{ napomena, zeli string }{
		{"", ""},
		{"  ", ""},
		{"brojala Petra", " Napomena: brojala Petra."},
		{"brojala Petra. Otključao: Marko Marić, 13. 10. 2026. u 11:16; 1 usklađenje je obrisano.",
			" Napomena: brojala Petra. Otključao: Marko Marić, 13. 10. 2026. u 11:16; 1 usklađenje je obrisano."},
	} {
		if got := naznaka(c.napomena); got != c.zeli {
			t.Errorf("%q: %q", c.napomena, got)
		}
	}
}
