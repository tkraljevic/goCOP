package models

import (
	"slices"
	"sort"
	"time"
)

// RjesenjeOznake: dva ovjerena akta nose istu oznaku (u pravilu s dva
// čvora), ili je isti nacrt ovjeren na dva čvora, sve ovjere ostaju valjane
// i potpisane, a Voditelj Centra obrane od poplava ili zamjenik označio je
// sukob riješenim, s kratkom napomenom
// (Odluke (8. 10. 2026.), pitanje 4). Upisuje se zasebno, s vlastitom
// verzijom u knjizi, a ne na akte: akt drugog čvora ostaje onakav kakav je
// ondje potpisan ili poništen.
type RjesenjeOznake struct {
	ID        string    `json:"id"`
	Sektor    string    `json:"sektor"`
	Oznaka    string    `json:"oznaka"`           // npr. B-1/2026
	Akti      []string  `json:"akti"`             // akti iste oznake koje rješenje obuhvaća
	Kodovi    []string  `json:"kodovi,omitempty"` // kodovi obuhvaćenih ovjera: isti nacrt ovjeren na dva čvora ima ih dva
	Napomena  string    `json:"napomena"`
	RijesioID string    `json:"rijesio_id"`
	Rijesio   string    `json:"rijesio"`
	Kad       time.Time `json:"kad"`
}

// Obuhvaca javlja obuhvaća li rješenje sve zadane ovjere: akt po
// identitetu i ovjeru po kodu. Ovjera koja stigne poslije rješenja (akt iste
// oznake, ili isti nacrt ovjeren i na drugom čvoru) nije obuhvaćena i sukob
// ponovno otvara. Rješenje bez kodova zapisano je u 0.0.35, koja ih još nije
// bilježila; ono obuhvaća akte samo po identitetu, da se sukobi riješeni
// prije nadogradnje ne otvore ponovno.
func (r *RjesenjeOznake) Obuhvaca(ovjere []Akt) bool {
	if r == nil {
		return false
	}
	for _, a := range ovjere {
		if !slices.Contains(r.Akti, a.ID) {
			return false
		}
		if len(r.Kodovi) > 0 && !slices.Contains(r.Kodovi, a.OvjeraKod) {
			return false
		}
	}
	return true
}

// SukobOznake: akt nosi istu oznaku kao drugi ovjereni akt, ili je isti
// nacrt ovjeren i na drugom čvoru
type SukobOznake struct {
	Drugi []Akt // ostali akti iste oznake; među njima i druga ovjera drugog akta (IzvanPopisa)
	// Ovjere su druge ovjere ovog akta: isti nacrt ovjeren je i na drugom
	// čvoru, dok među njima nije bilo veze, s drugim kodom i, u pravilu,
	// drugim brojem. U popisu akt stoji s jednom ovjerom; ove su u knjizi
	// verzija i ostaju valjane i potpisane (Odluke (8. 10. 2026.), pitanje 4).
	Ovjere   []Akt
	Rjesenje *RjesenjeOznake // nil dok sukob nije riješen

	izvanPopisa map[string]bool // ključevi ovjera (kljucOvjere) koje nisu u popisu
}

// Ima javlja ima li akt sukob oznake ili drugu ovjeru
func (s SukobOznake) Ima() bool { return len(s.Drugi)+len(s.Ovjere) > 0 }

// IzvanPopisa javlja je li akt d iz Drugi druga ovjera akta koji u popisu
// stoji s drukčijom ovjerom
func (s SukobOznake) IzvanPopisa(d Akt) bool { return s.izvanPopisa[kljucOvjere(d)] }

// Rijesen javlja je li sukob označen riješenim
func (s SukobOznake) Rijesen() bool { return s.Rjesenje != nil }

// Gdje su ovjereni akti iste oznake (SukobOznake.Cvorovi)
const (
	SukobRazlicitiCvorovi = "RAZLICITI" // bar jedan drugi akt na drugom čvoru
	SukobIstiCvor         = "ISTI"      // svi na čvoru ovog akta
)

// Cvorovi kaže gdje su ovjereni akti iste oznake, prema aktu a:
// SukobRazlicitiCvorovi, SukobIstiCvor, ili prazno kad se ne zna jer čvor
// nije upisan
func (s SukobOznake) Cvorovi(a Akt) string {
	if a.Cvor == "" {
		return ""
	}
	isti := SukobIstiCvor
	for _, d := range s.Drugi {
		switch d.Cvor {
		case a.Cvor:
		case "":
			isti = ""
		default:
			return SukobRazlicitiCvorovi
		}
	}
	return isti
}

// SukobiOznaka su sukobi oznaka među aktima, po ID-u akta. akti su akti u
// popisu, druge su druge ovjere tih akata iz knjige verzija (isti nacrt
// ovjeren i na drugom čvoru). Sukob ima svaki akt s brojem čiju oznaku
// (sektor, broj, godina) nosi još koji akt ili druga ovjera drugog akta, i
// svaki akt s drugom ovjerom. Nacrt nema broja i ne ulazi; poništen akt
// zadržava broj, pa ulazi. Sukob je riješen kad neko od rješenja obuhvaća
// sve njegove ovjere.
func SukobiOznaka(akti, druge []Akt, rjesenja []RjesenjeOznake) map[string]SukobOznake {
	izvan := map[string]bool{}
	for _, d := range druge {
		izvan[kljucOvjere(d)] = true
	}
	out := map[string]SukobOznake{}
	for _, s := range skupineOznaka(akti, druge) {
		for _, a := range s {
			if drugi := bezAkta(s, a.ID); len(drugi) > 0 && !izvan[kljucOvjere(a)] {
				out[a.ID] = SukobOznake{Drugi: drugi}
			}
		}
	}
	for _, d := range druge {
		x := out[d.ID]
		x.Ovjere = append(x.Ovjere, d)
		out[d.ID] = x
	}
	// ovjera akta u popisu: kod ovjere, ili prazan kad akt u popisu nema broja
	kodovi := map[string]string{}
	for _, a := range akti {
		kodovi[a.ID] = a.OvjeraKod
	}
	for id, x := range out {
		x.izvanPopisa = izvan
		x.Rjesenje = rjesenjeSukoba(x.SveOvjere(Akt{ID: id, OvjeraKod: kodovi[id]}), rjesenja)
		out[id] = x
	}
	return out
}

// SveOvjere su ovjere koje sukob akta a obuhvaća: a, ostali akti iste
// oznake i druge ovjere akta a
func (s SukobOznake) SveOvjere(a Akt) []Akt {
	out := append([]Akt{a}, s.Drugi...)
	return append(out, s.Ovjere...)
}

// KljuceviRjesenja su identiteti akata i kodovi ovjera koje rješenje
// obuhvaća, bez ponavljanja i poredani
func KljuceviRjesenja(ovjere []Akt) (akti, kodovi []string) {
	for _, a := range ovjere {
		akti = append(akti, a.ID)
		kodovi = append(kodovi, a.OvjeraKod)
	}
	sort.Strings(akti)
	sort.Strings(kodovi)
	return slices.Compact(akti), slices.Compact(kodovi)
}

// kljucOvjere razlikuje ovjere: kod nastaje pri svakoj ovjeri, pa isti akt
// ovjeren na dva čvora ima dva koda (i, u pravilu, dvije oznake)
func kljucOvjere(a Akt) string { return a.ID + "|" + a.OvjeraKod }

// skupineOznaka su akti s brojem i druge ovjere, po oznaci
func skupineOznaka(akti, druge []Akt) map[string][]Akt {
	skupine := map[string][]Akt{}
	for _, a := range append(slices.Clone(akti), druge...) {
		if a.Broj > 0 {
			skupine[a.Oznaka()] = append(skupine[a.Oznaka()], a)
		}
	}
	return skupine
}

// rjesenjeSukoba je rješenje koje obuhvaća sve ovjere sukoba
func rjesenjeSukoba(ovjere []Akt, rjesenja []RjesenjeOznake) *RjesenjeOznake {
	for i := range rjesenja {
		if rjesenja[i].Obuhvaca(ovjere) {
			return &rjesenja[i]
		}
	}
	return nil
}

// bezAkta su akti skupine osim akta id: druga ovjera istog akta s istom
// oznakom nije „drugi akt”, nego druga ovjera (SukobOznake.Ovjere)
func bezAkta(s []Akt, id string) []Akt {
	var out []Akt
	for _, a := range s {
		if a.ID != id {
			out = append(out, a)
		}
	}
	return out
}
