package web

import (
	"io/fs"
	"regexp"
	"strconv"
	"testing"

	webassets "gocop/web"
)

// Slike u Pomoći (Odluke (16. 11. 2026.)): svaka ima svijetlu i tamnu
// snimku, opis za čitač zaslona i širinu, a u mapi nema slike koju Pomoć
// ne prikazuje. Snimke dolaze s pokazne kopije s izmišljenim podacima.
func TestSlikeUPomociPostojeIImajuOpis(t *testing.T) {
	h := pomocHTML(t)
	staticFS, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		t.Fatal(err)
	}
	koristi := map[string]bool{}
	slike := regexp.MustCompile(`\{\{template "pomocSlika" \(list "([a-z0-9-]+)" "([^"]*)" "([^"]*)"\)\}\}`).FindAllStringSubmatch(h, -1)
	if len(slike) < 3 {
		t.Fatalf("u Pomoći je %d slika, očekujem barem 3", len(slike))
	}
	for _, s := range slike {
		ime, opis, sirina := s[1], s[2], s[3]
		if len([]rune(opis)) < 30 {
			t.Errorf("slika %s: opis %q je prekratak za čitač zaslona", ime, opis)
		}
		if w, err := strconv.Atoi(sirina); err != nil || w < 200 || w > 1000 {
			t.Errorf("slika %s: širina %q", ime, sirina)
		}
		for _, tema := range []string{"svijetla", "tamna"} {
			put := "img/pomoc/" + ime + "-" + tema + ".webp"
			koristi[put] = true
			if _, err := fs.Stat(staticFS, put); err != nil {
				t.Errorf("slika %s nema snimke %s", ime, put)
			}
		}
	}
	datoteke, err := fs.Glob(staticFS, "img/pomoc/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range datoteke {
		if !koristi[d] {
			t.Errorf("%s nije ni u jednom odlomku Pomoći", d)
		}
	}
}
