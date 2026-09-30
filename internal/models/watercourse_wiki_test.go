package models

import "testing"

func TestWikiNaslov(t *testing.T) {
	for _, c := range []struct{ upis, naslov, adresa string }{
		{"https://hr.wikipedia.org/wiki/Kara%C5%A1ica_(Dunav)", "Karašica (Dunav)", "https://hr.wikipedia.org/wiki/Kara%C5%A1ica_%28Dunav%29"},
		{"https://hr.m.wikipedia.org/wiki/Dunav#Tok", "Dunav", "https://hr.wikipedia.org/wiki/Dunav"},
		{"Vuka", "Vuka", "https://hr.wikipedia.org/wiki/Vuka"},
		{"Stara_Drava", "Stara Drava", "https://hr.wikipedia.org/wiki/Stara_Drava"},
		{"https://en.wikipedia.org/wiki/Karasica", "https://en.wikipedia.org/wiki/Karasica", "https://en.wikipedia.org/wiki/Karasica"},
		{"", "", ""},
	} {
		n := WikiNaslov(c.upis)
		if n != c.naslov {
			t.Errorf("%q: naslov %q, htio %q", c.upis, n, c.naslov)
		}
		if a := (Watercourse{WikiSlug: n}).WikiURL(); a != c.adresa {
			t.Errorf("%q: adresa %q, htio %q", c.upis, a, c.adresa)
		}
	}
}
