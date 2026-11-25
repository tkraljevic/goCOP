package propisi

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

func dioSTekstom(retci ...string) []models.DioPropisa {
	d := models.DioPropisa{Oznaka: "clanak-9", Vrsta: models.DioClanak}
	for _, r := range retci {
		d.Stavci = append(d.Stavci, models.StavakPropisa{Tekst: r, Navod: strings.HasPrefix(r, "»")})
	}
	return []models.DioPropisa{{Oznaka: "uvod", Vrsta: models.DioUvod}, d}
}

// Odluke (25. 11. 2026.): NN ne navode dan stupanja na snagu, pa se računa
// iz odredbe akta i datuma objave: „prvoga” i „osmoga dana od dana objave”,
// „danom objave” i stalni datum. Odredba u bloku izmjene ne vrijedi za samu
// izmjenu.
func TestStupanjeNaSnagu(t *testing.T) {
	objava := time.Date(2023, 5, 3, 0, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		tekst, dan, izraz string
	}{
		{"Ovaj Zakon stupa na snagu prvoga dana od dana objave u »Narodnim novinama«.", "2023-05-04", "prvoga dana od dana objave"},
		{"Ovaj Zakon stupa na snagu osmoga dana od dana objave u »Narodnim novinama«.", "2023-05-11", "osmoga dana od dana objave"},
		{"Ovaj Plan stupa na snagu danom objave u »Narodnim novinama«.", "2023-05-03", "danom objave"},
		{"Ovaj Zakon objavit će se u »Narodnim novinama«, a stupa na snagu 1. kolovoza 2021.", "2021-08-01", "1. kolovoza 2021."},
		{"Ovaj Pravilnik stupa na snagu 15. studenog 2024.", "2024-11-15", "15. studenog 2024."},
	} {
		dan, izraz, ok := StupaNaSnagu(dioSTekstom("Druga odredba.", s.tekst), objava)
		if !ok || dan.Format("2006-01-02") != s.dan || izraz != s.izraz {
			t.Errorf("%q: %v %q %v, želi %s %q", s.tekst, dan.Format("2006-01-02"), izraz, ok, s.dan, s.izraz)
		}
	}
	if _, _, ok := StupaNaSnagu(dioSTekstom("Bez odredbe.", "»Ovaj Zakon stupa na snagu danom objave.«"), objava); ok {
		t.Error("odredba bez izraza ili u bloku je prepoznata")
	}
	dan, izraz, ok := StupaNaSnagu(dioSTekstom("Ovaj Zakon stupa na snagu osmoga dana od dana objave."), time.Time{})
	if ok || !dan.IsZero() || izraz != "osmoga dana od dana objave" {
		t.Errorf("bez datuma objave: %v %q %v", dan, izraz, ok)
	}
}
