package web

import (
	"io/fs"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Karta slivova po crtežu 17e, 6d: boje međusliva i kišomjera su tokeni za
// obje teme (ne stalne boje u kodu), a oznaka međusliva stoji stalno na karti.
func TestKartaSlivovaTokeniIOznake(t *testing.T) {
	citaj := func(put string) string {
		b, err := fs.ReadFile(webassets.Files, put)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	tokeni := citaj("static/css/tokens.css")
	for _, token := range []string{"--karta-sliv:", "--karta-kisomjer:", "--karta-tocka-podloga:", "--karta-neaktivno:"} {
		if n := strings.Count(tokeni, token); n != 2 {
			t.Errorf("token %s je definiran %d puta, a treba za svijetlu i tamnu temu", token, n)
		}
	}
	js := citaj("static/js/app.js")
	if !strings.Contains(js, "kis-sliv-oznaka") || strings.Contains(js, "bojeSliva") || strings.Contains(js, "'#fff' : (t.aktivan") {
		t.Error("karta slivova nema stalne oznake ili još drži boje u kodu")
	}
}
