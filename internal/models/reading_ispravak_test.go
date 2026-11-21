package models

import "testing"

// Ispravljeno očitanje kaže što je pokazao uređaj i za koliko je ispravljeno;
// neispravljeno ne kaže ništa.
func TestIspravakOcitanja(t *testing.T) {
	cm, sirovo := 312, 355
	r := Reading{LevelCm: &cm, SirovoCm: &sirovo}
	if r.IspravakCm() != -43 || r.OpisIspravka() != "uređaj 355 cm, ispravljeno -43 cm po ručnom očitanju" {
		t.Errorf("ispravak %d, opis %q", r.IspravakCm(), r.OpisIspravka())
	}
	for _, bez := range []Reading{{LevelCm: &cm}, {SirovoCm: &sirovo}, {LevelCm: &cm, SirovoCm: &cm}} {
		if bez.IspravakCm() != 0 || bez.OpisIspravka() != "" {
			t.Errorf("neispravljeno očitanje: %d, %q", bez.IspravakCm(), bez.OpisIspravka())
		}
	}
	if u := (UpisanoOcitanje{Cm: 312, SirovoCm: &sirovo}); u.Sirovo() != 355 {
		t.Errorf("sirovo ispravljenog: %d", u.Sirovo())
	}
	if u := (UpisanoOcitanje{Cm: 312}); u.Sirovo() != 312 {
		t.Errorf("sirovo neispravljenog: %d", u.Sirovo())
	}
}

// Teren treba kad je ispravak velik; potvrda se čeka kad zadnje ručno
// očitanje odstupa, a ispravak ga ne prati.
func TestStanjeIspravka(t *testing.T) {
	for _, s := range []struct {
		ime         string
		s           StanjeIspravka
		teren, ceka bool
	}{
		{"bez ispravka i bez ručnog", StanjeIspravka{}, false, false},
		{"velik ispravak, u plusu", StanjeIspravka{PomakCm: 12}, true, false},
		{"velik ispravak, potvrđen", StanjeIspravka{PomakCm: -43, ImaRucno: true, RucnoCm: 312, SirovoCm: 355}, true, false},
		{"mali ispravak", StanjeIspravka{PomakCm: -7, ImaRucno: true, RucnoCm: 100, SirovoCm: 107}, false, false},
		{"ručno odstupa, ispravka nema", StanjeIspravka{ImaRucno: true, RucnoCm: 312, SirovoCm: 355}, false, true},
		{"ručno unutar točnosti", StanjeIspravka{ImaRucno: true, RucnoCm: 100, SirovoCm: 104}, false, false},
		{"ručno odstupa od ispravka koji vrijedi", StanjeIspravka{PomakCm: -43, ImaRucno: true, RucnoCm: 335, SirovoCm: 355}, true, true},
	} {
		if s.s.TrebaTeren() != s.teren || s.s.CekaPotvrdu() != s.ceka {
			t.Errorf("%s: teren %v, čeka %v", s.ime, s.s.TrebaTeren(), s.s.CekaPotvrdu())
		}
	}
}
