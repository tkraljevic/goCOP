package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Tablica koja na telefonu ne stane razlaže se u kartice, a naslov stupca uz
// svaku vrijednost skripta uzima iz zaglavlja tablice. Tablica bez zaglavlja
// tako bi se složila u niz golih vrijednosti bez ijedne oznake.
//
// Zato svaka tablica mora imati <thead>. Provjerava se u predlošcima, jer se
// tablice dodaju ondje a mehanizam živi drugdje i lako se zaboravi.
func TestSvakaTablicaImaZaglavlje(t *testing.T) {
	predlosci, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	imena, err := fs.Glob(predlosci, "*.html")
	if err != nil {
		t.Fatal(err)
	}
	tablica := regexp.MustCompile(`(?s)<table[^>]*class="[^"]*data-table[^"]*"[^>]*>(.*?)</table>`)
	ukupno := 0
	for _, ime := range imena {
		b, err := fs.ReadFile(predlosci, ime)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range tablica.FindAllStringSubmatch(string(b), -1) {
			ukupno++
			if !strings.Contains(m[1], "<thead") {
				t.Errorf("%s: tablica bez <thead> — složena u kartice ostala bi bez naslova stupaca:\n  %.90s…",
					ime, strings.TrimSpace(m[1]))
			}
		}
	}
	if ukupno < 20 {
		t.Errorf("pronađeno samo %d tablica — je li se izraz razišao s predlošcima?", ukupno)
	}
	t.Logf("provjereno tablica: %d", ukupno)
}
