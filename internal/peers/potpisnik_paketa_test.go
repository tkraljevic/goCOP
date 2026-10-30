package peers

import (
	"strings"
	"testing"
)

// Paket koji je potpisao član mreže pod svojim imenom ili ovo računalo samo
// prolazi; ključ izvan mreže i član pod tuđim imenom ne prolaze, a dok
// računalo nije u mreži, tuđi ključ ostaje neprovjeren. Vlastiti ključ
// računalo prepoznaje i bez mreže.
func TestPotpisnikPaketa(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	stranac := rubniCvor(t, "tudji-laptop")
	novi := rubniCvor(t, "novi-laptop")
	for _, x := range []struct {
		opis      string
		primatelj *Service
		izdao     string
		s         *Service
		potpisnik Potpisnik
		greska    string
	}{
		{"član mreže", b, a.node.ID, a, PotpisnikClan, ""},
		{"ovo računalo", b, b.node.ID, b, PotpisnikOvoRacunalo, ""},
		{"ključ izvan mreže", b, stranac.node.ID, stranac, PotpisnikNeprovjeren, "nije član naše mreže"},
		{"član pod tuđim imenom", b, stranac.node.ID, a, PotpisnikNeprovjeren, "potpisao ga je drugi član mreže"},
		{"računalo bez mreže, tuđi ključ", novi, a.node.ID, a, PotpisnikNeprovjeren, ""},
		{"računalo bez mreže, vlastiti ključ", novi, novi.node.ID, novi, PotpisnikOvoRacunalo, ""},
	} {
		p, err := x.primatelj.PotpisnikPaketa(ctxRub, x.izdao, x.s.javni())
		if p != x.potpisnik || (x.greska == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), x.greska)) {
			t.Errorf("%s: %v %v", x.opis, p, err)
		}
	}
}
