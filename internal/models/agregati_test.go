package models

import "testing"

// Obrazac nudi onoliko agregata koliko stanica ima crpki; bez poznatog
// broja tri, a upisani sati se ne skrivaju
func TestAgregataZaOcitanje(t *testing.T) {
	sati := 5
	for _, s := range []struct {
		opis, kapacitet string
		ocitanje        Reading
		zeli            int
	}{
		{"jedna crpka", "1,5", Reading{}, 1},
		{"dvije crpke", "2x2,5", Reading{}, 2},
		{"tri crpke", "2,5 + 2x1,75", Reading{}, 3},
		{"broj se ne zna", "", Reading{}, 3},
		{"više crpki nego polja", "2x1 + 2x1", Reading{}, 3},
		{"upisan drugi agregat na stanici s jednom crpkom", "1,5", Reading{AgHours2: &sati}, 2},
		{"upisan treći agregat", "2x2,5", Reading{AgHours3: &sati}, 3},
	} {
		if got := (Structure{CapacityText: s.kapacitet}).AgregataZaOcitanje(s.ocitanje); got != s.zeli {
			t.Errorf("%s: %d, želi %d", s.opis, got, s.zeli)
		}
	}
}

// Povijest očitanja ne pokazuje nulu za agregat bez upisa
func TestOpisSatiAgregata(t *testing.T) {
	a, b := 12, 4
	for zeli, r := range map[string]Reading{
		"12":     {AgHours1: &a},
		"12/—/4": {AgHours1: &a, AgHours3: &b},
		"—/4":    {AgHours2: &b},
		"":       {},
	} {
		if got := r.OpisSatiAgregata(); got != zeli {
			t.Errorf("%q, želi %q", got, zeli)
		}
	}
}

// Upisani broj agregata ima prednost pred kapacitetom; bez njega broj
// slijedi iz kapaciteta, a izvan granica se odbija
func TestBrojCrpkiIUpisaniBrojAgregata(t *testing.T) {
	for _, s := range []struct {
		opis string
		st   Structure
		zeli int
	}{
		{"samo kapacitet", Structure{CapacityText: "2x2,5"}, 2},
		{"upisani broj pobjeđuje kapacitet", Structure{CapacityText: "1,5", BrojAgregata: 4}, 4},
		{"upisani broj bez kapaciteta", Structure{BrojAgregata: 2}, 2},
		{"ništa se ne zna", Structure{CapacityText: "oko 2"}, 0},
	} {
		if got := s.st.BrojCrpki(); got != s.zeli {
			t.Errorf("%s: %d, želi %d", s.opis, got, s.zeli)
		}
	}
	if n := (Structure{CapacityText: "1,5", BrojAgregata: 2}).AgregataZaOcitanje(Reading{}); n != 2 {
		t.Errorf("obrazac očitanja ne slijedi upisani broj agregata: %d", n)
	}
	for broj, dobar := range map[int]bool{0: true, 1: true, 20: true, -1: false, 21: false} {
		if err := (Structure{BrojAgregata: broj}).ProvjeriBrojAgregata(); (err == nil) != dobar {
			t.Errorf("broj agregata %d: %v", broj, err)
		}
	}
}
