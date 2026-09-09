package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Boje stupnjeva obrane propisane su službenim dokumentima: pripremno plavo,
// redovna žuto, izvanredna narančasto, izvanredno stanje crveno. Stoje kao
// jedna ljestvica po temi, a pilule, karte stupnjeva, crte na grafu i na
// presjeku korita čitaju odande.
//
// Prije su bile upisane u dvadesetak razreda, a izvanredna i izvanredno stanje
// dijelili su istu — na presjeku korita se dvije crte nisu razlikovale.
func TestLjestvicaBojaStojiNaJednomMjestu(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)

	// Svaka od tri teme — svijetla, tamna po postavci sustava i tamna po
	// izboru — mora imati svih dvanaest vrijednosti.
	for _, faza := range []string{"p", "r", "i", "s"} {
		for _, dio := range []string{"bg", "fg", "crta"} {
			ime := "--faza-" + faza + "-" + dio
			if n := strings.Count(css, ime+":"); n != 3 {
				t.Errorf("%s definiran %d puta, očekivano 3 (svijetla i dvije tamne teme)", ime, n)
			}
		}
	}

	// Razredi stupnjeva ne smiju nositi vlastitu boju: promjena ljestvice mora
	// se raditi na jednom mjestu.
	boja := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\(`)
	for _, razred := range []string{
		".threshold-pill.prep", ".threshold-pill.regular", ".threshold-pill.emerg", ".threshold-pill.crit",
		".prag-kartica.prep", ".prag-kartica.regular", ".prag-kartica.emerg", ".prag-kartica.crit",
	} {
		i := strings.Index(css, razred)
		if i < 0 {
			t.Errorf("razred %s ne postoji", razred)
			continue
		}
		kraj := strings.Index(css[i:], "}")
		if kraj < 0 {
			continue
		}
		blok := css[i : i+kraj]
		if boja.MatchString(blok) {
			t.Errorf("%s ima upisanu boju umjesto var(--faza-…): %s", razred, strings.TrimSpace(blok))
		}
	}

	// Četiri stupnja moraju imati četiri različite crte, inače se na presjeku
	// i na grafu dva stapaju u jedno.
	for _, skup := range []string{".korito-graf .korito-prag", ".chart .thr"} {
		vidjeno := map[string]string{}
		for _, f := range []string{"prep", "regular", "emerg", "crit"} {
			pravilo := skup + "." + f + " {"
			i := strings.Index(css, pravilo)
			if i < 0 {
				t.Errorf("nema pravila %s", pravilo)
				continue
			}
			red := css[i : i+strings.Index(css[i:], "}")]
			m := regexp.MustCompile(`var\(--faza-[a-z]-crta\)`).FindString(red)
			if m == "" {
				t.Errorf("%s ne čita boju iz ljestvice: %s", pravilo, strings.TrimSpace(red))
				continue
			}
			if prije, ima := vidjeno[m]; ima {
				t.Errorf("%s i %s dijele istu boju %s", prije, pravilo, m)
			}
			vidjeno[m] = pravilo
		}
	}
}
