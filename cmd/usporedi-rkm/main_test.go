package main

import (
	"math"
	"testing"
)

func TestProcitajRKM(t *testing.T) {
	for zapis, want := range map[string]float64{
		"rkm 19,10":    19.10,
		"rkm 1.380,30": 1380.30,
		"rkm 1424+850": 1424.850,
		"rkm 242,00":   242,
	} {
		got, err := procitajRKM(zapis)
		if err != nil {
			t.Errorf("procitajRKM(%q): %v", zapis, err)
		} else if math.Abs(got-want) > 0.0001 {
			t.Errorf("procitajRKM(%q) = %.3f, očekivano %.3f", zapis, got, want)
		}
	}
}

func TestProcitajRKMNePrihvacaDruguStacionazu(t *testing.T) {
	if _, err := procitajRKM("nkm 19,55"); err == nil {
		t.Fatal("nkm ne smije proći kao rkm")
	}
}

func TestMedijan(t *testing.T) {
	if got := medijan([]float64{1, 2, 9}); got != 2 {
		t.Errorf("medijan neparnog niza = %v", got)
	}
	if got := medijan([]float64{1, 3}); got != 2 {
		t.Errorf("medijan parnog niza = %v", got)
	}
}
