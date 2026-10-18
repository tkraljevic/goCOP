package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dosegUDokumentu je riječ kojom docs/uloge.md u stupcu Doseg piše prirodni
// doseg uloge
var dosegUDokumentu = map[ScopeType]string{
	ScopeAll:     "cijela organizacija",
	ScopeSector:  "sektor",
	ScopeArea:    "branjeno područje",
	ScopeSection: "dionice",
}

// redakUloge je jedan redak tablice uloga u dokumentu
type redakUloge struct {
	naziv, doseg string
	broj         int
}

// celijeRetka su ćelije retka Markdown tablice, bez rubnih crta i razmaka
func celijeRetka(redak string) []string {
	celije := strings.Split(strings.Trim(redak, "|"), "|")
	for i := range celije {
		celije[i] = strings.TrimSpace(celije[i])
	}
	return celije
}

// redciTabliceUloga čita retke svih tablica čije zaglavlje počinje stupcima
// „Uloga” i „Doseg”; ostale tablice dokumenta ne gleda
func redciTabliceUloga(t *testing.T, put string) []redakUloge {
	t.Helper()
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	var out []redakUloge
	uTablici := false
	for i, redak := range strings.Split(string(b), "\n") {
		broj := i + 1
		redak = strings.TrimSpace(redak)
		if !strings.HasPrefix(redak, "|") {
			uTablici = false
			continue
		}
		celije := celijeRetka(redak)
		if len(celije) >= 2 && celije[0] == "Uloga" && celije[1] == "Doseg" {
			uTablici = true
			continue
		}
		// druga tablica ili crta ispod zaglavlja
		if !uTablici || strings.HasPrefix(celije[0], "---") {
			continue
		}
		if len(celije) < 2 {
			t.Errorf("%s:%d: redak tablice uloga nema stupac Doseg", put, broj)
			continue
		}
		out = append(out, redakUloge{naziv: celije[0], doseg: celije[1], broj: broj})
	}
	return out
}

// docs/uloge.md na GitHubu čitaju oni koji program procjenjuju ili pune
// imenik prvog računala, prije nego što vide Pomoć. Zato mora navesti točno
// uloge iz kataloga, s njihovim zadanim nazivom i dosegom: uloga dodana,
// uklonjena ili preimenovana u katalogu, a ne i u dokumentu, ruši test.
func TestDokumentUlogaPratiKatalog(t *testing.T) {
	put := filepath.Join("..", "..", "docs", "uloge.md")
	redci := redciTabliceUloga(t, put)

	poNazivu := map[string][]redakUloge{}
	for _, r := range redci {
		poNazivu[r.naziv] = append(poNazivu[r.naziv], r)
	}
	uKatalogu := map[string]bool{}
	for _, d := range RoleCatalog {
		uKatalogu[d.Name] = true
		nadeni := poNazivu[d.Name]
		switch len(nadeni) {
		case 0:
			t.Errorf("%s: nema uloge %q iz kataloga", put, d.Name)
			continue
		case 1:
		default:
			t.Errorf("%s: uloga %q navedena je %d puta", put, d.Name, len(nadeni))
		}
		if want := dosegUDokumentu[d.Scope]; nadeni[0].doseg != want {
			t.Errorf("%s:%d: doseg uloge %q je %q, a katalog kaže %q", put, nadeni[0].broj, d.Name, nadeni[0].doseg, want)
		}
	}
	for _, r := range redci {
		if !uKatalogu[r.naziv] {
			t.Errorf("%s:%d: uloge %q nema u katalogu", put, r.broj, r.naziv)
		}
	}
}

// Nacrt akta o stupnju obrane sastavlja samo Centar obrane od poplava
// sektora (Odluke (8. 10. 2026.), pitanje 18). Pomoć i INSTALACIJA.md to
// već moraju reći; isto mora i docs/uloge.md, jer se čita prije njih.
func TestDokumentUlogaKazeDaDosegNeDajeAkt(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "uloge.md"))
	if err != nil {
		t.Fatal(err)
	}
	h := strings.Join(strings.Fields(string(b)), " ")
	want := "Doseg dužnosti ne daje pravo na akt o stupnju obrane: nacrt rješenja ili obavijesti sastavlja samo Centar obrane od poplava sektora"
	if !strings.Contains(h, want) {
		t.Errorf("docs/uloge.md ne kaže %q", want)
	}
}
