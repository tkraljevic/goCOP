package models

import "testing"

// Znak u SVG-u program prikazuje na zaslonu, ali Word ga ne zna nacrtati bez
// rasterske zamjene. Postavke na to moraju upozoriti, inače se izostanak znaka
// primijeti tek na gotovom izvješću.
func TestZnakNijeZaWordPrepoznajeSVG(t *testing.T) {
	slucajevi := []struct {
		mime      string
		ima       bool
		upozorava bool
	}{
		{"image/png", true, false},
		{"image/jpeg", true, false},
		{"IMAGE/PNG", true, false},
		{"image/svg+xml", true, true},
		{"image/webp", true, true},
		{"image/gif", true, true},
		{"", false, false}, // bez znaka nema na što upozoriti
	}
	for _, s := range slucajevi {
		var terms OrgTerms
		if s.ima {
			terms.Logo, terms.LogoMime = []byte("znak"), s.mime
		}
		if terms.ZnakNijeZaWord() != s.upozorava {
			t.Errorf("%q: upozorenje %v, očekivano %v", s.mime, terms.ZnakNijeZaWord(), s.upozorava)
		}
	}
}
