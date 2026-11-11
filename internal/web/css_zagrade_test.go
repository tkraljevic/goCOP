package web

import (
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Blok bez zatvarajuće zagrade preglednik ne javlja: sve iza njega čita kao
// dio tog bloka. Dva bloka @media (max-width: 599px) ostala su tako otvorena
// pri spajanju grana, pa je na računalu nestao stil Očitanja i svega što u
// style.css stoji iza njih.
func TestCSSZagradeZatvorene(t *testing.T) {
	datoteke, err := fs.Glob(webassets.Files, "static/css/*.css")
	if err != nil {
		t.Fatal(err)
	}
	if len(datoteke) == 0 {
		t.Fatal("nijedna CSS datoteka nije nađena")
	}
	for _, ime := range datoteke {
		sadrzaj, err := webassets.Files.ReadFile(ime)
		if err != nil {
			t.Fatal(err)
		}
		if poruka := provjeriZagrade(string(sadrzaj)); poruka != "" {
			t.Errorf("%s: %s", ime, poruka)
		}
	}
}

func TestProvjeriZagradeNalaziOtvorenBlok(t *testing.T) {
	slucajevi := map[string]string{
		"a { b: c; }\n@media x {\n  a { b: c; }\n": "blok iz reda 2 nije zatvoren",
		"a { b: c; }\n}\n":                         "višak zatvarajuće zagrade u redu 2",
		"/* { */\na { b: c; }\n":                   "",
	}
	for css, ocekivano := range slucajevi {
		if dobiveno := provjeriZagrade(css); dobiveno != ocekivano {
			t.Errorf("provjeriZagrade(%q) = %q, očekivano %q", css, dobiveno, ocekivano)
		}
	}
}

var cssKomentar = regexp.MustCompile(`(?s)/\*.*?\*/`)

// provjeriZagrade vraća opis prve neravnoteže vitičastih zagrada ili prazan
// tekst. Komentari se preskaču, a broj redaka ostaje isti.
func provjeriZagrade(css string) string {
	css = cssKomentar.ReplaceAllStringFunc(css, func(k string) string {
		return strings.Repeat("\n", strings.Count(k, "\n"))
	})
	var otvoreni []int
	for i, redak := range strings.Split(css, "\n") {
		for _, z := range redak {
			switch z {
			case '{':
				otvoreni = append(otvoreni, i+1)
			case '}':
				if len(otvoreni) == 0 {
					return "višak zatvarajuće zagrade u redu " + strconv.Itoa(i+1)
				}
				otvoreni = otvoreni[:len(otvoreni)-1]
			}
		}
	}
	if len(otvoreni) > 0 {
		return "blok iz reda " + strconv.Itoa(otvoreni[0]) + " nije zatvoren"
	}
	return ""
}
