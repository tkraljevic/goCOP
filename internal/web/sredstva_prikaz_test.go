package web

import (
	"testing"

	"gocop/internal/models"
)

// Čipovi broje retke; tablica se otvara na „ima”, a kad takvih nema, na
// zadnjem čipu koji pokazuje sve.
func TestCipoviPrikaza(t *testing.T) {
	d := MtsPageData{
		Stanje:  []models.StanjeVrste{{Vrsta: models.VrstaSredstva{ID: "a"}, Ukupno: 3}, {Vrsta: models.VrstaSredstva{ID: "b"}}, {Vrsta: models.VrstaSredstva{ID: "c"}, Potrebno: 2}},
		Potrebe: map[string]models.Potreba{"b": {Kolicina: 5}, "c": {Kolicina: 0}},
		Popis: &models.Popis{Stavke: []models.PopisnaStavka{
			{Knjizno: 7, Utvrdjeno: 7}, {Knjizno: 10, Utvrdjeno: 0}, {Utvrdjeno: 4}, {},
		}},
	}
	broj := func(p PrikazRedaka) []int {
		var b []int
		for _, c := range p.Cipovi {
			b = append(b, c.Broj)
		}
		return b
	}
	for ime, s := range map[string]struct {
		p      PrikazRedaka
		cilj   string
		zadano string
		broj   []int
	}{
		"stanje":          {d.PrikazStanja(), "sr-stanje", "ima", []int{2, 3}},
		"potrebe":         {d.PrikazPotreba(), "sr-potrebe", "ima", []int{1, 3}},
		"dokument":        {d.PrikazPopisa(), "sr-stavke", "ima", []int{2, 3, 4}},
		"obrazac":         {d.PrikazObrascaPopisa(), "sr-stavke", "ima", []int{2, 4}},
		"potrebe prazno":  {MtsPageData{Stanje: d.Stanje}.PrikazPotreba(), "sr-potrebe", "sve", []int{0, 3}},
		"bez inventure":   {MtsPageData{}.PrikazPopisa(), "sr-stavke", "sve", []int{0, 0, 0}},
		"obrazac bez nje": {MtsPageData{}.PrikazObrascaPopisa(), "sr-stavke", "sve", []int{0, 0}},
	} {
		b := broj(s.p)
		if s.p.Cilj != s.cilj || s.p.Zadano != s.zadano || len(b) != len(s.broj) {
			t.Errorf("%s: %+v", ime, s.p)
			continue
		}
		for i := range b {
			if b[i] != s.broj[i] {
				t.Errorf("%s: brojevi %v, očekivano %v", ime, b, s.broj)
				break
			}
		}
	}
}

// Uz naslov skupine piše koliko njezinih vrsta ima.
func TestBrojImaSkupine(t *testing.T) {
	g := GrupaStanja{Redci: []models.StanjeVrste{{Ukupno: 3}, {}, {Potrebno: 1}}}
	if g.BrojIma() != 2 {
		t.Errorf("ima %d", g.BrojIma())
	}
}
