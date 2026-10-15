package models

import "time"

// PotvrdaAkta je izričita potvrda uz akt: što je program pitao, tko je
// potvrdio i kada (Odluke (8. 10. 2026.)). Bilježi se uz akt, a ne ulazi u
// potpisani sadržaj.
type PotvrdaAkta struct {
	Vrsta string    `json:"vrsta"` // PotvrdaVrijedi, PotvrdaPrimatelji
	Tekst string    `json:"tekst"` // što je potvrđeno, kako je pitano
	TkoID string    `json:"tko_id"`
	Tko   string    `json:"tko"`
	Kad   time.Time `json:"kad"`
}

// Vrste potvrda uz akt
const (
	// PotvrdaVrijedi: „Vrijedi od” je više od 24 sata unatrag ili više od 3
	// dana unaprijed (Odluke (8. 10. 2026.), pitanje 17)
	PotvrdaVrijedi = "VRIJEDI"
	// PotvrdaPrimatelji: akt je ovjeren dok je stajalo upozorenje o popisu
	// primatelja (Odluke (8. 10. 2026.), pitanje 19)
	PotvrdaPrimatelji = "PRIMATELJI"
)

// ImaPotvrdu javlja nosi li akt potvrdu te vrste
func (a Akt) ImaPotvrdu(vrsta string) bool {
	for _, p := range a.Potvrde {
		if p.Vrsta == vrsta {
			return true
		}
	}
	return false
}
