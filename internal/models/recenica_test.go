package models

import "testing"

func TestZavrsenaRecenica(t *testing.T) {
	for _, c := range []struct{ tekst, zeli string }{
		{"", ""},
		{"   ", ""},
		{"brojala Petra", "brojala Petra."},
		{"Brojala Petra.", "Brojala Petra."},
		{"Fali li lopata?", "Fali li lopata?"},
		{"Pazi!", "Pazi!"},
		{" oštećeno 5 ćuskija ", "oštećeno 5 ćuskija."},
		{"nedostaje čekić", "nedostaje čekić."},
	} {
		if got := ZavrsenaRecenica(c.tekst); got != c.zeli {
			t.Errorf("%q: %q, očekivano %q", c.tekst, got, c.zeli)
		}
	}
}
