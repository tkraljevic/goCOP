package razmjena

import (
	"encoding/json"
	"slices"
	"testing"
)

// Odluke (30. 10. 2026.), 13: uloge računala putuju u znanju granice samo
// za prikaz. Bez uloga znanje ostaje bajt po bajt isto, a s njima ključ
// "uloge" stoji iza ostalih polja i ne poklapa se ni s jednim poljem granice.
func TestUlogeUZnanjuNaZici(t *testing.T) {
	bez := Znanje{Mogucnosti: []string{OznakaRazmjena}}
	if b, err := json.Marshal(bez); err != nil || string(b) != `{"mogucnosti":["razmjena/1"]}` {
		t.Errorf("bez uloga: %s %v", b, err)
	}
	s := Znanje{Mogucnosti: []string{OznakaRazmjena}, Uloge: []string{UlogaIzdaje, UlogaPreuzima}}
	if b, err := json.Marshal(s); err != nil || string(b) != `{"mogucnosti":["razmjena/1"],"uloge":["izdaje","preuzima"]}` {
		t.Errorf("s ulogama: %s %v", b, err)
	}
	var stiglo Znanje
	if err := json.Unmarshal([]byte(`{"uloge":["preuzima"]}`), &stiglo); err != nil || !slices.Equal(stiglo.Uloge, []string{UlogaPreuzima}) {
		t.Errorf("primljeno: %+v %v", stiglo, err)
	}
}

// Od uloga druge strane ostaju samo poznate, poredane i bez ponavljanja.
func TestUlogeOciscene(t *testing.T) {
	for _, x := range []struct {
		stiglo, ostaje []string
	}{
		{nil, nil},
		{[]string{}, nil},
		{[]string{"nepoznata", "IZDAJE", ""}, nil},
		{[]string{UlogaPreuzima, UlogaIzdaje, UlogaPreuzima, "brise-sve"}, []string{UlogaIzdaje, UlogaPreuzima}},
		{[]string{UlogaPreuzima}, []string{UlogaPreuzima}},
	} {
		if o := (Znanje{Uloge: x.stiglo}).Ocisceno(); !slices.Equal(o.Uloge, x.ostaje) || (x.ostaje == nil && o.Uloge != nil) {
			t.Errorf("%q: ostaje %q, a treba %q", x.stiglo, o.Uloge, x.ostaje)
		}
	}
}
