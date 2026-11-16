package web

import (
	"regexp"
	"strconv"
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

// Odluke (16. 11. 2026.), Pomoć 3: ulaz ima sve teme u pet skupina, svako
// poglavlje točno jednom, a broj uz temu je broj podnaslova u njezinom popisu.
func TestUlazPomociImaSveTemeUPetSkupina(t *testing.T) {
	h := pomocHTML(t)
	teme := h[strings.Index(h, `<nav class="pomoc-kazalo"`):]
	teme = teme[:strings.Index(teme, "</nav>")]
	skupine := regexp.MustCompile(`<div class="pomoc-skupina">\s*<h3>([^<]+)</h3>`).FindAllStringSubmatch(teme, -1)
	var imena []string
	for _, s := range skupine {
		imena = append(imena, s[1])
	}
	if got := strings.Join(imena, ", "); got != "Početak, Svaki dan, Obrana i dokumenti, Ljudi i postavke, Registri i arhiva" {
		t.Errorf("skupine tema: %s", got)
	}
	redovi := regexp.MustCompile(`<a class="pomoc-tema" href="#([a-z0-9-]+)">.*?<span>[^<]+</span>(?:<small>(\d+) podnaslov[a]?</small>)?`).FindAllStringSubmatch(teme, -1)
	vidjeno := map[string]bool{}
	for _, r := range redovi {
		if vidjeno[r[1]] {
			t.Errorf("tema #%s je na ulazu dvaput", r[1])
		}
		vidjeno[r[1]] = true
		poglavlje := regexp.MustCompile(`(?s)<section class="detail-section" id="` + r[1] + `">(.*?)</section>`).FindStringSubmatch(h)
		if poglavlje == nil {
			t.Errorf("tema #%s nema poglavlja", r[1])
			continue
		}
		ima := 0
		if d := regexp.MustCompile(`(?s)<details class="pomoc-podnaslovi".*?</details>`).FindString(poglavlje[1]); d != "" {
			ima = strings.Count(d, `<a href="#`)
		}
		if r[2] != strconv.Itoa(ima) && !(r[2] == "" && ima == 0) {
			t.Errorf("tema #%s: uz nju piše %q podnaslova, a popis ih ima %d", r[1], r[2], ima)
		}
	}
	if n := strings.Count(h, `<section class="detail-section" id="`); len(vidjeno) != n {
		t.Errorf("na ulazu je %d tema, a poglavlja %d", len(vidjeno), n)
	}
}
