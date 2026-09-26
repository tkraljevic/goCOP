package db

import "testing"

// Plan sektora A navodi elektrane kao protok; letve se zovu po elektrani i brani.
func TestImeIzProtoka(t *testing.T) {
	for ulaz, want := range map[string]string{
		"ukupni protok na HE Dubrava":  "HE Dubrava",
		"ukupni protok na HE Dubrava ": "HE Dubrava",
		"protok na brani HE Varaždin":  "Brana HE Varaždin",
		"protok na HE Čakovec":         "HE Čakovec",
		"Botovo":                       "Botovo",
		"Molve, cestovni most":         "Molve, cestovni most",
	} {
		if got := ImeIzProtoka(ulaz); got != want {
			t.Errorf("%q → %q, očekivano %q", ulaz, got, want)
		}
	}
}
