package models

import (
	"testing"
	"time"
)

// Riječi stanja lista pišu se kao na crtežu: čeka ovjeru i ovjeren.
func TestStanjeLista(t *testing.T) {
	sad := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		l      VodocuvarskiList
		zelimo string
	}{
		{VodocuvarskiList{ID: "l1"}, "u pisanju"},
		{VodocuvarskiList{ID: "l1", PredanoAt: &sad}, "čeka ovjeru"},
		{VodocuvarskiList{ID: "l1", PredanoAt: &sad, PotvrdenoAt: &sad}, "ovjeren"},
		{VodocuvarskiList{ID: "l1", Rekonstrukcija: true}, "ovjereno prijenosom"},
	} {
		if got := s.l.Stanje(); got != s.zelimo {
			t.Errorf("Stanje = %q, očekivano %q", got, s.zelimo)
		}
	}
}

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
