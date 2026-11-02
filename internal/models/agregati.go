package models

import (
	"strconv"
	"strings"
)

// NajviseAgregata je za koliko agregata očitanje bilježi sate rada
const NajviseAgregata = 3

// SatiAgregata su sati rada agregata redom, od prvog
func (r Reading) SatiAgregata() []*int {
	return []*int{r.AgHours1, r.AgHours2, r.AgHours3}
}

// OpisSatiAgregata su sati rada za povijest očitanja, npr. „12/—/4”;
// agregati bez upisa na kraju se izostavljaju
func (r Reading) OpisSatiAgregata() string {
	sati := r.SatiAgregata()
	for len(sati) > 0 && sati[len(sati)-1] == nil {
		sati = sati[:len(sati)-1]
	}
	dijelovi := make([]string, len(sati))
	for i, v := range sati {
		dijelovi[i] = "—"
		if v != nil {
			dijelovi[i] = strconv.Itoa(*v)
		}
	}
	return strings.Join(dijelovi, "/")
}

// AgregataZaOcitanje je koliko agregata obrazac očitanja nudi: onoliko
// koliko stanica ima crpki, a kad se broj ne zna, najviše koliko očitanje
// bilježi. Agregat kojem su sati već upisani ostaje u obrascu, da se pri
// izmjeni ne izgube.
func (s Structure) AgregataZaOcitanje(r Reading) int {
	n := len(s.Crpke())
	if n == 0 || n > NajviseAgregata {
		n = NajviseAgregata
	}
	for i, v := range r.SatiAgregata() {
		if v != nil && i >= n {
			n = i + 1
		}
	}
	return n
}
