package models

import (
	"testing"
	"time"
)

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

// Odluke (8. 10. 2026.), pitanje 10: opis rada bira uprava centra kad se ne
// zna mjesto rada, i kad ga je već ispravila (u napomeni je trag ispravka)
func TestOpisBiraUprava(t *testing.T) {
	for _, s := range []struct {
		ime  string
		d    Dezurstvo
		want bool
	}{
		{"opis s popisa", Dezurstvo{Opis: "Dežurstvo u COP-u", Mjesto: MjestoUred, Napomena: "noćna smjena"}, false},
		{"mjesto nepoznato", Dezurstvo{Opis: "Opis s drugog čvora"}, true},
		{"opis ispravila uprava", Dezurstvo{Opis: "Dežurstvo u COP-u", Mjesto: MjestoUred,
			Napomena: "noćna [opis ispravio Ana Anić 8.10.2026. 14:20; prije: „Opis s drugog čvora”]"}, true},
		{"samo slično tragu", Dezurstvo{Opis: "Dežurstvo u COP-u", Mjesto: MjestoUred, Napomena: "[opis ispravio sam]"}, false},
	} {
		if got := s.d.OpisBiraUprava(); got != s.want {
			t.Errorf("%s: %v, a treba %v", s.ime, got, s.want)
		}
	}
	napomena := "a [opis ispravio Ana Anić 8.10.2026. 14:20; prije: „—”] b [opis ispravio Ivo Ivić 9.10.2026. 08:05; prije: „Dežurstvo u COP-u”]"
	if got := TragoviIspravkaOpisa(napomena); len(got) != 2 || got[1] != "[opis ispravio Ivo Ivić 9.10.2026. 08:05; prije: „Dežurstvo u COP-u”]" {
		t.Errorf("tragovi: %q", got)
	}
	if got := TragoviIspravkaOpisa("noćna smjena"); len(got) != 0 {
		t.Errorf("bez traga: %q", got)
	}
}

// Odluke (8. 10. 2026.), pitanje 10: plan osobe na profilu zna za dežurstva
// kojima se ne zna mjesto rada. Takvo je „za provjeru” i kad je potvrđeno
// (stiglo potvrđeno s drugog čvora ili potvrđeno prije nadogradnje), jer ne
// ulazi u obračun; nepotvrđeno ne „čeka potvrdu”, jer se ne potvrđuje dok
// uprava centra ne izabere opis. Prvi i zadnji dan ne ovise o redoslijedu.
func TestPlanOsobeDodaj(t *testing.T) {
	sat := func(dan, h int) time.Time { return time.Date(2026, 9, dan, h, 0, 0, 0, Zagreb) }
	potvrdeno := sat(20, 8)
	var p PlanOsobe
	for _, d := range []Dezurstvo{
		{Od: sat(9, 8), Do: sat(9, 12), Opis: "Dežurstvo u COP-u", Mjesto: MjestoUred, PotvrdenoAt: &potvrdeno},
		{Od: sat(7, 22), Do: sat(8, 6), Opis: "Opis s drugog čvora", PotvrdenoAt: &potvrdeno},
		{Od: sat(12, 8), Do: sat(12, 10), Opis: "Opis s drugog čvora"},
		{Od: sat(10, 8), Do: sat(10, 9), Opis: "Obilazak i pregled obrambenih objekata"},
	} {
		p.Dodaj(d)
	}
	if p.Dezurstava != 4 || p.Sati != 15*time.Hour || p.ZaProvjeru != 2 || p.CekaPotvrdu != 1 {
		t.Errorf("plan: %d dežurstava, %v, za provjeru %d, čeka potvrdu %d", p.Dezurstava, p.Sati, p.ZaProvjeru, p.CekaPotvrdu)
	}
	if !p.Prvo.Equal(sat(7, 22)) || !p.Zadnje.Equal(sat(12, 10)) {
		t.Errorf("plan od %v do %v", p.Prvo, p.Zadnje)
	}
}
