package web

import (
	"strings"
	"testing"
	"time"
)

func TestPlocicaRazmjene(t *testing.T) {
	sad := time.Now()
	malo := sad.Add(-2 * time.Minute)
	for _, slucaj := range []struct {
		ime     string
		st      RazmjenaStanje
		ocekuje []string
	}{
		{"bez mreže", RazmjenaStanje{}, []string{"nije ni u jednoj mreži"}},
		{"usklađeno", RazmjenaStanje{UMrezi: true,
			Cvorovi:  []CvorRazmjene{{Naziv: "COP Osijek (Unraid)", Dostupnost: "online", Zadnja: &malo}},
			Prognoza: PrognozaRazmjene{Ima: true, Izdavac: "cop-osijek-unraid", Primljeno: true, Izdano: sad, Nastalo: sad}},
			[]string{"Sve je usklađeno s 1 čvorom", "prije 2 min", "s čvora cop-osijek-unraid"}},
		{"prva razmjena", RazmjenaStanje{UMrezi: true,
			Cvorovi:     []CvorRazmjene{{Naziv: "laptop", Dostupnost: "online", Zadnja: &malo, Primljeno: 5000, JosPrima: true}},
			Arhiva:      ArhivaRazmjene{UKazalu: 298, Ugradjeno: 168, Ceka: 130, CekaBajtova: 12_400_000, Trenutno: "ugradnja paketa osijek"},
			SadrzajCeka: 12, SadrzajBajtova: 3_000_000,
			Prognoza: PrognozaRazmjene{Ima: true, Nastalo: sad.Add(-4 * time.Hour), Izdano: sad.Add(-4 * time.Hour)}},
			[]string{"prima se još", "ugrađeno 168 od 298 paketa", "12 MB", "upravo: ugradnja paketa osijek", "width:56%", "čeka dohvat 12", "starije od tri sata"}},
	} {
		var b strings.Builder
		if err := razmjenaTmpl.Execute(&b, slucaj.st); err != nil {
			t.Fatalf("%s: %v", slucaj.ime, err)
		}
		for _, o := range slucaj.ocekuje {
			if !strings.Contains(b.String(), o) {
				t.Errorf("%s: nema %q u\n%s", slucaj.ime, o, b.String())
			}
		}
	}
}
