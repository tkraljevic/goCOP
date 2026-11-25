package propisi

import (
	"reflect"
	"testing"
)

// Odluke (25. 11. 2026.): točku i članak traži se i brojem: „XXIII.”,
// „točka XXIII”, „t. 23” daju točku, a „čl. 119”, „članak 119.” i sam broj
// članak. Oznake su kao u stalnoj adresi.
func TestUpitTockaIClanak(t *testing.T) {
	for upit, zeli := range map[string]Upit{
		"XXIII.":            {Tocka: "xxiii"},
		" XXIII ":           {Tocka: "xxiii"},
		"točka XXIII.":      {Tocka: "xxiii"},
		"Točku xi":          {Tocka: "xi"},
		"t. 23":             {Tocka: "xxiii"},
		"toč. 4.":           {Tocka: "iv"},
		"čl. 119":           {Clanak: "clanak-119"},
		"članak  119.":      {Clanak: "clanak-119"},
		"Članka 97.a":       {Clanak: "clanak-97a"},
		"119":               {Clanak: "clanak-119"},
		"97.a":              {Clanak: "clanak-97a"},
		"pripremnog stanja": {Korijeni: []string{"pripremn", "stanj"}},
	} {
		if got := RastaviUpit(upit); !reflect.DeepEqual(got, zeli) {
			t.Errorf("RastaviUpit(%q) = %+v, želi %+v", upit, got, zeli)
		}
	}
}

// Odluke (25. 11. 2026.): malo „i” je veznik, a ne točka I.; rimski broj
// bez riječi „točka” traži velika slova, a nepravilan rimski broj nije
// točka, nego se traži kao riječ.
func TestMaloINijeRimski(t *testing.T) {
	for upit, zeli := range map[string]Upit{
		"i":             {},
		"I":             {Tocka: "i"},
		"točka i":       {Tocka: "i"},
		"IIII":          {Korijeni: []string{"iiii"}},
		"točka IIII":    {Korijeni: []string{"tock", "iiii"}},
		"vode i nasipi": {Korijeni: []string{"vode", "nasip"}},
	} {
		if got := RastaviUpit(upit); !reflect.DeepEqual(got, zeli) {
			t.Errorf("RastaviUpit(%q) = %+v, želi %+v", upit, got, zeli)
		}
	}
}

// Odluke (25. 11. 2026.): upit od riječi kraćih od tri slova nema što
// tražiti, pa je prazan.
func TestPrekratakUpit(t *testing.T) {
	for _, upit := range []string{"", "  ", "ab", "o i u"} {
		if got := RastaviUpit(upit); !reflect.DeepEqual(got, Upit{}) {
			t.Errorf("RastaviUpit(%q) = %+v, želi prazno", upit, got)
		}
	}
}
