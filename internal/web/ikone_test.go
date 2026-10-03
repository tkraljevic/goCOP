package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Svaka ikona iz predložaka i koda mora postojati u img/icons.svg. Ikona
// koje nema ne javlja grešku nego ostavlja praznu pločicu uz naslov, pa se
// godinama nije primijetilo da „Obračun sati” nema svoju.
func TestSveIkoneSuUSpriteu(t *testing.T) {
	sprite, err := webassets.Files.ReadFile("static/img/icons.svg")
	if err != nil {
		t.Fatal(err)
	}
	ima := map[string]bool{}
	for _, m := range regexp.MustCompile(`<symbol id="([a-z0-9-]+)"`).FindAllStringSubmatch(string(sprite), -1) {
		ima[m[1]] = true
	}
	poziv := regexp.MustCompile(`icon "([a-z0-9-]+)"`)
	for _, korijen := range []string{filepath.Join("..", "..", "web", "templates"), filepath.Join("..")} {
		err := filepath.WalkDir(korijen, func(put string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !(strings.HasSuffix(put, ".html") || strings.HasSuffix(put, ".go")) || strings.HasSuffix(put, "_test.go") {
				return err
			}
			b, err := os.ReadFile(put)
			if err != nil {
				return err
			}
			for _, m := range poziv.FindAllStringSubmatch(string(b), -1) {
				if !ima[m[1]] {
					t.Errorf("%s: ikona %q nije u img/icons.svg", put, m[1])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
