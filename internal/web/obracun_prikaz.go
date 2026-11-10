package web

import (
	"time"

	"gocop/internal/models"
)

// rasponDana piše razdoblje obračuna kako ga crtež pokazuje: „12. 10. –
// 26. 10. 2026.”, a preko granice godine „28. 12. 2025. – 3. 1. 2026.”.
// Ulaz su dani iz obrasca (2006-01-02); ono što se ne da pročitati ostaje
// kako je upisano.
func rasponDana(od, do string) string {
	a, errA := time.ParseInLocation("2006-01-02", od, models.Zagreb)
	b, errB := time.ParseInLocation("2006-01-02", do, models.Zagreb)
	if errA != nil || errB != nil {
		return od + " – " + do
	}
	kraj := b.Format("2. 1. 2006.")
	if a.Year() == b.Year() {
		return a.Format("2. 1.") + " – " + kraj
	}
	return a.Format("2. 1. 2006.") + " – " + kraj
}

// danKratko je dan na kartici IORS-a, „pon 26. 10. 2026.”
func danKratko(t time.Time) string {
	return daniUTjednuKratko[t.Weekday()] + " " + t.Format("2. 1. 2006.")
}
