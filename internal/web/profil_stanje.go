package web

import (
	"fmt"

	"gocop/internal/models"
)

// Stanje uz odjeljke u popisu Profila (Odluke (15. 11. 2026.), Profil 8):
// ispod naziva odjeljka kratko piše što je u njemu, da se ne mora otvarati.

// StanjeDezurstava zbraja dežurstva iz svih planova osobe i koliko ih čeka
// potvrdu; prazno kad osoba nije ni u jednom planu
func (d UserPageData) StanjeDezurstava() string {
	n, ceka := 0, 0
	for _, p := range d.Planovi {
		n += p.Dezurstava
		ceka += p.CekaPotvrdu
	}
	if n == 0 {
		return ""
	}
	s := fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "dežurstvo", "dežurstva", "dežurstava"))
	if ceka > 0 {
		s += fmt.Sprintf(" · %d %s potvrdu", ceka, models.UzBrojHR(ceka, "čeka", "čekaju", "čeka"))
	}
	return s
}

// StanjePrijava broji prijave osobe i one na koje administrator još nije
// odgovorio; prazno kad prijava nema
func (d UserPageData) StanjePrijava() string {
	n := len(d.MojePrijave)
	if n == 0 {
		return ""
	}
	bez := 0
	for _, p := range d.MojePrijave {
		if len(p.Odgovori) == 0 {
			bez++
		}
	}
	s := fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "prijava", "prijave", "prijava"))
	if bez > 0 {
		s += fmt.Sprintf(" · %d bez odgovora", bez)
	}
	return s
}

// StanjePotpisa kaže ima li sken vlastoručnog potpisa i do kada vrijedi
// elektronički ključ
func (d UserPageData) StanjePotpisa() string {
	sken := "bez skena"
	if d.ImaPotpisSliku {
		sken = "sken spremljen"
	}
	switch {
	case !d.Kljuc.Ima:
		return sken + " · bez ključa"
	case d.Kljuc.Istekao:
		return sken + " · ključ istekao"
	}
	return fmt.Sprintf("%s · ključ do %d.", sken, d.Kljuc.Do.In(models.Zagreb).Year())
}
