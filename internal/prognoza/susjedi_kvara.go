package prognoza

import "slices"

// SusjediKvara su susjedi s kojima se provjerava kvar mjerila letve (paket
// plato), a registar ih ne zna: letva s druge obale (ParoviObala) i susjedi
// iz rekonstrukcije rupe (Rekonstrukcije). Mnoge strane letve u registru
// nemaju vodu ni stacionažu, a ovi parovi su na arhivi provjereni.
func SusjediKvara() map[string][]string {
	out := map[string][]string{}
	dodaj := func(a, b string) {
		if !slices.Contains(out[a], b) {
			out[a] = append(out[a], b)
		}
	}
	for _, p := range ParoviObala {
		dodaj(p[0], p[1])
		dodaj(p[1], p[0])
	}
	for letva, susjedi := range Rekonstrukcije {
		for _, s := range susjedi {
			dodaj(letva, s.Letva)
		}
	}
	return out
}
