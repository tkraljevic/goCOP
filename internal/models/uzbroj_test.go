package models

import "testing"

// Oblik uz broj ide po zadnjoj znamenki, osim druge desetice (11–14) svake
// stotine. Na stranicama je pisalo „21 očitanja”, „2 ispravaka” i
// „1 listova”.
func TestUzBrojHR(t *testing.T) {
	for _, s := range []struct {
		n    int
		zeli string
	}{
		{0, "očitanja"}, {1, "očitanje"}, {2, "očitanja"}, {4, "očitanja"}, {5, "očitanja"},
		{11, "očitanja"}, {12, "očitanja"}, {14, "očitanja"}, {21, "očitanje"}, {22, "očitanja"},
		{25, "očitanja"}, {101, "očitanje"}, {111, "očitanja"}, {-1, "očitanje"},
	} {
		if got := UzBrojHR(s.n, "očitanje", "očitanja", "očitanja"); got != s.zeli {
			t.Errorf("%d: %q, očekivano %q", s.n, got, s.zeli)
		}
	}
	for n, zeli := range map[int]string{
		0: "listova", 1: "list", 2: "lista", 4: "lista", 5: "listova", 11: "listova", 12: "listova",
		14: "listova", 21: "list", 22: "lista", 25: "listova", 101: "list", 111: "listova", 112: "listova", 124: "lista",
	} {
		if got := UzBrojHR(n, "list", "lista", "listova"); got != zeli {
			t.Errorf("%d %s, očekivano %d %s", n, got, n, zeli)
		}
	}
}
