package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Listanje bez ponovnog učitavanja radi tako da skripta nađe okvir oko popisa
// i zamijeni mu sadržaj. Listač koji nije u takvom okviru tiho se vraća na
// staro ponašanje — cijela stranica se učita i pogled odskoči na vrh. Kvar se
// ne vidi ni u jednom drugom testu, pa se traži ovdje.
func TestSvakiListacJeUOkviruZaZamjenu(t *testing.T) {
	imenik := filepath.Join("..", "..", "web", "templates")
	stavke, err := os.ReadDir(imenik)
	if err != nil {
		t.Fatal(err)
	}
	listac := regexp.MustCompile(`\{\{template "pager"[^}]*\}\}`)
	for _, s := range stavke {
		if s.IsDir() || !strings.HasSuffix(s.Name(), ".html") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(imenik, s.Name()))
		if err != nil {
			t.Fatal(err)
		}
		tekst := string(b)
		for _, m := range listac.FindAllStringIndex(tekst, -1) {
			if !uOkviru(tekst, m[0]) {
				t.Errorf("%s: listač na položaju %d nije unutar <div data-listanje=…>: %s",
					s.Name(), m[0], strings.TrimSpace(tekst[m[0]:m[1]]))
			}
		}
	}
}

// uOkviru broji otvorene i zatvorene <div> prije položaja i javlja je li
// nedovršeni okvir s data-listanje još otvoren.
func uOkviru(tekst string, poloz int) bool {
	prije := tekst[:poloz]
	oznake := regexp.MustCompile(`<div\b[^>]*>|</div>`).FindAllString(prije, -1)
	var stog []bool
	for _, o := range oznake {
		if strings.HasPrefix(o, "</") {
			if len(stog) > 0 {
				stog = stog[:len(stog)-1]
			}
			continue
		}
		stog = append(stog, strings.Contains(o, "data-listanje="))
	}
	for _, jest := range stog {
		if jest {
			return true
		}
	}
	return false
}

// Okvir bez listača u sebi je mrtvo slovo: skripta bi zamijenila sadržaj, a
// ničim se ne bi pokrenula.
func TestSvakiOkvirZaZamjenuSadrziListac(t *testing.T) {
	imenik := filepath.Join("..", "..", "web", "templates")
	stavke, _ := os.ReadDir(imenik)
	for _, s := range stavke {
		if s.IsDir() || !strings.HasSuffix(s.Name(), ".html") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(imenik, s.Name()))
		tekst := string(b)
		for _, m := range regexp.MustCompile(`<div data-listanje="([^"]+)"`).FindAllStringSubmatchIndex(tekst, -1) {
			kljuc := tekst[m[2]:m[3]]
			ostatak := tekst[m[1]:]
			kraj := krajOkvira(ostatak)
			if !strings.Contains(ostatak[:kraj], `{{template "pager"`) {
				t.Errorf("%s: okvir %q nema listača u sebi", s.Name(), kljuc)
			}
		}
	}
}

// krajOkvira vraća položaj na kojem se okvir zatvara.
func krajOkvira(tekst string) int {
	dubina := 1
	for _, m := range regexp.MustCompile(`<div\b[^>]*>|</div>`).FindAllStringIndex(tekst, -1) {
		if strings.HasPrefix(tekst[m[0]:m[1]], "</") {
			dubina--
			if dubina == 0 {
				return m[0]
			}
			continue
		}
		dubina++
	}
	return len(tekst)
}
