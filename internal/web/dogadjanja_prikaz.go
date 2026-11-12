package web

import (
	"maps"
	"net/url"
	"slices"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Izgled stranice Događanja po crtežu „15a · Dokumenti”, odjeljak
// „1b · Događanja”: modul kao čipovi, razdoblje i ostali filtri iznad
// popisa, dani kao kartice. Polja filtra i adrese ostaju iste.

// parametriDogadjanja su polja filtra koja čuvaju čipovi, izvoz i „Starije”
var parametriDogadjanja = []string{"modul", "od", "do", "sektor", "podrucje", "osoba"}

// cipoviModula su čipovi iznad popisa: prvi je „Svi”, a modula koje račun ne
// vidi nema. Poveznica čuva razdoblje, sektor, BP i osobu, a listanje unatrag
// počinje ispočetka.
func cipoviModula(izabran string, skriveni []string, q url.Values) []CipPopisa {
	osnova := zadrziParametre(q, "od", "do", "sektor", "podrucje", "osoba")
	out := []CipPopisa{{Naziv: "Svi", URL: adresaSIzborom("/dogadjanja", osnova), Odabran: izabran == ""}}
	for _, m := range service.ModuliZida {
		if slices.Contains(skriveni, m.ID) {
			continue
		}
		izbor := maps.Clone(osnova)
		izbor.Set("modul", m.ID)
		out = append(out, CipPopisa{Naziv: m.Naziv, URL: adresaSIzborom("/dogadjanja", izbor), Odabran: m.ID == izabran})
	}
	return out
}

// razdobljeDogadjanja: „19. – 26. 10. 2026.”, „od 19. 10. 2026.”, „sve”
func razdobljeDogadjanja(od, do string) string {
	a, errA := time.ParseInLocation("2006-01-02", od, models.Zagreb)
	b, errB := time.ParseInLocation("2006-01-02", do, models.Zagreb)
	switch {
	case errA != nil && errB != nil:
		return "sve"
	case errB != nil:
		return "od " + a.Format("2. 1. 2006.")
	case errA != nil:
		return "do " + b.Format("2. 1. 2006.")
	case a.Equal(b):
		return a.Format("2. 1. 2006.")
	case a.Year() == b.Year() && a.Month() == b.Month():
		return a.Format("2.") + " – " + b.Format("2. 1. 2006.")
	case a.Year() == b.Year():
		return a.Format("2. 1.") + " – " + b.Format("2. 1. 2006.")
	}
	return a.Format("2. 1. 2006.") + " – " + b.Format("2. 1. 2006.")
}

// izgledDogadjanja upisuje u stranicu čipove, razdoblje i adrese izvoza i
// listanja unatrag.
func izgledDogadjanja(data *DogadjanjaPageData, q url.Values) {
	data.CipoviModula = cipoviModula(data.Filtar.Modul, data.Filtar.Skriveni, q)
	data.Razdoblje = razdobljeDogadjanja(data.Od, data.Do)
	izbor := zadrziParametre(q, parametriDogadjanja...)
	data.IzvozURL = adresaSIzborom("/dogadjanja.xlsx", izbor)
	if data.Dalje != "" {
		starije := maps.Clone(izbor)
		starije.Set("prije", data.Dalje)
		data.StarijeURL = adresaSIzborom("/dogadjanja", starije)
	}
}
