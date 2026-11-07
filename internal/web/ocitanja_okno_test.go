package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stupac popisa uz letvu stoji od prvog iscrtavanja, pa se letva ne pomakne kad
// popis stigne: oznaka mora ostati prazna i bez hidden, a uz nju natpis o učitavanju.
func TestStupacPopisaOdPrvogIscrtavanja(t *testing.T) {
	citaj := func(dijelovi ...string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, dijelovi...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, predlozak := range []string{"reading_history.html", "station_detail.html", "station_history.html"} {
		s := citaj("web", "templates", predlozak)
		i := strings.Index(s, "<aside class=\"letva-okno\"")
		if i < 0 {
			t.Fatalf("%s: nema stupca popisa", predlozak)
		}
		oznaka, _, _ := strings.Cut(s[i:], "</aside>")
		if strings.Contains(oznaka, " hidden") || !strings.HasSuffix(oznaka, ">") {
			t.Errorf("%s: stupac mora biti prazan i vidljiv: %s", predlozak, oznaka)
		}
	}
	css := citaj("web", "static", "css", "style.css")
	if !strings.Contains(css, `.letva-okno:empty::before`) || !strings.Contains(css, "Učitavam popis letava") {
		t.Error("stupac bez popisa ne kaže da se učitava")
	}
	if !strings.Contains(citaj("web", "static", "js", "ocitanja-okno.js"), "okno.hidden = true") {
		t.Error("kad popis ne stigne, stupac mora nestati")
	}
}
