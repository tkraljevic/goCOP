package models

import "testing"

// Upisi u list dijele se na naredbe rukovoditelja i bilješke onoga tko list
// ne parafira (dežurni operater), svaka skupina redom upisa (Odluke (8. 10.
// 2026.), pitanje 1). Podaci su izmišljeni.
func TestUpisiNaredbeIBiljeske(t *testing.T) {
	l := VodocuvarskiList{Upisi: []UpisRukovoditelja{
		{Ime: "Ivo Ivić", Tekst: "Pojačati obilazak"},
		{Ime: "Ana Anić", Tekst: "Vodostaj raste", Biljeska: true},
		{Ime: "Ivo Ivić", Tekst: "Javiti se u 18 h"},
	}}
	naredbe, biljeske := l.UpisiNaredbe(), l.UpisiBiljeske()
	if len(naredbe) != 2 || naredbe[0].Tekst != "Pojačati obilazak" || naredbe[1].Tekst != "Javiti se u 18 h" {
		t.Errorf("naredbe: %+v", naredbe)
	}
	if len(biljeske) != 1 || biljeske[0].Ime != "Ana Anić" {
		t.Errorf("bilješke: %+v", biljeske)
	}
	if (VodocuvarskiList{}).UpisiBiljeske() != nil {
		t.Error("list bez upisa ima bilješke")
	}
}
