package models

import "testing"

// Izvornik dnevnika nije ovdje samo kad zapis ima otisak, a PDF-a nema
func TestIzvornikDnevnikaNijeOvdje(t *testing.T) {
	for _, s := range []struct {
		ime string
		iz  *IzvornikDnevnika
		je  bool
	}{
		{"nema zapisa", nil, false},
		{"zapis bez otiska", &IzvornikDnevnika{}, false},
		{"PDF je ovdje", &IzvornikDnevnika{Otisak: "abc", PDF: []byte("%PDF")}, false},
		{"otisak bez PDF-a", &IzvornikDnevnika{Otisak: "abc"}, true},
	} {
		if got := s.iz.NijeOvdje(); got != s.je {
			t.Errorf("%s: NijeOvdje = %v, očekivano %v", s.ime, got, s.je)
		}
	}
}
