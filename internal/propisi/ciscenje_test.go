package propisi

import "testing"

// Odluke (25. 11. 2026.): tekst NN ima meku crticu usred riječi, tvrde i
// dvostruke razmake, a Go-ov \s tvrdi razmak ne hvata. Čisti se prije svih
// uzoraka; prijelom retka iz <br> ostaje, a <sup>3</sup> je „³”.
func TestOcistiMekuCrticuNbspISup(t *testing.T) {
	for ulaz, zeli := range map[string]string{
		"Repub\u00adlike":                "Republike",
		"i\u00a0\u00a0češće":             "i češće",
		"  dva   razmaka\t i tab  ":      "dva razmaka i tab",
		"prvi redak \n\n  drugi redak\r": "prvi redak\ndrugi redak",
		"   ":                            "",
	} {
		if got := ocisti(ulaz); got != zeli {
			t.Errorf("ocisti(%q) = %q, želi %q", ulaz, got, zeli)
		}
	}
	if got := nadznak("m3 i 10"); got != "m³ i ¹⁰" {
		t.Errorf("nadznak = %q", got)
	}
}

// Odluke (25. 11. 2026.): „Clanak”, „Clanak--” i „clanak-” znače isto, a
// klase Box_N, pleft i F_N služe samo za izgled. Klasa se gleda malim
// slovima, bez crtica na kraju i bez „-Copy-N”.
func TestKlasaBezSumaIMalimSlovima(t *testing.T) {
	for ulaz, zeli := range map[string]string{
		"Clanak--":                "clanak",
		"clanak-":                 "clanak",
		"Box_1 T-9-8 pleft":       "t-9-8",
		"T-9-8-Copy-3":            "t-9-8",
		"Box_2 Broj-D pright F_3": "broj-d",
		"Donositelj_5 TB-NA18":    "tb-na18",
		"broj-clanka broj_460815": "broj-clanka",
		"Box_1 pcenter":           "",
		"--":                      "",
		"":                        "",
	} {
		if got := klasaOdlomka(ulaz); got != zeli {
			t.Errorf("klasaOdlomka(%q) = %q, želi %q", ulaz, got, zeli)
		}
	}
}

// Odluke (25. 11. 2026.): ćirilično slovo koje izgleda kao latinično u
// latiničnom tekstu daje upozorenje, a tekst ostaje doslovan.
func TestMijesanoPismoNadjeCirilicu(t *testing.T) {
	for ulaz, zeli := range map[string]bool{
		"obrana оd poplava": true,
		"obrana od poplava": false,
		"Пример":            false,
		"123 ...":           false,
	} {
		if got := MijesanoPismo(ulaz); got != zeli {
			t.Errorf("MijesanoPismo(%q) = %v, želi %v", ulaz, got, zeli)
		}
	}
}
