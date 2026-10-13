package web

import (
	"strings"
	"testing"
)

// Pomoć o pretplati i arhivi vodostaja prati program: od 0.0.38 pretplata
// „Arhiva vodostaja” se sprema, a pakete donosi i pravilo „Sve vrste” za
// područje ili sektor, bez obzira na godine.
func TestPomocOpisujeTkoDobivaPaketeArhive(t *testing.T) {
	h := pomocHTML(t)
	if strings.Contains(h, "zasad se ne da spremiti") {
		t.Error("pomoć i dalje kaže da se pretplata na arhivu ne da spremiti")
	}
	for _, want := range []string{
		"za jedno područje ili sektor", "Godine se tada ne gledaju",
		"za sva područja donosi arhivu samo", "0.0.38 ili novijim",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć o paketima arhive nema %q", want)
		}
	}
}
