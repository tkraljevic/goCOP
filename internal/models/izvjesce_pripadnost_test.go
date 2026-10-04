package models

import "testing"

// Izvješće pronađeno zadanim ID-om vrijedi samo u svojoj dionici i sektoru;
// nepronađeno (nil) ne vrijedi nigdje
func TestPripadnostIzvjesca(t *testing.T) {
	var nema *DnevnoIzvjesce
	if nema.IzDionice("P.1.1") || (&DnevnoIzvjesce{SectionCode: "P.1.2"}).IzDionice("P.1.1") || !(&DnevnoIzvjesce{SectionCode: "P.1.1"}).IzDionice("P.1.1") {
		t.Error("dnevno izvješće izvan svoje dionice")
	}
	var nemaS *SektorskoIzvjesce
	if nemaS.IzSektora("P") || (&SektorskoIzvjesce{Sektor: "Q"}).IzSektora("P") || !(&SektorskoIzvjesce{Sektor: "P"}).IzSektora("P") {
		t.Error("izvješće sektora izvan svog sektora")
	}
}

// Vodotok nosi najviši stadij svojih dionica bez obzira na redoslijed;
// istim stadijem tendenciju daje prva dionica koja je ima
func TestVodotokPrimaStadij(t *testing.T) {
	for _, tc := range []struct {
		ime        string
		zatecen    DefensePhase
		tendencija string
		novi       DefensePhase
		prima      bool
	}{
		{"viši stadij", PhasePrep, "raste", PhaseRegular, true},
		{"niži stadij bez tendencije", PhaseRegular, "", PhasePrep, false},
		{"niži stadij s tendencijom", PhaseRegular, "raste", PhasePrep, false},
		{"isti stadij bez tendencije", PhaseRegular, "", PhaseRegular, true},
		{"isti stadij s tendencijom", PhaseRegular, "raste", PhaseRegular, false},
	} {
		v := &VodotokUPregledu{Stadij: tc.zatecen, Tendencija: tc.tendencija}
		if v.PrimaStadij(tc.novi) != tc.prima {
			t.Errorf("%s: prima = %v", tc.ime, !tc.prima)
		}
	}
}
