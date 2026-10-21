package models

import "testing"

// Izvor se ispisuje svojim imenom, preračun imenom letve iz koje je
// preračunat, a nepoznat izvor svojom šifrom.
func TestNazivIzvora(t *testing.T) {
	for _, p := range []struct{ izvor, zeli string }{
		{"vizugy-ddvizig", "Mađarska vodoprivreda (DDVIZIG)"},
		{"vituki", "vizugy.hu, Mađarska"},
		{"his2000", "DHMZ, ovjereno"},
		{"preracun-hq", "preračunato iz krivulje protoka"},
		{"preracun-hq-izvan", "preračunato iz krivulje protoka, izvan mjerenog odnosa"},
		{"neki-novi", "neki-novi"},
	} {
		if got := NazivIzvora(p.izvor); got != p.zeli {
			t.Errorf("NazivIzvora(%q) = %q, očekivano %q", p.izvor, got, p.zeli)
		}
	}
}
