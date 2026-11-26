package peers

// Opoziv člana i obnova ili primanje koje stigne poslije: primatelj koji
// opoziv još nije primio izda opozvanom članu novo članstvo s kasnijim
// trenutkom. Opoziv ključem mreže ili od primatelja koji je članstvo izdao
// pokriva svako članstvo tog ključa koje taj primatelj potpiše, bez obzira
// na trenutke (Odluke (26. 11. 2026.)), pa opozvani član, kad opoziv
// stigne, ne prolazi nijedna vrata. Uz svaki put ide
// kontrola da drugi član istog primatelja i dalje razmjenjuje sa svima.

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// scenaOpoziva: a drži ključ mreže, p prima s ovlašću, c je član kojeg je
// a primio prije 200 dana (obnova mu produlji rok), d je član kojeg je
// primio p i koji ima stariji program (zna samo osnovni razgovor). Svi
// slušaju na loopbacku.
type scenaOpoziva struct {
	a, p, c, d *Service
	mc         razmjena.Membership
}

func novaScenaOpoziva(t *testing.T) scenaOpoziva {
	t.Helper()
	a := rubnaMreza(t, "cop-osijek")
	p, c, d := rubniCvor(t, "ured-vukovar"), rubniCvor(t, "laptop-baranja"), rubniCvor(t, "server-kuca")
	d.mogucnosti = func() []string { return []string{razmjena.OznakaRazmjena} }
	primateljUMrezi(t, a, p)
	mp := clanstvoKod(t, a, p.node.ID)
	md, err := p.izdajClanstvo(ctxRub, d.node.ID, d.javni())
	if err != nil {
		t.Fatal(err)
	}
	uMrezuS(t, p, d, md)
	sada := time.Now()
	mc := potpisanoClanstvo(t, a.network.Private(), c.node.ID, c.javni(), sada.Add(-200*dan), sada.Add(165*dan))
	uMrezuS(t, a, c, mc)
	mrezaNaPetlji(t, []*Service{a, p, c, d}, clanstvoKod(t, a, a.node.ID), mp, md, mc)
	return scenaOpoziva{a: a, p: p, c: c, d: d, mc: mc}
}

// opozovi: a opozove c-a s trenutkom 10 s unatrag, da je ono što p poslije
// izda barem sekundu iza opoziva; opoziv ostaje samo kod a
func (sc scenaOpoziva) opozovi(t *testing.T, opozivatelj *Service) {
	t.Helper()
	opozivatelj.satOpoziva = func() time.Time { return time.Now().Add(-10 * time.Second) }
	if _, err := opozivatelj.RevokeMembership(ctxRub, sc.c.node.ID); err != nil {
		t.Fatal(err)
	}
}

// opozivStigne: opoziv ide od a do p preko d (stariji posrednik), a ono
// što je p izdao natrag do a i d
func (sc scenaOpoziva) opozivStigne(t *testing.T) {
	t.Helper()
	moraRazmijeniti(t, sc.a, sc.d)
	moraRazmijeniti(t, sc.d, sc.p)
	moraRazmijeniti(t, sc.p, sc.a)
	moraRazmijeniti(t, sc.p, sc.d)
}

// opozvanVani: nakon što je opoziv stigao, c ne prolazi ničija vrata, a d
// i dalje razmjenjuje sa svima
func (sc scenaOpoziva) opozvanVani(t *testing.T) {
	t.Helper()
	for _, s := range []*Service{sc.a, sc.p, sc.d} {
		if err := razmijeni(sc.c, s); err == nil {
			t.Errorf("opozvani član prošao je vrata od %s", s.node.ID)
		}
		if s != sc.d {
			moraRazmijeniti(t, sc.d, s)
		}
	}
}

// Zahtjev za obnovu napisan prije opoziva stigne primatelju razmjenom, a
// njegova vrata c-u još vjeruju.
func TestOpozivIZahtjevPrije(t *testing.T) {
	sc := novaScenaOpoziva(t)
	if _, err := sc.c.ZatraziObnovu(ctxRub); err != nil {
		t.Fatal(err)
	}
	sc.opozovi(t, sc.a)
	moraRazmijeniti(t, sc.c, sc.p)
	cekajDa(t, "obnova kod primatelja", func() bool { return rokClanstva(sc.p, sc.c.node.ID).After(sc.mc.ExpiresAt) })
	sc.opozivStigne(t)
	sc.opozvanVani(t)
}

// Zahtjev napisan nakon opoziva, isto.
func TestOpozivIZahtjevPoslije(t *testing.T) {
	sc := novaScenaOpoziva(t)
	sc.opozovi(t, sc.a)
	if _, err := sc.c.ZatraziObnovu(ctxRub); err != nil {
		t.Fatal(err)
	}
	moraRazmijeniti(t, sc.c, sc.p)
	cekajDa(t, "obnova kod primatelja", func() bool { return rokClanstva(sc.p, sc.c.node.ID).After(sc.mc.ExpiresAt) })
	sc.opozivStigne(t)
	sc.opozvanVani(t)
}

// Gumb Obnovi kod primatelja koji opoziv još nema.
func TestOpozivIGumbObnovi(t *testing.T) {
	sc := novaScenaOpoziva(t)
	sc.opozovi(t, sc.a)
	if _, err := sc.p.ObnoviClana(ctxRub, sc.c.node.ID); err != nil {
		t.Fatal(err)
	}
	sc.opozivStigne(t)
	sc.opozvanVani(t)
}

// Novo uparivanje kod primatelja koji opoziv još nema.
func TestOpozivINovoUparivanje(t *testing.T) {
	sc := novaScenaOpoziva(t)
	sc.opozovi(t, sc.a)
	m, err := sc.p.izdajClanstvo(ctxRub, sc.c.node.ID, sc.c.javni())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Service{sc.p, sc.c} {
		if err := s.saveMembership(ctxRub, m); err != nil {
			t.Fatal(err)
		}
	}
	sc.opozivStigne(t)
	sc.opozvanVani(t)
}

// Drugo računalo s kopijom ključa mreže, koje opoziv još nema, obnovi c-a
// ključem mreže. To članstvo vrijedi i kad opoziv stigne: ponovni prijem
// ključem mreže mora raditi, a ovaj put je prihvaćen rizik (Odluke (26. 11.
// 2026.)). Test čuva da je tako, da se promjena ne dogodi nehotice.
func TestOpozivIKopijaKljuca(t *testing.T) {
	brziScrypt(t)
	sc := novaScenaOpoziva(t)
	raw, _, err := sc.a.KopijaKljucaMreze(ctxRub, lozinkaKopije)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.p.UcitajKopijuKljuca(ctxRub, raw, lozinkaKopije); err != nil {
		t.Fatal(err)
	}
	sc.opozovi(t, sc.a)
	if _, err := sc.p.ObnoviClana(ctxRub, sc.c.node.ID); err != nil {
		t.Fatal(err)
	}
	sc.opozivStigne(t)
	moraRazmijeniti(t, sc.c, sc.d)
	moraRazmijeniti(t, sc.c, sc.a)
}

// Opoziv koji potpiše primatelj q (c-a je primio q) ne pokriva članstvo
// koje c-u poslije izda primatelj p, a q ne smije opozvati člana kojeg je
// primio p. Takvog člana isključuje samo ključ mreže. To je ograničenje
// (Odluke (26. 11. 2026.)), i test ga čuva.
func TestOpozivDrugogPrimatelja(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p, q, c, d := rubniCvor(t, "ured-vukovar"), rubniCvor(t, "ured-vinkovci"), rubniCvor(t, "laptop-baranja"), rubniCvor(t, "server-kuca")
	primateljUMrezi(t, a, p)
	mq := primiIzravno(t, a, q)
	oq := potpisanaOvlast(t, a.network.Private(), q.node.ID, q.javni(), time.Now().Add(-300*dan), time.Now().Add(430*dan))
	for _, s := range []*Service{a, q} {
		if err := s.saveOvlast(ctxRub, oq); err != nil {
			t.Fatal(err)
		}
	}
	md := primiIzravno(t, a, d)
	mc := clanstvoPrimateljaOd(t, q, oq, c.node.ID, c.javni(), time.Now().Add(-200*dan))
	uMrezuS(t, q, c, mc)
	mrezaNaPetlji(t, []*Service{a, p, q, c, d}, clanstvoKod(t, a, a.node.ID), clanstvoKod(t, a, p.node.ID), mq, md, mc)
	sc := scenaOpoziva{a: q, p: p, c: c, d: d, mc: mc}
	sc.opozovi(t, q)
	if _, err := p.ObnoviClana(ctxRub, c.node.ID); err != nil {
		t.Fatal(err)
	}
	for _, par := range [][2]*Service{{q, p}, {p, q}, {p, d}, {q, d}, {p, a}} {
		moraRazmijeniti(t, par[0], par[1])
	}
	moraRazmijeniti(t, c, d) // ograničenje: q-ov opoziv ne pokriva članstvo od p
	if _, err := q.RevokeMembership(ctxRub, c.node.ID); err == nil || !strings.Contains(err.Error(), "primatelj koji ga je izdao") {
		t.Errorf("q opoziva člana kojeg je primio p: %v", err)
	}

	if _, err := a.RevokeMembership(ctxRub, c.node.ID); err != nil {
		t.Fatalf("ključ mreže ne opoziva člana: %v", err)
	}
	moraRazmijeniti(t, a, d)
	for _, s := range []*Service{a, d} {
		if err := razmijeni(c, s); err == nil {
			t.Errorf("član kojeg je opozvao ključ mreže prošao je vrata od %s", s.node.ID)
		}
	}
	moraRazmijeniti(t, d, p)
	moraRazmijeniti(t, p, q)
}
