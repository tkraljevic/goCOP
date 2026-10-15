package peers

import (
	"errors"
	"testing"

	"gocop/internal/razmjena"
)

// Odbijenica druge strane postaje greška s njezinim razlogom i kodom.
// Odbijenica čvora 0.0.38 nema kod, pa ga novi čvor uzme iz poznatog
// razloga: tako i od starijeg čvora prepozna isključen Tailscale. Nepoznat
// razlog bez koda daje kod 0, a s kodom odlučuje kod. Tekst greške za
// korisnika je isti s kodom i bez njega. G3 (docs/plan-web-sync.md §3).
func TestOdbijenicaBezKodaOdStarijegCvora(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	for _, p := range []struct {
		razlog         string
		kodStarog, kod razmjena.KodRazloga
	}{
		{razmjena.RazlogZauzet, razmjena.KodZauzet, razmjena.KodZauzet},
		{razlogUpisZauzet, razmjena.KodUpisZauzet, razmjena.KodUpisZauzet},
		{razlogBezTailscalea, razmjena.KodBezTailscalea, razmjena.KodBezTailscalea},
		{"razmjena na ovim vratima nije dopuštena", 0, razmjena.KodOdbijeno},
		// noviji čvor s drukčijim tekstom: odlučuje kod, ne tekst
		{"Tailscale je na ovom računalu isključen", 0, razmjena.KodBezTailscalea},
	} {
		stara := neocekivanaPoruka(kindFrontier, razmjena.Envelope{Kind: razmjena.VrstaOdbijeno, Reason: p.razlog})
		nova := neocekivanaPoruka(kindFrontier, razmjena.Odbijenica(p.kod, p.razlog))
		if stara != (odbijenaRazmjena{p.razlog, p.kodStarog}) || nova != (odbijenaRazmjena{p.razlog, p.kod}) {
			t.Errorf("%q: bez koda %#v, s kodom %#v", p.razlog, stara, nova)
		}
		var o odbijenaRazmjena
		if !errors.As(stara, &o) || stara.Error() != "čvor odbio razmjenu: "+p.razlog || nova.Error() != stara.Error() {
			t.Errorf("tekst: %q / %q", stara, nova)
		}
		if s.odbioTailscale(ctxRub, "ured-vukovar", stara) != (p.kodStarog == razmjena.KodBezTailscalea) ||
			s.odbioTailscale(ctxRub, "ured-vukovar", nova) != (p.kod == razmjena.KodBezTailscalea) {
			t.Errorf("%q: isključen Tailscale prepoznat krivo", p.razlog)
		}
	}
}
