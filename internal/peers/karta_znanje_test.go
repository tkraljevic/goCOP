package peers

import (
	"testing"
)

// Računalo javlja drugome koju kartu bez interneta ima (samo za prikaz na
// Stanju razmjene, Odluke (18. 11. 2026.), Administracija 8): nakon
// razmjene drugo računalo zna otisak i je li karta složena, a pločica
// ovog računala pokazuje i vlastitu
func TestRazmjenaJavljaKartu(t *testing.T) {
	smanjiOgrade(t, 64<<10, 64<<10, 16<<10)
	k, _, izvor := kartaUDijelovima(t, 300_000, 16<<10, 31)
	o := &opisKarte{}
	o.postavi(&k)
	cv := clanovi(t, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	if l.znanjeKarte() != nil {
		t.Fatal("bez prijenosa karata znanje o karti mora biti nil")
	}
	a.PostaviKarte(izvor, o.citaj, nil)
	l.PostaviKarte(t.TempDir(), o.citaj, nil)
	smiri(a.karte.Load())
	smiri(l.karte.Load())
	if z := l.znanjeKarte(); z == nil || z.Otisak != k.Otisak || z.Slozena || z.Svih != len(k.Dijelovi) {
		t.Fatalf("prije prijenosa: %+v", z)
	}
	if _, _, err := l.SyncWith(ctxRub, a.node.ID); err != nil {
		t.Fatal(err)
	}
	stanja, err := l.syncStates(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	if z := stanja[a.node.ID].Karta; z == nil || z.Otisak != k.Otisak || !z.Slozena || z.Dijelova != len(k.Dijelovi) {
		t.Errorf("što L zna o karti A: %+v", z)
	}
	st, err := l.Status(ctxRub, false)
	if err != nil || st.Karta == nil || st.Karta.Otisak != k.Otisak {
		t.Errorf("pločica L o svojoj karti: %+v %v", st, err)
	}

	// bez opisa karte računalo javlja da je nema
	prazno := &opisKarte{}
	b := clanovi(t, "ured-vukovar")[0]
	b.PostaviKarte(t.TempDir(), prazno.citaj, nil)
	smiri(b.karte.Load())
	if z := b.znanjeKarte(); z == nil || z.Otisak != "" {
		t.Errorf("bez karte: %+v", z)
	}
}
