package web

import "strconv"

// TerminProfila je jedan čip iznad uzdužnog profila: sada, +12 h ili dan u
// 7 h. Odabrani termin crta se na profilu i ističe u karticama letvi.
type TerminProfila struct {
	Oznaka string // u adresi: ?dan=sada, ?dan=12h, ?dan=1 … 6
	Naslov string // na čipu, isto kao naslov stupca u kartici letve
	Opis   string // u legendi, iza „plavo:”
	Doseg  int    // sati od izdanja; 0 je zadnje mjerenje
}

// terminSada je zadani termin: zadnje mjerenje.
const terminSada = "sada"

// oznakaDana je oznaka k-tog dana (od nule) u adresi i na pločici kartice.
func oznakaDana(k int) string { return strconv.Itoa(k + 1) }

// terminiProfila slaže čipove: sada, +12 h i isti dani kao stupci kartica
// letvi (dani i ciljevi iz daniPregleda), s dosegom od sata izdanja.
func terminiProfila(dani []string, ciljevi []int64, izdanoH int64) []TerminProfila {
	termini := []TerminProfila{
		{Oznaka: terminSada, Naslov: "sada", Opis: "sada, zadnje mjerenje"},
		{Oznaka: "12h", Naslov: "+12 h", Opis: "za 12 h", Doseg: 12},
	}
	for k := 0; k < len(dani) && k < len(ciljevi); k++ {
		termini = append(termini, TerminProfila{Oznaka: oznakaDana(k), Naslov: dani[k],
			Opis: "odabrani dan, " + dani[k] + " u 7 h", Doseg: int(ciljevi[k] - izdanoH)})
	}
	return termini
}

// odabraniTermin vraća termin iz adrese (?dan=). Prazan ili nepoznat je
// „sada”, i kad poveznica ostane od starijeg izdanja.
func odabraniTermin(termini []TerminProfila, dan string) TerminProfila {
	for _, t := range termini {
		if t.Oznaka == dan {
			return t
		}
	}
	if len(termini) > 0 {
		return termini[0]
	}
	return TerminProfila{Oznaka: terminSada, Naslov: "sada", Opis: "sada, zadnje mjerenje"}
}
