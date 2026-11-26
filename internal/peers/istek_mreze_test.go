package peers

// Rokovi članstva u pravoj razmjeni: svaki čvor sluša na loopbacku
// (naPetlji), a razmjena ide kroz SyncWith i vrata druge strane (primiVezu,
// trusted), ne kroz razgovaraj koji na vratima pušta samo ključ pozivatelja.
// Sat se ne pomiče: istek se postiže članstvima i ovlastima s rokom u
// prošlosti. Nijedan rok ne smije tiho raspasti mrežu (Odluke (18. 10.
// 2026.)); ono što ovdje prestaje raditi prestaje namjerno i uz upozorenje.

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// mrezaNaPetlji: čvorovi slušaju razmjene na loopbacku, svaki zna adresu
// svakog drugog, primljeno ide na površinu, a svako članstvo iz clanstva
// upisano je kod svih
func mrezaNaPetlji(t *testing.T, cvorovi []*Service, clanstva ...razmjena.Membership) {
	t.Helper()
	for _, s := range cvorovi {
		sPovrsinom(s)
		naPetlji(t, s)
	}
	for _, a := range cvorovi {
		for _, b := range cvorovi {
			if a != b {
				upoznaj(t, a, b, adresaNaPetlji(b))
			}
		}
		for _, m := range clanstva {
			if err := a.saveMembership(ctxRub, m); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// razmijeni: a nazove b, pa vrata od b odluče hoće li razmjena ići
func razmijeni(a, b *Service) error {
	_, _, err := a.SyncWith(ctxRub, b.node.ID)
	return err
}

// moraRazmijeniti: razmjena a → b mora proći
func moraRazmijeniti(t *testing.T, a, b *Service) {
	t.Helper()
	if err := razmijeni(a, b); err != nil {
		t.Fatalf("razmjena %s → %s: %v", a.node.ID, b.node.ID, err)
	}
}

// odbijenNaVratima javlja ima li s u zapisu odbijenog današnji redak vrata
// s razlogom nije-clan za računalo id
func odbijenNaVratima(s *Service, id string) bool {
	zapis, err := s.Odbijeno(ctxRub, 1)
	return err == nil && slices.ContainsFunc(zapis, func(z ZapisOdbijenog) bool {
		return z.Vrsta == VrstaVrata && z.Razlog == string(razmjena.VrataNijeClan) && z.Racunalo == id
	})
}

// clanstvoKod je članstvo računala id koje vrijedi kod s
func clanstvoKod(t *testing.T, s *Service, id string) razmjena.Membership {
	t.Helper()
	m, err := s.izabranoClanstvo(ctxRub, id)
	if err != nil {
		t.Fatalf("%s ne zna valjano članstvo od %s: %v", s.node.ID, id, err)
	}
	return *m
}

// rokClanstva je rok članstva računala id koje vrijedi kod s; nula kad
// nijedno ne vrijedi ni u pričuvi
func rokClanstva(s *Service, id string) time.Time {
	m, err := s.izabranoClanstvo(ctxRub, id)
	if err != nil {
		return time.Time{}
	}
	return m.ExpiresAt
}

// Član u pričuvi razmjenjuje u oba smjera i preko posrednika dobije
// obnovu od primatelja: zahtjev ide član → posrednik → primatelj, novo
// članstvo istim putem natrag. Računalo s ključem mreže u tome ne
// sudjeluje (da sudjeluje, obnovilo bi ga samo). Posrednik je stariji
// program, koji zna samo osnovni razgovor.
func TestClanUPricuviRazmjenjujeIObnoviSe(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p, h, c := rubniCvor(t, "ured-vukovar"), rubniCvor(t, "server-kuca"), rubniCvor(t, "laptop-baranja")
	h.mogucnosti = func() []string { return []string{razmjena.OznakaRazmjena} }
	primateljUMrezi(t, a, p)
	mp := clanstvoKod(t, a, p.node.ID)
	mh := primiIzravno(t, a, h)
	mc := uMrezuSRokom(t, a, c, -5*dan)
	mrezaNaPetlji(t, []*Service{p, h, c}, mp, mh, mc)

	moraRazmijeniti(t, c, h)
	moraRazmijeniti(t, h, c)
	// nakon uspješne razmjene član u pričuvi sam upiše zahtjev za obnovu
	cekajDa(t, "zahtjev za obnovu", func() bool { return verzijaZapisa(t, c, EntityZahtjeviObnove, c.node.ID) == 1 })
	moraRazmijeniti(t, c, h) // zahtjev ide posredniku
	if n := verzijaZapisa(t, h, EntityZahtjeviObnove, c.node.ID); n != 1 {
		t.Fatalf("posrednik ima %d zahtjeva", n)
	}
	moraRazmijeniti(t, h, p) // pa primatelju, koji ga obradi nakon razmjene
	cekajDa(t, "obnova kod primatelja", func() bool { return rokClanstva(p, c.node.ID).After(time.Now().Add(300 * dan)) })
	moraRazmijeniti(t, p, h)
	moraRazmijeniti(t, h, c)
	novo, err := c.izabranoClanstvo(ctxRub, c.node.ID)
	if err != nil || novo.IssuedBy != p.node.ID || novo.ExpiresAt.Before(time.Now().Add(300*dan)) {
		t.Fatalf("obnova nije stigla do člana: %+v %v", novo, err)
	}
	if r, _ := c.VlastitiRokovi(ctxRub); r == nil || r.Stanje != RokVrijedi {
		t.Errorf("rok člana nakon obnove: %+v", r)
	}
	moraRazmijeniti(t, c, h)
	moraRazmijeniti(t, c, p)
}

// Član kojem je prošla i pričuva ne prolazi vrata: drugo računalo to
// zapiše (vrata, nije-clan, s njegovim imenom), a samo ga ne zove. Obnova
// ga više ne vraća, ni gumbom kod primatelja ni zahtjevom, jer zahtjev ne
// prolazi vrata; vraća ga samo novo primanje. Tako je namjerno.
func TestClanNakonPricuveSamoNovimPrimanjem(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p, h, c := rubniCvor(t, "ured-vukovar"), rubniCvor(t, "server-kuca"), rubniCvor(t, "laptop-ilok")
	primateljUMrezi(t, a, p)
	mp := clanstvoKod(t, a, p.node.ID)
	mh := primiIzravno(t, a, h)
	mc := uMrezuSRokom(t, a, c, -31*dan)
	mrezaNaPetlji(t, []*Service{p, h, c}, mp, mh, mc)

	if err := razmijeni(c, p); err == nil {
		t.Fatal("član nakon pričuve prošao je vrata primatelja")
	}
	cekajDa(t, "zapis odbijenog kod primatelja", func() bool { return odbijenNaVratima(p, c.node.ID) })
	if err := razmijeni(c, h); err == nil {
		t.Fatal("član nakon pričuve prošao je vrata posrednika")
	}
	cekajDa(t, "zapis odbijenog kod posrednika", func() bool { return odbijenNaVratima(h, c.node.ID) })
	if err := razmijeni(p, c); err == nil || !strings.Contains(err.Error(), "nije član naše mreže") {
		t.Errorf("primatelj zove člana nakon pričuve: %v", err)
	}
	moraRazmijeniti(t, h, p) // ostali i dalje razmjenjuju

	if _, err := p.ObnoviClana(ctxRub, c.node.ID); err == nil || !strings.Contains(err.Error(), "treba ponovno primiti") {
		t.Errorf("gumb Obnovi nakon pričuve: %v", err)
	}
	if _, err := c.ZatraziObnovu(ctxRub); err != nil {
		t.Fatal(err)
	}
	if err := razmijeni(c, h); err == nil {
		t.Fatal("zahtjev za obnovu prošao je vrata")
	}
	if n := verzijaZapisa(t, h, EntityZahtjeviObnove, c.node.ID); n != 0 {
		t.Errorf("zahtjev je stigao posredniku: %d", n)
	}

	novo, err := p.izdajClanstvo(ctxRub, c.node.ID, c.javni()) // novo primanje
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Service{p, c} {
		if err := s.saveMembership(ctxRub, novo); err != nil {
			t.Fatal(err)
		}
	}
	moraRazmijeniti(t, c, p)
	moraRazmijeniti(t, c, h) // h članstvo zna iz razmjene s p, ili ga vidi u certifikatu
}

// primateljBezObnove: primatelj p s članstvom do clanstvoDo i ovlasti do
// ovlastDo, oboje potpisano ključem mreže od a i oboje u punom trajanju
// (članstvo 365 dana, ovlast 730). Računalo s ključem mreže a ne sluša, pa
// nitko primatelju ne obnavlja ni članstvo ni ovlast.
func primateljBezObnove(t *testing.T, a *Service, id string, clanstvoDo, ovlastDo time.Time) (*Service, razmjena.Ovlast, razmjena.Membership) {
	t.Helper()
	p := rubniCvor(t, id)
	mp := potpisanoClanstvo(t, a.network.Private(), p.node.ID, p.javni(), clanstvoDo.Add(-MembershipValidity), clanstvoDo)
	uMrezuS(t, a, p, mp)
	o := potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), ovlastDo.Add(-OvlastValidity), ovlastDo)
	for _, s := range []*Service{a, p} {
		if err := s.saveOvlast(ctxRub, o); err != nil {
			t.Fatal(err)
		}
	}
	return p, o, mp
}

// imaObavijest javlja ima li među obavijestima za e-poštu ona o računalu
// id te vrste i stanja
func imaObavijest(obavijesti []ObavijestRoka, id, vrsta, stanje string) bool {
	return slices.ContainsFunc(obavijesti, func(o ObavijestRoka) bool {
		return strings.HasPrefix(o.Kljuc, id+"|") && o.Vrsta == vrsta && o.Rok.Stanje == stanje
	})
}

// Primatelju nitko ne obnavlja rokove. Članstvo mu ističe prije ovlasti;
// prije toga to piše na pločici i ide e-poštom, za članstvo i za ovlast.
func TestPrimateljBezObnoveUpozorava(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	sada := time.Now()
	p, _, _ := primateljBezObnove(t, a, "ured-vukovar", sada.Add(25*dan), sada.Add(25*dan+MembershipValidity))
	if u := strings.Join(p.upozorenjaRokova(ctxRub, nil), "\n"); !strings.Contains(u, "Članstvo ovog računala u mreži ističe") || strings.Contains(u, "Ovlast ovog računala") {
		t.Errorf("pločica dok članstvo ističe:\n%s", u)
	}
	if o := p.ObavijestiRoka(ctxRub); !imaObavijest(o, p.node.ID, VrstaObavijestiClanstvo, RokIstjece) {
		t.Errorf("e-pošta dok članstvo ističe: %+v", o)
	}
	if _, err := p.izdajClanstvo(ctxRub, "laptop-x", rubniCvor(t, "laptop-x").javni()); err != nil {
		t.Errorf("primatelj kojem članstvo tek ističe ne prima: %v", err)
	}

	q, _, _ := primateljBezObnove(t, a, "ured-vinkovci", sada.Add(-5*dan), sada.Add(20*dan))
	u := strings.Join(q.upozorenjaRokova(ctxRub, nil), "\n")
	if !strings.Contains(u, "Razmjena radi još do") || !strings.Contains(u, "Ovlast ovog računala za primanje ističe") {
		t.Errorf("pločica u pričuvi, uz ovlast koja ističe:\n%s", u)
	}
	o := q.ObavijestiRoka(ctxRub)
	if !imaObavijest(o, q.node.ID, VrstaObavijestiClanstvo, RokPricuva) || !imaObavijest(o, q.node.ID, VrstaObavijestiOvlast, RokIstjece) {
		t.Errorf("e-pošta u pričuvi, uz ovlast koja ističe: %+v", o)
	}

	r, _, _ := primateljBezObnove(t, a, "ured-zupanja", sada.Add(-5*dan), sada.Add(-dan))
	if u := strings.Join(r.upozorenjaRokova(ctxRub, nil), "\n"); !strings.Contains(u, "Ovlast ovog računala za primanje istekla je") {
		t.Errorf("pločica s isteklom ovlasti:\n%s", u)
	}
	if o := r.ObavijestiRoka(ctxRub); !imaObavijest(o, r.node.ID, VrstaObavijestiOvlast, RokIsteklo) {
		t.Errorf("e-pošta s isteklom ovlasti: %+v", o)
	}
}

// Primatelj u pričuvi više nikoga ne obnavlja, ni na zahtjev koji stigne
// razmjenom. Članovi koje je primio ne istječu s njim, nego svaki svojim
// danom: onaj u roku i onaj u pričuvi razmjenjuju, a onoga kojem je prošla
// pričuva vrata odbiju. Primatelj u pričuvi i sam razmjenjuje.
func TestPrimateljUPricuviClanoviIstjecuSvojimDanom(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	sada := time.Now()
	p, o, mp := primateljBezObnove(t, a, "ured-vukovar", sada.Add(-5*dan), sada.Add(320*dan))
	d := rubniCvor(t, "server-kuca")
	md := primiIzravno(t, a, d)
	uRoku, uPricuvi, isteklo := rubniCvor(t, "laptop-baranja"), rubniCvor(t, "laptop-vinkovci"), rubniCvor(t, "laptop-ilok")
	clanstva := []razmjena.Membership{mp, md}
	for _, x := range []struct {
		s  *Service
		od time.Time
	}{{uRoku, sada.Add(-300 * dan)}, {uPricuvi, sada.Add(-370 * dan)}, {isteklo, sada.Add(-400 * dan)}} {
		m := clanstvoPrimateljaOd(t, p, o, x.s.node.ID, x.s.javni(), x.od)
		uMrezuS(t, p, x.s, m)
		clanstva = append(clanstva, m)
	}
	mrezaNaPetlji(t, []*Service{p, d, uRoku, uPricuvi, isteklo}, clanstva...)

	moraRazmijeniti(t, p, d)
	moraRazmijeniti(t, d, p)
	moraRazmijeniti(t, uRoku, d)
	moraRazmijeniti(t, uPricuvi, d)
	moraRazmijeniti(t, d, uPricuvi)
	if err := razmijeni(isteklo, d); err == nil {
		t.Fatal("član kojem je prošla pričuva prošao je vrata")
	}
	cekajDa(t, "zapis odbijenog", func() bool { return odbijenNaVratima(d, isteklo.node.ID) })

	// član u pričuvi traži obnovu, zahtjev stigne primatelju, a on ga ne obnovi
	prije := rokClanstva(p, uPricuvi.node.ID)
	cekajDa(t, "zahtjev za obnovu", func() bool { return verzijaZapisa(t, uPricuvi, EntityZahtjeviObnove, uPricuvi.node.ID) == 1 })
	moraRazmijeniti(t, uPricuvi, p)
	p.obradiZahtjeveObnove(ctxRub)
	if !rokClanstva(p, uPricuvi.node.ID).Equal(prije) {
		t.Error("primatelj u pričuvi obnovio je člana na zahtjev")
	}
	if _, err := p.ObnoviClana(ctxRub, uRoku.node.ID); err == nil || !strings.Contains(err.Error(), "isteklo") {
		t.Errorf("gumb Obnovi kod primatelja u pričuvi: %v", err)
	}
}

// Članstvo koje primatelj izda ne traje dulje od njegove ovlasti, i s
// takvim kratkim članstvom novi član razmjenjuje.
func TestClanstvoPrimateljaNeTrajeDuljeOdOvlasti(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	sada := time.Now()
	p, o, mp := primateljBezObnove(t, a, "ured-vukovar", sada.Add(200*dan), sada.Add(10*dan))
	d, x := rubniCvor(t, "server-kuca"), rubniCvor(t, "laptop-baranja")
	md := primiIzravno(t, a, d)
	mx, err := p.izdajClanstvo(ctxRub, x.node.ID, x.javni())
	if err != nil {
		t.Fatal(err)
	}
	if !mx.ExpiresAt.Equal(o.ExpiresAt) {
		t.Errorf("članstvo vrijedi do %v, a ovlast primatelja do %v", mx.ExpiresAt, o.ExpiresAt)
	}
	uMrezuS(t, p, x, mx)
	mrezaNaPetlji(t, []*Service{p, d, x}, mp, md, mx)
	moraRazmijeniti(t, x, d)
	moraRazmijeniti(t, d, x)
}

// Računalo s ključem mreže kojem je prošla i pričuva samo sebe više ne
// obnavlja (trebaObnova), ali i dalje prima druge ključem mreže, a gumb
// Obnovi članstvo ga vrati. Do tada ga ostali ne zovu i ne puštaju kroz
// vrata; nakon gumba ga puštaju, jer novo članstvo nosi u certifikatu.
func TestRacunaloSKljucemNakonPricuve(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "server-kuca")
	mb := primiIzravno(t, a, b)
	for _, upit := range []string{`DELETE FROM memberships WHERE node_id = ?`, `DELETE FROM record_versions WHERE entity = '` + EntityMemberships + `' AND entity_id = ?`} {
		if _, err := a.db.Exec(upit, a.node.ID); err != nil {
			t.Fatal(err)
		}
	}
	sada := time.Now()
	staro := potpisanoClanstvo(t, a.network.Private(), a.node.ID, a.javni(), sada.Add(-400*dan), sada.Add(-35*dan))
	mrezaNaPetlji(t, []*Service{a, b}, staro, mb)
	if r, _ := a.VlastitiRokovi(ctxRub); r == nil || r.Stanje != RokIsteklo {
		t.Fatalf("rok računala s ključem: %+v", r)
	}
	if a.trebaObnova(ctxRub, true) {
		t.Error("automatska obnova ide i nakon pričuve")
	}
	if err := razmijeni(b, a); err == nil {
		t.Error("član zove računalo s ključem kojem je prošla pričuva")
	}
	if err := razmijeni(a, b); err == nil {
		t.Error("računalo s ključem nakon pričuve prošlo je vrata")
	}
	if _, err := a.izdajClanstvo(ctxRub, "laptop-x", rubniCvor(t, "laptop-x").javni()); err != nil {
		t.Errorf("računalo s ključem nakon pričuve ne prima: %v", err)
	}

	novo, err := a.ZatraziObnovu(ctxRub) // gumb Obnovi članstvo
	if err != nil || novo == nil || novo.ExpiresAt.Before(sada.Add(MembershipValidity)) {
		t.Fatalf("gumb Obnovi članstvo: %+v %v", novo, err)
	}
	moraRazmijeniti(t, a, b)
	moraRazmijeniti(t, b, a)
	if _, err := b.izabranoClanstvo(ctxRub, a.node.ID); errors.Is(err, errNijeClan) {
		t.Error("novo članstvo računala s ključem nije stiglo drugom računalu")
	}
}
