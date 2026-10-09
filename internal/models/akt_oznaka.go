package models

import (
	"sort"
	"time"
)

// RjesenjeOznake: dva ovjerena akta nose istu oznaku (u pravilu s dva
// čvora), oba ostaju valjana i potpisana, a Voditelj Centra obrane od
// poplava ili zamjenik označio je sukob riješenim, s kratkom napomenom
// (Odluke (8. 10. 2026.), pitanje 4). Upisuje se zasebno, s vlastitom
// verzijom u knjizi, a ne na akte: akt drugog čvora ostaje onakav kakav je
// ondje potpisan ili poništen.
type RjesenjeOznake struct {
	ID        string    `json:"id"`
	Sektor    string    `json:"sektor"`
	Oznaka    string    `json:"oznaka"` // npr. B-1/2026
	Akti      []string  `json:"akti"`   // akti iste oznake koje rješenje obuhvaća
	Napomena  string    `json:"napomena"`
	RijesioID string    `json:"rijesio_id"`
	Rijesio   string    `json:"rijesio"`
	Kad       time.Time `json:"kad"`
}

// Obuhvaca javlja obuhvaća li rješenje sve zadane akte; akt iste oznake koji
// stigne poslije rješenja nije obuhvaćen i sukob ponovno otvara
func (r *RjesenjeOznake) Obuhvaca(ids []string) bool {
	if r == nil {
		return false
	}
	ima := map[string]bool{}
	for _, id := range r.Akti {
		ima[id] = true
	}
	for _, id := range ids {
		if !ima[id] {
			return false
		}
	}
	return true
}

// SukobOznake: akt nosi istu oznaku kao drugi ovjereni akt
type SukobOznake struct {
	Drugi    []Akt           // ostali akti iste oznake
	Rjesenje *RjesenjeOznake // nil dok sukob nije riješen
}

// Rijesen javlja je li sukob označen riješenim
func (s SukobOznake) Rijesen() bool { return s.Rjesenje != nil }

// SukobiOznaka su sukobi oznaka među aktima, po ID-u akta: svaki akt s
// brojem čiju oznaku (sektor, broj, godina) nosi još koji akt. Nacrt nema
// broja i ne ulazi; poništen akt zadržava broj, pa ulazi. Sukob je riješen
// kad neko od rješenja obuhvaća sve te akte.
func SukobiOznaka(akti []Akt, rjesenja []RjesenjeOznake) map[string]SukobOznake {
	skupine := map[string][]Akt{}
	for _, a := range akti {
		if a.Broj > 0 {
			skupine[a.Oznaka()] = append(skupine[a.Oznaka()], a)
		}
	}
	out := map[string]SukobOznake{}
	for _, s := range skupine {
		if len(s) < 2 {
			continue
		}
		rjesenje := rjesenjeSkupine(s, rjesenja)
		for i := range s {
			out[s[i].ID] = SukobOznake{Drugi: bezAkta(s, i), Rjesenje: rjesenje}
		}
	}
	return out
}

// IDAkata su identiteti akata, poredani
func IDAkata(akti []Akt) []string {
	out := make([]string, 0, len(akti))
	for _, a := range akti {
		out = append(out, a.ID)
	}
	sort.Strings(out)
	return out
}

// rjesenjeSkupine je rješenje koje obuhvaća sve akte skupine iste oznake
func rjesenjeSkupine(s []Akt, rjesenja []RjesenjeOznake) *RjesenjeOznake {
	ids := IDAkata(s)
	for i := range rjesenja {
		if rjesenja[i].Obuhvaca(ids) {
			return &rjesenja[i]
		}
	}
	return nil
}

// bezAkta su akti skupine osim i-tog
func bezAkta(s []Akt, i int) []Akt {
	out := make([]Akt, 0, len(s)-1)
	out = append(out, s[:i]...)
	return append(out, s[i+1:]...)
}
