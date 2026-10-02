package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Što javlja drugo računalo u lokalnoj mreži (naziv, oznaka, adresa,
// poruka greške) i nazivi iz registra koji stižu razmjenom idu u stranicu
// samo kao tekst. Ovaj test čuva mjesta gdje su bili ubačeni kao HTML.
func TestNeprovjereniPodaciNisuHTML(t *testing.T) {
	provjere := map[string][]string{
		"../../web/templates/settings.html":     {`\$\{f\.(name|deviceId|addr)`, `onclick="syncNow\('\$\{`, `'\$\{f\.addr\}'`, `\+ st\.error \+`, `\+ e\.message \+ '</p>'`},
		"../../web/templates/uparivanje.html":   {`\$\{f\.(name|deviceId|addr)`, `dial\('\$\{`, `\+ st\.error \+`, `\+ e\.message \+`},
		"../../web/templates/section_form.html": {`innerHTML = \(s \? s\.label`, `innerHTML = \(TERR_LABELS`, `'"> ' \+ s\.name`, `'">' \+ n \+ '</option>'`},
	}
	// na stranicama uparivanja i postavki svaki podatak koji stiže od
	// poslužitelja ili drugog računala ide u innerHTML samo kroz escPronadjeno
	sirovo := regexp.MustCompile(`innerHTML[^;\n]*\+ *\(?(r|st|e|lines)\.`)
	for _, datoteka := range []string{"../../web/templates/settings.html", "../../web/templates/uparivanje.html"} {
		b, err := os.ReadFile(datoteka)
		if err != nil {
			t.Fatal(err)
		}
		for _, loc := range sirovo.FindAllIndex(b, -1) {
			red := strings.Count(string(b[:loc[0]]), "\n") + 1
			t.Errorf("%s:%d: podatak ide u innerHTML bez escPronadjeno", datoteka, red)
		}
	}
	for datoteka, uzorci := range provjere {
		b, err := os.ReadFile(datoteka)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range uzorci {
			if loc := regexp.MustCompile(u).FindIndex(b); loc != nil {
				red := strings.Count(string(b[:loc[0]]), "\n") + 1
				t.Errorf("%s:%d: neprovjereni podatak ide u stranicu kao HTML (%s)", datoteka, red, u)
			}
		}
	}
}
