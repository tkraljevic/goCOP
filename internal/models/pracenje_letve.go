package models

import (
	"sort"
	"strings"
)

// Sektori i branjena područja koji letvu prate (Station.PrateSektori i
// Station.PratePodrucja; Odluke (8. 10. 2026.), pitanja 8 i 9). Upisuju se
// u obrascu letve, s istim pravom kao i ostali podaci letve, i putuju s
// letvom razmjenom.

// ImaPracenje javlja ima li letva upisan ijedan sektor ili područje koje je
// prati
func (st Station) ImaPracenje() bool {
	return len(st.PrateSektori) > 0 || len(st.PratePodrucja) > 0
}

// SektoriPracenja su sektori koje letvi daje upisano praćenje: upisani
// sektori i sektori upisanih područja (sektorPodrucja: područje → sektor),
// bez praznih i dvojnika, poredani. Područje kojeg nema u sektorPodrucja
// sektor ne daje.
func (st Station) SektoriPracenja(sektorPodrucja map[int]string) []string {
	sve := append([]string{}, st.PrateSektori...)
	for _, a := range st.PratePodrucja {
		sve = append(sve, sektorPodrucja[a])
	}
	return urediSektore(sve)
}

// PratiSektor javlja je li sektor upisan među one koji letvu prate
func (st Station) PratiSektor(sektor string) bool { return sadrzi(st.PrateSektori, sektor) }

// PratiPodrucje javlja je li branjeno područje upisano među ona koja letvu
// prate
func (st Station) PratiPodrucje(podrucje int) bool { return sadrzi(st.PratePodrucja, podrucje) }

// UrediPracenje sređuje sektore i područja koji letvu prate: bez praznih i
// dvojnika, poredane; prazan popis je nil, pa ga JSON izostavi
func (st *Station) UrediPracenje() {
	st.PrateSektori = urediSektore(st.PrateSektori)
	st.PratePodrucja = urediPodrucja(st.PratePodrucja)
}

// NovoPracenje su sektori i područja praćenja kojih na staroj letvi nema;
// bez stare (nova letva) to je sve upisano
func (st Station) NovoPracenje(staro *Station) ([]string, []int) {
	if staro == nil {
		return st.PrateSektori, st.PratePodrucja
	}
	var sektori []string
	for _, s := range st.PrateSektori {
		if !sadrzi(staro.PrateSektori, s) {
			sektori = append(sektori, s)
		}
	}
	var podrucja []int
	for _, a := range st.PratePodrucja {
		if !sadrzi(staro.PratePodrucja, a) {
			podrucja = append(podrucja, a)
		}
	}
	return sektori, podrucja
}

func sadrzi[T comparable](popis []T, x T) bool {
	for _, y := range popis {
		if y == x {
			return true
		}
	}
	return false
}

// urediSektore: obrezane šifre, bez praznih i dvojnika, poredane; nil kad
// ne ostane nijedna
func urediSektore(sektori []string) []string {
	var out []string
	for _, s := range sektori {
		if s = strings.TrimSpace(s); s != "" && !sadrzi(out, s) {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// urediPodrucja: brojevi područja veći od nule, bez dvojnika, poredani; nil
// kad ne ostane nijedan
func urediPodrucja(podrucja []int) []int {
	var out []int
	for _, a := range podrucja {
		if a > 0 && !sadrzi(out, a) {
			out = append(out, a)
		}
	}
	sort.Ints(out)
	return out
}
