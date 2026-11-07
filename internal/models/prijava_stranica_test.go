package models

import (
	"strings"
	"testing"
	"time"
)

// Puno mjesto ide na stranicu prijave, a Mjesto() ostaje kakav jest, jer ga
// koristi Naslovna.
func TestMjestoPuno(t *testing.T) {
	for _, c := range []struct {
		ime   string
		p     PrijavaSTerena
		zeljo string
	}{
		{"sve", PrijavaSTerena{Vodotok: "kanal Travnik", Objekt: "nasip Draž", DionicaCode: "B.16.2", Stacionaza: "4+300"}, "kanal Travnik · nasip Draž · B.16.2 · st. 4+300"},
		{"voda i stacionaža", PrijavaSTerena{Vodotok: "kanal Travnik", Stacionaza: "1+850"}, "kanal Travnik · st. 1+850"},
		{"samo objekt", PrijavaSTerena{Objekt: "ustava Bilje"}, "ustava Bilje"},
		{"bez mjesta", PrijavaSTerena{}, ""},
	} {
		if s := c.p.MjestoPuno(); s != c.zeljo {
			t.Errorf("%s: %q, a treba %q", c.ime, s, c.zeljo)
		}
	}
	p := PrijavaSTerena{Vodotok: "kanal Travnik", Objekt: "nasip Draž", DionicaCode: "B.16.2", Stacionaza: "4+300"}
	if s := p.Mjesto(); s != "kanal Travnik, nasip Draž, dionica B.16.2, st. 4+300" {
		t.Errorf("Mjesto() se promijenio: %q", s)
	}
}

// Zapis na stranici drži sve što i Podaci: sekunde, pet decimala i uređaj.
// Slika bez zapisa kaže da ga nema, a nula od vremena nije zapis.
func TestZapisNaStranici(t *testing.T) {
	kad := time.Date(2026, 10, 26, 8, 52, 17, 0, Zagreb)
	nula := time.Time{}
	for _, c := range []struct {
		ime   string
		s     SlikaPrijave
		ima   bool
		zeljo string
	}{
		{"sve", SlikaPrijave{Snimljeno: &kad, Lat: 45.7312, Lon: 18.8741, Uredjaj: "mobitel Primjer X1"}, true, "26. 10. 2026. 08:52:17 · 45,73120; 18,87410 · mobitel Primjer X1"},
		{"samo vrijeme", SlikaPrijave{Snimljeno: &kad}, true, "26. 10. 2026. 08:52:17"},
		{"samo položaj, južno i zapadno", SlikaPrijave{Lat: -1.5, Lon: -2.25}, true, "-1,50000; -2,25000"},
		{"samo uređaj", SlikaPrijave{Uredjaj: "mobitel Primjer X1"}, true, "mobitel Primjer X1"},
		{"bez zapisa", SlikaPrijave{}, false, "Fotoaparat uz sliku nije zapisao vrijeme, položaj ni uređaj."},
		{"nula od vremena", SlikaPrijave{Snimljeno: &nula}, false, "Fotoaparat uz sliku nije zapisao vrijeme, položaj ni uređaj."},
	} {
		if g := c.s.ImaZapisFotoaparata(); g != c.ima {
			t.Errorf("%s: ima zapis %v, a treba %v", c.ime, g, c.ima)
		}
		if s := c.s.ZapisNaStranici(); s != c.zeljo {
			t.Errorf("%s: %q, a treba %q", c.ime, s, c.zeljo)
		}
	}
}

// Skraćen otisak je samo za zaslon: cijeli ostaje u opisu izvorne datoteke.
func TestOtisakKratko(t *testing.T) {
	cijeli := "7f3a" + strings.Repeat("0", 56) + "c91e"
	s := SlikaPrijave{Naziv: "IMG_2041.jpg", Otisak: cijeli, IzvornoBajtova: 2048}
	if g := s.OtisakKratko(); g != "7f3a…c91e" {
		t.Errorf("skraćen otisak: %q", g)
	}
	if !strings.Contains(s.Izvornik(), cijeli) {
		t.Errorf("opis izvorne datoteke bez cijelog otiska: %q", s.Izvornik())
	}
	if g := (SlikaPrijave{}).OtisakKratko(); g != "" {
		t.Errorf("bez otiska: %q", g)
	}
	if g := (SlikaPrijave{Otisak: "abcd"}).OtisakKratko(); g != "abcd" {
		t.Errorf("kratak otisak se ne skraćuje: %q", g)
	}
}
