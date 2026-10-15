package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Obrazac kakav ga preglednik pošalje natrag: testovi njime provjeravaju da
// se spremljena vrijednost iz obrasca vraća onakva kakva je spremljena, pa
// spremanje bez ijedne izmjene ništa ne mijenja.

var (
	reUnos       = regexp.MustCompile(`<input\b[^>]*>`)
	reTekstPolje = regexp.MustCompile(`(?s)<textarea\b([^>]*)>(.*?)</textarea>`)
	reIzbornik   = regexp.MustCompile(`(?s)<select\b([^>]*)>(.*?)</select>`)
	reOpcija     = regexp.MustCompile(`<option\b[^>]*>`)
)

// atributHTML vraća vrijednost atributa iz oznake; ok=false kad ga nema
func atributHTML(oznaka, ime string) (string, bool) {
	m := regexp.MustCompile(`\s` + ime + `="([^"]*)"`).FindStringSubmatch(oznaka)
	if m == nil {
		return "", false
	}
	return html.UnescapeString(m[1]), true
}

// ima javlja nosi li oznaka logički atribut (checked, selected, disabled)
func ima(oznaka, ime string) bool {
	return regexp.MustCompile(`\s` + ime + `(\s|/?>|=)`).MatchString(oznaka)
}

// poljaObrasca čita sa stranice obrazac s akcijom akcija i vraća ono što bi
// preglednik poslao gumbom bez imena: polja s imenom osim onemogućenih i
// gumba, samo označene kvačice i izbore, tekst iz textarea i izabranu
// opciju izbornika (ili prvu, kad nijedna nije izabrana)
func poljaObrasca(t *testing.T, stranica, akcija string) url.Values {
	t.Helper()
	pocetak := strings.Index(stranica, `action="`+akcija+`"`)
	if pocetak < 0 {
		t.Fatalf("na stranici nema obrasca %s", akcija)
	}
	obrazac := stranica[pocetak:]
	obrazac = obrazac[:strings.Index(obrazac, "</form>")]
	v := url.Values{}
	for _, oznaka := range reUnos.FindAllString(obrazac, -1) {
		ime, ok := atributHTML(oznaka, "name")
		vrsta, _ := atributHTML(oznaka, "type")
		switch {
		case !ok || ima(oznaka, "disabled") || vrsta == "submit" || vrsta == "button":
			continue
		case (vrsta == "checkbox" || vrsta == "radio") && !ima(oznaka, "checked"):
			continue
		}
		vrijednost, _ := atributHTML(oznaka, "value")
		v.Add(ime, vrijednost)
	}
	for _, m := range reTekstPolje.FindAllStringSubmatch(obrazac, -1) {
		if ime, ok := atributHTML(m[1], "name"); ok {
			v.Add(ime, html.UnescapeString(strings.TrimPrefix(m[2], "\n")))
		}
	}
	for _, m := range reIzbornik.FindAllStringSubmatch(obrazac, -1) {
		ime, ok := atributHTML(m[1], "name")
		opcije := reOpcija.FindAllString(m[2], -1)
		if !ok || len(opcije) == 0 {
			continue
		}
		izabrana := opcije[0]
		for _, o := range opcije {
			if ima(o, "selected") {
				izabrana = o
				break
			}
		}
		vrijednost, _ := atributHTML(izabrana, "value")
		v.Add(ime, vrijednost)
	}
	return v
}

// Površina u hektarima čita se kao i drugi brojevi u obrascima: s
// decimalnim zarezom ili točkom i s razdjelnicima tisućica kakve je obrazac
// prije pisao („1.500,0”) ili ih ubacuje Excel. Prije se takav zapis tiho
// pretvarao u nulu, pa bi spremanje izvješća površinu izbrisalo.
func TestIzvjesceIzObrascaCitaHektare(t *testing.T) {
	obrazac := url.Values{"popl_sumske": {"1.500,0"}, "popl_poljoprivredne": {"1 234,5"}, "popl_ostale": {"0,25"}}
	r := httptest.NewRequest(http.MethodPost, "/izvjesca", strings.NewReader(obrazac.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	p := izvjesceIzObrasca(r).Sadrzaj.Poplavljeno
	if p.SumskeHa != 1500 || p.PoljoprivredneHa != 1234.5 || p.OstaleHa != 0.25 {
		t.Errorf("površine iz obrasca: %v, %v, %v", p.SumskeHa, p.PoljoprivredneHa, p.OstaleHa)
	}
}
