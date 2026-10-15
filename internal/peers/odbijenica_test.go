package peers

import (
	"errors"
	"maps"
	"testing"

	"gocop/internal/razmjena"
)

// Odbijenice koje šalje čvor 0.0.38 (v0.0.38-alfa, c0162ca9), zamrznute kao
// literali, kao omotnica0038 u paketu razmjena. Čvor 0.0.38 šalje samo
// razlog, a novi čvor kod uzme iz njega (kodoviStarijih). Čvor 0.0.38 i
// isključen Tailscale prepoznaje samo po tekstu. Ne mijenjaju se s
// konstantama programa.
const (
	zauzet0038        = "čvor je zauzet, pokušajte kasnije"
	upisZauzet0038    = "čvor upravo upisuje ono što je primio od drugih; poslano stiže sljedećom razmjenom"
	bezTailscalea0038 = "na ovom računalu razmjena preko Tailscalea je isključena ([sync] tailscale = false)"
)

// Novi čvor prepoznaje sve tri odbijenice čvora 0.0.38 i šalje ih istim
// tekstom. Kad se tekst promijeni, čvor 0.0.38 ga više ne prepoznaje (npr.
// isključen Tailscale: ne zove sljedeću adresu i pamti Tailscale adresu), a
// novi čvor ne prepoznaje tekst koji 0.0.38 i dalje šalje.
func TestOdbijeniceCvora0038(t *testing.T) {
	odbijenice0038 := map[string]razmjena.KodRazloga{
		zauzet0038:        razmjena.KodZauzet,
		upisZauzet0038:    razmjena.KodUpisZauzet,
		bezTailscalea0038: razmjena.KodBezTailscalea,
	}
	if !maps.Equal(kodoviStarijih, odbijenice0038) {
		t.Errorf("kodoviStarijih %v nisu odbijenice čvora 0.0.38 %v", kodoviStarijih, odbijenice0038)
	}
	for _, p := range []struct {
		ime, tekst, tekst0038 string
	}{
		{"razmjena.RazlogZauzet", razmjena.RazlogZauzet, zauzet0038},
		{"razlogUpisZauzet", razlogUpisZauzet, upisZauzet0038},
		{"razlogBezTailscalea", razlogBezTailscalea, bezTailscalea0038},
	} {
		if p.tekst != p.tekst0038 {
			t.Errorf("%s je %q, a čvor 0.0.38 prepoznaje samo %q", p.ime, p.tekst, p.tekst0038)
		}
	}
}

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
		{zauzet0038, razmjena.KodZauzet, razmjena.KodZauzet},
		{upisZauzet0038, razmjena.KodUpisZauzet, razmjena.KodUpisZauzet},
		{bezTailscalea0038, razmjena.KodBezTailscalea, razmjena.KodBezTailscalea},
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
