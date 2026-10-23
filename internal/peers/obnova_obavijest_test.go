package peers

// Obavijesti o roku za e-poštu (G9b, obnova_obavijest.go): računalo koje
// prima vidi članstva i ovlasti koje ističu ili su istekle; svaki
// administrator dobije svaku jednom, i kad je šalju dva računala.

import (
	"strings"
	"testing"
	"time"
)

// izdajOvlastSRokom: osnivač a izda s ovlast koja ističe za rok (0: istekla
// čim je izdana, jer je izdavanje zaokruženo na sekundu) i upiše je kod
// obojice
func izdajOvlastSRokom(t *testing.T, a, s *Service, rok time.Duration) {
	t.Helper()
	o, err := a.network.Ovlasti(s.node.ID, s.javni(), a.node.ID, rok)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []*Service{a, s} {
		if err := x.saveOvlast(ctxRub, o); err != nil {
			t.Fatal(err)
		}
	}
}

// stanjaObavijesti su računalo|vrsta|stanje obavijesti, bez roka
func stanjaObavijesti(obavijesti []ObavijestRoka) map[string]bool {
	out := map[string]bool{}
	for _, o := range obavijesti {
		out[strings.Join(strings.Split(o.Kljuc, "|")[:3], "|")] = true
	}
	return out
}

func TestObavijestiRoka(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b, c, d := rubniCvor(t, "laptop-baranja"), rubniCvor(t, "laptop-vinkovci"), rubniCvor(t, "laptop-ilok")
	p, q := rubniCvor(t, "server-ured"), rubniCvor(t, "server-split")
	uMrezuSRokom(t, a, b, 10*dan)
	uMrezuSRokom(t, a, c, -5*dan)
	primiIzravno(t, a, d)
	primiIzravno(t, a, p)
	primiIzravno(t, a, q)
	izdajOvlastSRokom(t, a, p, 10*dan)
	izdajOvlastSRokom(t, a, q, 0)

	rokovi := a.ObavijestiRoka(ctxRub)
	got := stanjaObavijesti(rokovi)
	for _, want := range []string{"laptop-baranja|clanstvo|istjece", "laptop-vinkovci|clanstvo|pricuva", "server-ured|ovlast|istjece", "server-split|ovlast|isteklo"} {
		if !got[want] {
			t.Errorf("nema obavijesti %s u %v", want, got)
		}
	}
	if len(rokovi) != 4 {
		t.Errorf("obavijesti %v", got)
	}
	predmet, tekst := TekstObavijesti(rokovi, "Kućni server")
	for _, d := range []string{"laptop-baranja: članstvo u mreži ističe", "laptop-vinkovci: članstvo u mreži isteklo je", "Razmjena radi još do",
		"server-ured: ovlast za primanje ističe", "server-split: ovlast za primanje istekla je", "gumb Obnovi uz računalo", "Poruku je poslalo računalo Kućni server."} {
		if !strings.Contains(tekst, d) {
			t.Errorf("tekst nema %q:\n%s", d, tekst)
		}
	}
	if predmet != "goCOP: rok članstva u mreži" {
		t.Errorf("predmet %q", predmet)
	}

	// svaki administrator dobije svaku obavijest jednom
	if n := a.NoveObavijestiRoka(ctxRub, "ana", rokovi); len(n) != 4 {
		t.Fatalf("nove za Anu %d", len(n))
	}
	a.ZabiljeziObavijestiRoka(ctxRub, "ana", rokovi)
	if n := a.NoveObavijestiRoka(ctxRub, "ana", rokovi); len(n) != 0 {
		t.Errorf("Ana bi ih dobila opet: %v", stanjaObavijesti(n))
	}
	if n := a.NoveObavijestiRoka(ctxRub, "branko", rokovi); len(n) != 4 {
		t.Errorf("Branko nije dobio nijednu, a ima %d novih", len(n))
	}

	// zapis ide knjigom: drugo računalo koje prima ne šalje Ani iste
	razgovaraj(t, a, p)
	if n := p.NoveObavijestiRoka(ctxRub, "ana", p.ObavijestiRoka(ctxRub)); len(n) != 0 {
		t.Errorf("server-ured bi Ani poslao opet: %v", stanjaObavijesti(n))
	}

	// obnovljeno članstvo s novim rokom u istom stanju je nova obavijest:
	// rok je dio ključa
	uMrezuSRokom(t, a, b, 9*dan)
	nove := a.NoveObavijestiRoka(ctxRub, "ana", a.ObavijestiRoka(ctxRub))
	if len(nove) != 1 || !stanjaObavijesti(nove)["laptop-baranja|clanstvo|istjece"] {
		t.Errorf("novi rok u istom stanju: %v", stanjaObavijesti(nove))
	}
	a.ZabiljeziObavijestiRoka(ctxRub, "ana", nove)

	// članstvo koje uđe u pričuvu je nova obavijest
	uMrezuSRokom(t, a, b, -1*dan)
	if n := a.NoveObavijestiRoka(ctxRub, "ana", a.ObavijestiRoka(ctxRub)); !stanjaObavijesti(n)["laptop-baranja|clanstvo|pricuva"] || len(n) != 1 {
		t.Errorf("pričuva nakon isteka: %v", stanjaObavijesti(n))
	}

	// računalo kojem je ovlast istekla i dalje šalje, i o sebi
	if !stanjaObavijesti(q.ObavijestiRoka(ctxRub))["server-split|ovlast|isteklo"] {
		t.Errorf("računalo s isteklom ovlasti ne javlja: %v", stanjaObavijesti(q.ObavijestiRoka(ctxRub)))
	}
	if d.ObavijestiRoka(ctxRub) != nil || rubniCvor(t, "samotnjak").ObavijestiRoka(ctxRub) != nil {
		t.Error("računalo koje ne prima šalje obavijesti")
	}
}

// Računalo s ključem mreže ne javlja svoju ovlast: prima i bez nje, a
// ovlast koja ističe samo obnovi.
func TestVlastitaOvlastRacunalaSKljucem(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	o, err := a.network.Ovlasti(a.node.ID, a.javni(), a.node.ID, 10*dan)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveOvlast(ctxRub, o); err != nil {
		t.Fatal(err)
	}
	if stanjaObavijesti(a.rokoviOvlasti(ctxRub, true))["cop-osijek|ovlast|istjece"] || !stanjaObavijesti(a.rokoviOvlasti(ctxRub, false))["cop-osijek|ovlast|istjece"] {
		t.Error("vlastita ovlast računala s ključem javlja se")
	}
	a.mozdaZatraziObnovu(ctxRub)
	if n := verzijaZapisa(t, a, EntityOvlasti, a.node.ID); n != 2 {
		t.Errorf("vlastita ovlast nije obnovljena: %d verzija", n)
	}
	a.obnoviVlastituOvlast(ctxRub, false) // svježa: ništa
	if n := verzijaZapisa(t, a, EntityOvlasti, a.node.ID); n != 2 {
		t.Errorf("svježa ovlast obnovljena: %d verzija", n)
	}
}

// Kad se zapis ne da upisati ni pročitati, obavijest se ne šalje svaki sat,
// a greška ide u dnevnik.
func TestObavijestiRokaKadBazaNeRadi(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-baranja")
	uMrezuSRokom(t, a, b, 10*dan)
	rokovi := a.ObavijestiRoka(ctxRub)
	kvarUpisa(t, a, "INSERT", "record_versions", "NEW.entity = '"+EntityObavijestiRoka+"'")
	a.ZabiljeziObavijestiRoka(ctxRub, "ana", rokovi)
	if !strings.Contains(dnevnik.String(), "poslana obavijest o roku nije zapamćena") {
		t.Errorf("dnevnik bez greške:\n%s", dnevnik)
	}
	if _, err := a.db.Exec(`ALTER TABLE record_versions RENAME TO knjiga_skrivena`); err != nil {
		t.Fatal(err)
	}
	if !a.poslana(ctxRub, rokovi[0].Kljuc, "ana") {
		t.Error("nečitljiva knjiga: obavijest bi išla svaki sat")
	}
	pokvariTablicu(t, a, "ovlasti")
	pokvariTablicu(t, a, "memberships")
	if a.rokoviClanstava(ctxRub) != nil || a.rokoviOvlasti(ctxRub, true) != nil {
		t.Error("rokovi bez tablica")
	}
}
