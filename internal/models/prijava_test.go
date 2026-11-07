package models

import "testing"

// Kratko mjesto ide na karticu popisa: objekt ima prednost pred vodom, a
// postojeći Mjesto() ostaje kakav jest, jer ga koristi Naslovna.
func TestMjestoKratko(t *testing.T) {
	for _, c := range []struct {
		ime   string
		p     PrijavaSTerena
		zeljo string
	}{
		{"objekt, dionica i stacionaža", PrijavaSTerena{Vodotok: "kanal Travnik", Objekt: "nasip Draž", DionicaCode: "B.16.2", Stacionaza: "4+300"}, "nasip Draž · B.16.2 · st. 4+300"},
		{"voda i dionica", PrijavaSTerena{Vodotok: "potok Karašica", DionicaCode: "B.16.3"}, "potok Karašica · B.16.3"},
		{"voda i stacionaža", PrijavaSTerena{Vodotok: "kanal Travnik", Stacionaza: "1+850"}, "kanal Travnik · st. 1+850"},
		{"samo objekt", PrijavaSTerena{Objekt: "ustava Bilje"}, "ustava Bilje"},
		{"samo dionica", PrijavaSTerena{DionicaCode: "B.16.1"}, "B.16.1"},
		{"bez mjesta", PrijavaSTerena{}, ""},
	} {
		if s := c.p.MjestoKratko(); s != c.zeljo {
			t.Errorf("%s: %q, a treba %q", c.ime, s, c.zeljo)
		}
	}
	p := PrijavaSTerena{Vodotok: "kanal Travnik", Objekt: "nasip Draž", DionicaCode: "B.16.2", Stacionaza: "4+300"}
	if s := p.Mjesto(); s != "kanal Travnik, nasip Draž, dionica B.16.2, st. 4+300" {
		t.Errorf("Mjesto() se promijenio: %q", s)
	}
}
