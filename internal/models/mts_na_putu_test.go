package models

import "testing"

// Redak slanja ide na put samo kad skida i ima odredište; stari potezi bez
// odredišta i redci s plusom nisu na putu.
func TestPrometNaPutu(t *testing.T) {
	for _, k := range []struct {
		p      Promet
		naPutu bool
	}{
		{Promet{Kolicina: -5, PremaSkladisteID: "sava"}, true},
		{Promet{Kolicina: -5, PremaAreaID: 34}, true},
		{Promet{Kolicina: -5}, false},
		{Promet{Kolicina: 5, PremaSkladisteID: "sava"}, false},
	} {
		if k.p.IdeNaPut() != k.naPutu {
			t.Errorf("%+v: na put %v", k.p, !k.naPutu)
		}
	}
	sl := Promet{Kolicina: -400, PremaAreaID: 34, PremaSectionCode: "B.34.1", PremaStructureID: "nasip", PremaMjesto: "kod ustave"}
	if m := sl.PremaTerenu(); m.Kljuc() != "34|B.34.1|nasip|kod ustave" {
		t.Errorf("odredište na terenu: %+v", m)
	}
	if n := (Posiljka{Slanje: sl, Zaprimljeno: 380}).NaPutu(); n != 20 {
		t.Errorf("na putu: %v", n)
	}
}

// Odredište slanja za prikaz: ime skladišta ili mjesto na terenu
func TestPremaNaziv(t *testing.T) {
	for _, k := range []struct {
		p    Promet
		want string
	}{
		{Promet{PremaSkladisteID: "sava", PremaSkladisteNaziv: "Skladište Sava"}, "Skladište Sava"},
		{Promet{PremaSkladisteID: "sava"}, "skladište"},
		{Promet{PremaAreaID: 34, PremaAreaName: "Drava i Dunav", PremaSectionCode: "B.34.1", PremaMjesto: "kod ustave"}, "teren · BP 34 Drava i Dunav · B.34.1 · kod ustave"},
	} {
		if got := k.p.PremaNaziv(); got != k.want {
			t.Errorf("%+v: %q, očekivano %q", k.p, got, k.want)
		}
	}
}
