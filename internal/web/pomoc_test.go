package web

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func pomocHTML(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "templates", "pomoc.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Znak „?" u zaglavlju vodi na /pomoc#<modul>, gdje je modul vrijednost
// ActiveNav te stranice. Nestane li sidro, poveznica ne puca nego tiho
// otvori vrh pomoći — korisnik traži svoj odjeljak i ne nalazi ga, a nijedan
// drugi test to ne primijeti.
func TestSvakiModulImaSvojOdjeljakUPomoci(t *testing.T) {
	h := pomocHTML(t)
	sidra := map[string]bool{}
	for _, m := range regexp.MustCompile(`id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		sidra[m[1]] = true
	}
	// vrijednosti ActiveNav koje stranice postavljaju
	for _, modul := range []string{
		"pocetak", "dashboard", "teren", "sections", "stations", "readings", "arhiva",
		"journals", "users", "registers", "organizacija", "territories", "structures",
		"watercourses", "firme", "maintenance", "admin", "moduli", "settings", "sync",
		"profile", "pojmovi", "pomoc",
	} {
		if !sidra[modul] {
			t.Errorf("pomoć nema odjeljak #%s — znak „?“ s te stranice vodi na vrh", modul)
		}
	}
}

// Sidro upisano dvaput vodi na prvo pojavljivanje, pa poveznica završi na
// krivom mjestu bez ijedne naznake.
func TestSidraUPomociSuJedinstvena(t *testing.T) {
	h := pomocHTML(t)
	broj := map[string]int{}
	for _, m := range regexp.MustCompile(`id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		broj[m[1]]++
	}
	var dupli []string
	for id, n := range broj {
		if n > 1 {
			dupli = append(dupli, id)
		}
	}
	sort.Strings(dupli)
	if len(dupli) > 0 {
		t.Errorf("udvostručena sidra u pomoći: %v", dupli)
	}
}

// Kazalo vodi na odjeljke; poveznica bez cilja je mrtva i ne javlja se.
func TestKazaloPomociVodiNaPostojecaSidra(t *testing.T) {
	h := pomocHTML(t)
	sidra := map[string]bool{}
	for _, m := range regexp.MustCompile(`id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		sidra[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`href="#([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		if !sidra[m[1]] {
			t.Errorf("kazalo pomoći vodi na #%s, a tog odjeljka nema", m[1])
		}
	}
	// kazalo mora pokrivati sve odjeljke prve razine
	for _, m := range regexp.MustCompile(`<section class="detail-section" id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		if !strings.Contains(h, `href="#`+m[1]+`"`) {
			t.Errorf("odjeljak #%s nije u kazalu", m[1])
		}
	}
}

// Postupci koje korisnik izvodi rukom moraju biti opisani, jer ih nema u
// sučelju: arhiva se gradi izvan programa, a očitanja se sele alatom.
func TestPomocOpisujePostupkeSArhivom(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		"arhiva-vodostaja", "selidba-arhive", "vodostaji/",
		"ne briše dok arhiva ne dokaže da ga pokriva",
		"arhiva ostaje netaknuta",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje %q", want)
		}
	}
}
