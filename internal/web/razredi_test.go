package web

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Razred koji ne postoji u CSS-u ne javlja se nikako: stranica se iscrta, samo
// bez oblikovanja. Kartice "Unos u arhivu" i "Izvori arhive" na Administraciji
// tako su mjesecima stajale bez ikone, jer su pisane napamet — "dash-card-head"
// umjesto "dash-card-header" i "dash-btn-icon" umjesto "arrow".
//
// Gleda se samo obitelj "dash-", koja je cijela naša i ne dolazi ni iz jedne
// knjižnice, pa nema lažnih uzbuna.
func TestRazrediNadzornePloceImajuSvojCSS(t *testing.T) {
	css, err := webassets.Files.ReadFile("static/css/style.css")
	if err != nil {
		t.Fatal(err)
	}
	izCSSa := map[string]bool{}
	for _, m := range regexp.MustCompile(`\.(dash-[a-z0-9-]+)`).FindAllStringSubmatch(string(css), -1) {
		izCSSa[m[1]] = true
	}
	if len(izCSSa) < 5 {
		t.Fatalf("u CSS-u nađeno samo %d razreda obitelji dash- — provjera ne bi ništa uhvatila", len(izCSSa))
	}

	// Razredi koji nose samo ustroj, bez oblikovanja. Kartica je flex stupac a
	// podnožje se drži dna s margin-top:auto, pa tijelu nijedno pravilo ne
	// treba. Popis je kratak namjerno: svaki novi unos je odluka, ne rupa.
	bezOblikovanja := map[string]bool{
		"dash-card-body": true,
	}

	nepoznati := map[string][]string{}
	razred := regexp.MustCompile(`class="([^"{}]*)"`)
	err = fs.WalkDir(webassets.Files, "templates", func(put string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(put, ".html") {
			return err
		}
		b, err := webassets.Files.ReadFile(put)
		if err != nil {
			return err
		}
		for _, m := range razred.FindAllStringSubmatch(string(b), -1) {
			for _, r := range strings.Fields(m[1]) {
				if strings.HasPrefix(r, "dash-") && !izCSSa[r] && !bezOblikovanja[r] {
					nepoznati[r] = append(nepoznati[r], strings.TrimPrefix(put, "templates/"))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(nepoznati) > 0 {
		imena := make([]string, 0, len(nepoznati))
		for r := range nepoznati {
			imena = append(imena, r)
		}
		sort.Strings(imena)
		for _, r := range imena {
			t.Errorf("razred %q nema oblikovanje u style.css; koriste ga: %s",
				r, strings.Join(nepoznati[r], ", "))
		}
	}
}

// Svaka kartica nadzorne ploče ima ikonu. Bez nje red kartica gubi ritam, a
// oko se ne može zakvačiti ni za što osim naslova.
func TestSvakaKarticaImaIkonu(t *testing.T) {
	b, err := webassets.Files.ReadFile("templates/administracija.html")
	if err != nil {
		t.Fatal(err)
	}
	dijelovi := strings.Split(string(b), `<div class="dash-card">`)
	if len(dijelovi) < 5 {
		t.Fatalf("nađeno %d kartica — provjera ne bi ništa uhvatila", len(dijelovi)-1)
	}
	for _, d := range dijelovi[1:] {
		kraj := strings.Index(d, "dash-card-body")
		if kraj < 0 {
			t.Error("kartica nema tijelo")
			continue
		}
		glava := d[:kraj]
		if !strings.Contains(glava, "dash-card-icon-box") {
			naslov := "?"
			if i := strings.Index(d, `dash-card-title">`); i >= 0 {
				ostatak := d[i+len(`dash-card-title">`):]
				if j := strings.Index(ostatak, "<"); j >= 0 {
					naslov = ostatak[:j]
				}
			}
			t.Errorf("kartica %q nema ikonu", naslov)
		}
	}
}
