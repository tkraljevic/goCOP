package web

import (
	"regexp"
	"strings"
	"testing"
)

// Odluke (16. 11. 2026.), Pomoć 1, prvi korak: poglavlje s podnaslovima ima
// uz tekst popis „U ovom poglavlju”, i svaka poveznica u njemu vodi na
// mjesto u istom poglavlju. Do sada se podnaslov tražio u kazalu na vrhu.
func TestPoglavljePomociImaPopisPodnaslova(t *testing.T) {
	h := pomocHTML(t)
	poglavlja := regexp.MustCompile(`(?s)<section class="detail-section" id="([a-z0-9-]+)">(.*?)</section>`).FindAllStringSubmatch(h, -1)
	sPopisom := 0
	for _, p := range poglavlja {
		id, tijelo := p[1], p[2]
		popis := regexp.MustCompile(`(?s)<details class="pomoc-podnaslovi"[^>]*>(.*?)</details>`).FindStringSubmatch(tijelo)
		if popis == nil {
			continue
		}
		sPopisom++
		if !strings.Contains(tijelo, `<div class="pomoc-poglavlje">`) {
			t.Errorf("#%s: popis podnaslova nije uz tekst", id)
		}
		for _, a := range regexp.MustCompile(`href="#([a-z0-9-]+)"`).FindAllStringSubmatch(popis[1], -1) {
			if !strings.Contains(tijelo, `id="`+a[1]+`"`) {
				t.Errorf("#%s: podnaslov #%s nije u tom poglavlju", id, a[1])
			}
		}
	}
	if sPopisom < 10 {
		t.Errorf("popis podnaslova ima %d poglavlja, očekujem barem 10", sPopisom)
	}
}
