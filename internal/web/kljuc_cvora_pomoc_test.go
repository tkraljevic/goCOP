package web

import (
	"strings"
	"testing"

	"gocop/internal/peers"
)

// Pomoć opisuje ključ čvora kako ga program provjerava pri pokretanju
// (Odluke (8. 10. 2026.), plan izdanja 0.0.35, stavka 6): postojeći čvor bez
// ključa ne kreće (pod Postavom razlog piše i na ploči), a novi identitet
// dobiva samo zastavicom koju program doista čita; bez gocop.toml ime uzima
// iz baze.
func TestPomocKljucCvora(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	for _, mora := range []string{
		"<code>" + peers.KeyFileName + "</code>",
		"Svjež čvor ga napravi sam",
		"bez njega se ne pokreće",
		"Pod Postavom isto piše i na ploči tog računala",
		"<code>-" + peers.ZastavicaNovogKljuca + "</code>",
		"ime uzima iz baze i ne zapisuje novi primjer postavki",
	} {
		if !strings.Contains(h, mora) {
			t.Errorf("pomoć ne kaže %q", mora)
		}
	}
}
