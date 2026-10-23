package razmjena

import "testing"

// Naziv puta ovisi o tome je li adresa druge strane privatna, a ne o
// podmreži (plan WEB synca, „Putovi i natpisi”); prazan ili nepoznat put
// nema naziv.
func TestNazivPuta(t *testing.T) {
	for _, c := range []struct {
		put      Put
		privatna bool
		naziv    string
	}{
		{PutLAN, false, NazivLAN}, {PutLAN, true, NazivLAN},
		{PutAdresa, true, NazivLAN}, {PutAdresa, false, NazivServer},
		{PutProbijeno, true, NazivLAN}, {PutProbijeno, false, NazivWeb},
		{PutTailscale, true, NazivTailscale}, {PutTailscale, false, NazivTailscale},
		{PutKrozTunel, true, NazivServer}, {PutKrozTunel, false, NazivServer},
		{"", true, ""}, {"nepoznat", false, ""},
	} {
		if got := NazivPuta(c.put, c.privatna); got != c.naziv {
			t.Errorf("%q (privatna %v): %q, očekivano %q", c.put, c.privatna, got, c.naziv)
		}
	}
}

// Razlozi puta spremaju se u bazu, pa se vrijednosti ne smiju mijenjati;
// trajna su točno tri koja traže čovjeka.
func TestRazloziPutaStalni(t *testing.T) {
	trajni := map[RazlogPuta]bool{RazlogPutaNijeClan: true, RazlogPutaStarijiProgram: true, RazlogPutaIstiKljuc: true}
	for r, vrijednost := range map[RazlogPuta]string{
		RazlogPutaNijeSeJavio: "nije-se-javio", RazlogPutaBezOdgovora: "bez-odgovora", RazlogPutaUDPNeProlazi: "udp-ne-prolazi", RazlogPutaNATOvisan: "nat-ovisan",
		RazlogPutaUDPDoDrugih: "udp-do-drugih", RazlogPutaVatrozid: "vatrozid", RazlogPutaNijeClan: "nije-clan",
		RazlogPutaStarijiProgram: "stariji-program", RazlogPutaPriprema: "priprema", RazlogPutaPauza: "pauza",
		RazlogPutaIstiKljuc: "isti-kljuc",
	} {
		if string(r) != vrijednost {
			t.Errorf("%q se promijenio u %q", vrijednost, r)
		}
		if r.Trajan() != trajni[r] {
			t.Errorf("%q: trajan %v", r, r.Trajan())
		}
	}
}
