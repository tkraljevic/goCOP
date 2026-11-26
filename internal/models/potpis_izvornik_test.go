package models

import "testing"

// Izvornik lista nije ovdje samo kad zapis ima otisak, a PDF-a nema
func TestIzvornikNijeOvdje(t *testing.T) {
	sPDF := &IzvornikLista{Otisak: "abc", PDF: []byte("%PDF")}
	for _, s := range []struct {
		ime string
		iz  *IzvornikLista
		je  bool
	}{
		{"nema zapisa", nil, false},
		{"zapis bez otiska", &IzvornikLista{}, false},
		{"PDF je ovdje", sPDF, false},
		{"otisak bez PDF-a", &IzvornikLista{Otisak: "abc"}, true},
	} {
		if got := s.iz.NijeOvdje(); got != s.je {
			t.Errorf("%s: NijeOvdje = %v, očekivano %v", s.ime, got, s.je)
		}
	}
}
