package models

import "testing"

// Odluke (8. 10. 2026.), pitanje 10: mjesto rada je pohranjeno mjesto kad je
// ured ili teren; inače se izvodi iz opisa s popisa, a kad ni to ne ide,
// dežurstvo je „za provjeru” — ništa se ne pretpostavlja, ni ured.
func TestMjestoRadaDezurstva(t *testing.T) {
	for _, s := range []struct {
		ime           string
		d             Dezurstvo
		mjesto, tekst string
		zaProvjeru    bool
	}{
		{"pohranjen teren", Dezurstvo{Opis: "Dežurstvo u COP-u", Mjesto: MjestoTeren}, MjestoTeren, "teren", false},
		{"pohranjen ured", Dezurstvo{Opis: "Opis s drugog čvora", Mjesto: MjestoUred}, MjestoUred, "ured", false},
		{"prazno, opis s popisa", Dezurstvo{Opis: "Obilazak i pregled obrambenih objekata"}, MjestoTeren, "teren", false},
		{"nepoznato, opis s popisa", Dezurstvo{Opis: "Administrativni rad pri provedbi obrane od poplava", Mjesto: "NEPOZNATO"}, MjestoUred, "ured", false},
		{"malim slovima nije poznato", Dezurstvo{Opis: "Opis s drugog čvora", Mjesto: "teren"}, "", OznakaZaProvjeru, true},
		{"ni mjesto ni opis", Dezurstvo{Opis: "Opis s drugog čvora"}, "", OznakaZaProvjeru, true},
	} {
		if m := s.d.MjestoRada(); m != s.mjesto {
			t.Errorf("%s: MjestoRada = %q, a mora biti %q", s.ime, m, s.mjesto)
		}
		if s.d.ZaProvjeru() != s.zaProvjeru || s.d.MjestoTekst() != s.tekst {
			t.Errorf("%s: ZaProvjeru = %v, MjestoTekst = %q", s.ime, s.d.ZaProvjeru(), s.d.MjestoTekst())
		}
	}
	if NazivMjesta("") != "mjesto nepoznato — za provjeru" {
		t.Errorf("oznaka: %q", NazivMjesta(""))
	}
}
