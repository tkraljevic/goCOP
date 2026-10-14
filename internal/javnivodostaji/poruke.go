package javnivodostaji

import (
	"fmt"

	"gocop/internal/models"
)

// Redci ispisa kruga („Detalji postupka” na Prognoze → Postavke). Riječ uz
// broj ide kroz models.UzBrojHR: „1 novo očitanje”, „2 nova očitanja”,
// „5 novih očitanja”, ali „21 novo očitanje”.

// brojNovihOcitanja je broj novih očitanja s riječima u obliku koji ide uz taj broj
func brojNovihOcitanja(n int) string {
	return fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "novo očitanje", "nova očitanja", "novih očitanja"))
}

// redakLetve je redak ispisa za jednu letvu kruga: greška preuzimanja, koliko
// je novih očitanja upisano ili da nema ništa novo
func redakLetve(ime string, s StanjeLetve) string {
	switch {
	case s.Greska != "":
		return ime + ": " + s.Greska
	case s.Novih > 0:
		return ime + ": " + brojNovihOcitanja(s.Novih)
	}
	return ime + ": ništa novo"
}

// opisPreuzetih je ishod preuzimanja svih letvi kruga, npr. „1 letva,
// 21 novo očitanje”
func opisPreuzetih(letvi, novih int) string {
	return fmt.Sprintf("%d %s, %s", letvi, models.UzBrojHR(letvi, "letva", "letve", "letvi"), brojNovihOcitanja(novih))
}
