package models

import (
	"reflect"
	"testing"
)

// Broj i kapaciteti crpki slijede iz zapisa kapaciteta, s točkom ili zarezom
func TestCrpkeIzKapaciteta(t *testing.T) {
	for zapis, zeli := range map[string][]float64{
		"1,75":          {1.75},
		"2x2.5":         {2.5, 2.5},
		"2,5 + 2x1,75":  {2.5, 1.75, 1.75},
		"3 × 0,3":       {0.3, 0.3, 0.3},
		"":              nil,
		"oko 2":         nil,
		"0":             nil,
		"2x":            nil,
		"1 + nepoznato": nil,
	} {
		if got := (Structure{CapacityText: zapis}).Crpke(); !reflect.DeepEqual(got, zeli) {
			t.Errorf("%q: %v, želi %v", zapis, got, zeli)
		}
	}
	if u := (Structure{CapacityText: "2,5 + 2x1,75"}).UkupniKapacitet(); u != 6 {
		t.Errorf("ukupni kapacitet: %v", u)
	}
}

// Okvirni vodostaj pogona je prvi broj; broj u zagradi je iznimka
func TestVodostajPogona(t *testing.T) {
	for zapis, zeli := range map[string]*int{
		"165": intP(165), "76 (90)": intP(76), "200 (220!)": intP(200), "-100": intP(-100), "+90": intP(90),
		"od -20 do 200": nil, "": nil, "oko 90": nil,
	} {
		got := VodostajPogona(zapis)
		if (got == nil) != (zeli == nil) || got != nil && *got != *zeli {
			t.Errorf("%q: %v, želi %v", zapis, got, zeli)
		}
	}
}

func intP(v int) *int { return &v }
