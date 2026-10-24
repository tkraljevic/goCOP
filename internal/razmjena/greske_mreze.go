package razmjena

// Greške spajanja koje program prepoznaje po vrsti: veza odbijena (na
// portu nitko ne sluša) i nema rute do adrese. Brojevi grešaka razlikuju se
// po sustavu (greske_mreze_*.go).

import (
	"errors"
	"slices"
)

// VezaOdbijena: računalo je tu, ali na tom portu nitko ne sluša
func VezaOdbijena(err error) bool { return jednaOd(err, greskeOdbijeno) }

// BezRute: do adrese nema rute (mreža ili računalo nedostupno)
func BezRute(err error) bool { return jednaOd(err, greskeBezRute) }

func jednaOd(err error, zadane []error) bool {
	return err != nil && slices.ContainsFunc(zadane, func(z error) bool { return errors.Is(err, z) })
}
