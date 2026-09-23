package web

import "testing"

// Račun e-pošte je adresa, a mletva.voda.hr traži samo korisničko ime domene.
func TestKorisnikDomene(t *testing.T) {
	for ulaz, treba := range map[string]string{
		"pero.peric@voda.hr": "pero.peric",
		" pero ":             "pero",
		"@voda.hr":           "@voda.hr",
	} {
		if got := KorisnikDomene(ulaz); got != treba {
			t.Errorf("%q → %q, a treba %q", ulaz, got, treba)
		}
	}
}
