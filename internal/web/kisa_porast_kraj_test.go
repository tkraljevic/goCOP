package web

import (
	"testing"

	"gocop/internal/prognoza"
)

// Redak porasta pokazuje letvu na kraju međusliva, a ne prvu s popisa:
// za Mohács → Aljmaš Aljmaš, iako Osijek stoji prvi i raste 2 cm.
func TestPorastRetkaLetvaNaKraju(t *testing.T) {
	sliv := func(oznaka string, letve ...PorastLetve) SlivUKisi {
		return SlivUKisi{StanjeSliva: prognoza.StanjeSliva{Sliv: oznaka}, Letve: letve}
	}
	osijek := PorastLetve{Letva: "osijek", Naziv: "Osijek", Cm: 2, Dan: 6, Ima: true, Od: -170}
	batina := PorastLetve{Letva: "batina", Naziv: "Batina", Cm: 55, Dan: 6, Ima: true, Od: -55}
	aljmas := PorastLetve{Letva: "aljmas", Naziv: "Aljmaš", Cm: 45, Dan: 6, Ima: true, Od: -10}
	tests := []struct {
		ime  string
		s    SlivUKisi
		want string
	}{
		{"letva na kraju", sliv("J", osijek, batina, aljmas), "u prognozi Aljmaš raste s −10 na 35 cm do 6. dana"},
		{"letva na kraju ne raste", sliv("J", osijek, batina, PorastLetve{Letva: "aljmas", Naziv: "Aljmaš", Cm: 0.4, Ima: true}), ""},
		{"letve na kraju nema u nizu", sliv("J", osijek, batina, PorastLetve{Letva: "aljmas", Naziv: "Aljmaš"}), "u prognozi Osijek raste s −170 na −168 cm do 6. dana"},
		{"nepoznat međusliv", sliv("X", PorastLetve{Letva: "a", Naziv: "Prva", Cm: 0.2, Ima: true}, batina), "u prognozi Batina raste s −55 na 0 cm do 6. dana"},
		{"bez porasta", sliv("X", PorastLetve{Letva: "a", Naziv: "Prva"}), ""},
	}
	for _, tt := range tests {
		if got := porastRetka(tt.s); got != tt.want {
			t.Errorf("%s: %q, a treba %q", tt.ime, got, tt.want)
		}
	}
}

// Svaka letva na kraju mora biti među letvama u čiju prognozu ulazi kiša
// tog međusliva, inače redak porasta tiho padne na prvu letvu.
func TestKrajMedjuslivaUPrognozi(t *testing.T) {
	letve := prognoza.SlivoviUPrognozi()
	for s, kraj := range krajMedjusliva {
		ima := false
		for _, l := range letve[s] {
			ima = ima || l == kraj
		}
		if !ima {
			t.Errorf("međusliv %s: letve %s nema među %v", s, kraj, letve[s])
		}
	}
	for s := range letve {
		if _, ok := krajMedjusliva[s]; !ok {
			t.Errorf("međusliv %s nema letvu na kraju", s)
		}
	}
}
