package models

import "testing"

// Spremljen list bez rednog broja je nacrt za dan koji još nije počeo
// (Odluke (8. 10. 2026.), pitanja 5 i 6). Nespremljen list i prenesen list
// nisu takav nacrt, a list s brojem nije bez broja.
func TestNacrtBezBroja(t *testing.T) {
	for _, s := range []struct {
		ime string
		l   VodocuvarskiList
		bez bool
	}{
		{"spremljen nacrt bez broja", VodocuvarskiList{ID: "l1"}, true},
		{"list s brojem", VodocuvarskiList{ID: "l1", Broj: 4}, false},
		{"još nespremljen list", VodocuvarskiList{}, false},
		{"prenesen iz ranije evidencije", VodocuvarskiList{ID: "l1", Rekonstrukcija: true}, false},
	} {
		if got := s.l.NacrtBezBroja(); got != s.bez {
			t.Errorf("%s: NacrtBezBroja = %v, očekivano %v", s.ime, got, s.bez)
		}
	}
}
