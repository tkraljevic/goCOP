package web

import (
	"regexp"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Značka ugroženog područja nije se dala maknuti: klik na × nije radio ništa.
//
// Brisalo se s `filter(x => x !== t)`, a `t` je objekt iz ranijeg JSON.parse.
// Svaki JSON.parse stvara nove objekte, pa je usporedba po istovjetnosti bila
// istinita za svaki element i ništa se nije izbacivalo. Značke vodomjera rade
// jer se ondje uspoređuje niz znakova.
//
// Ovo je provjera nad tekstom predloška, ne nad ponašanjem u pregledniku:
// program nema pokretač JavaScripta i ne želi ga. Drži baš taj obrazac, koji je
// lako ponoviti pri sljedećoj značci.
func TestZnackeNeBriseUsporedbomObjekata(t *testing.T) {
	b, err := webassets.Files.ReadFile("templates/section_form.html")
	if err != nil {
		t.Fatal(err)
	}
	izvor := string(b)

	// Usporedba raščlanjenog objekta sa zapamćenim objektom. Go-ov regexp nema
	// povratne reference, pa se podudaranje imena provjerava ovdje.
	po := regexp.MustCompile(`JSON\.parse\([^)]*\)\.filter\(\s*(\w+)\s*=>\s*(\w+)\s*!==\s*(\w+)\s*\)`)
	for _, m := range po.FindAllStringSubmatch(izvor, -1) {
		param, lijevo, desno := m[1], m[2], m[3]
		if param != lijevo {
			continue // ne uspoređuje se sam element
		}
		// Niz znakova se smije uspoređivati ovako; objekt ne. Vodomjeri drže
		// same oznake, pa im je ovo ispravno.
		if desno == "id" {
			continue
		}
		t.Errorf("značka se briše usporedbom objekata (%s !== %s) — JSON.parse svaki put stvara nove "+
			"objekte, pa filtar ne izbacuje ništa", lijevo, desno)
	}

	// Brisanje područja mora ići kroz funkciju koja uspoređuje po ključu.
	if !strings.Contains(izvor, "function makniPodrucje(") {
		t.Error("nema makniPodrucje — brisanje područja opet ovisi o istovjetnosti objekta")
	}
	if !strings.Contains(izvor, "function kljucPodrucja(") {
		t.Error("nema kljucPodrucja — ključ se računa na više mjesta i može se razići")
	}
	if !strings.Contains(izvor, "makniPodrucje(el, t)") {
		t.Error("značka područja ne zove makniPodrucje")
	}
}
