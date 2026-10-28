package web

import (
	"io/fs"
	"net/http"
	"os"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Natpis o podrijetlu karte stranice upisuju u kartu kao HTML (Leaflet).
// Iz programa se zato sprema samo običan tekst, a karta i natpis koji je
// netko upisao u datoteku ručno pokazuje kao tekst.
func TestNatpisKarteBezHTML(t *testing.T) {
	s, put := cvorSPostavkama(t, sadrzajPostavk, nil)
	napad := `<img src=x onerror="fetch('/posta')">`
	w := spremi(t, s, "/administracija/opcije", obrazacBloka("opcije", otisakDatoteke(t, put), map[string]string{"karta.zasluge": napad}), kaoAdmin)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "običan tekst") {
		t.Errorf("natpis s oznakom: %d", w.Code)
	}
	if b, _ := os.ReadFile(put); string(b) != sadrzajPostavk {
		t.Errorf("natpis s oznakom zapisan:\n%s", b)
	}
	js, err := fs.ReadFile(webassets.Files, "static/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for i, redak := range strings.Split(string(js), "\n") {
		if !strings.Contains(redak, "attribution:") {
			continue
		}
		n++
		if !strings.Contains(redak, "zaslugeKarte(") && !strings.Contains(redak, "escapeHtml(") {
			t.Errorf("app.js:%d: natpis karte ide u kartu bez escapeHtml: %s", i+1, strings.TrimSpace(redak))
		}
	}
	if n == 0 {
		t.Error("app.js nema nijedne karte s natpisom")
	}
}
