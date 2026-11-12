package web

import (
	"maps"
	"net/url"
	"strconv"

	"gocop/internal/models"
)

// Popis dionica (goCOP-m3, 17b · Registri, 3a): sektor i branjeno područje
// biraju se čipovima, pretraga ostaje u istom parametru q. Čipovi čuvaju
// pretragu; promjena sektora briše područje, jer ono pripada sektoru.

// cipoviPodrucjaDionica su čipovi branjenih područja izabranog sektora,
// prvi je „Sva područja”; bez izabranog sektora ih nema.
func cipoviPodrucjaDionica(q url.Values, sektor string, podrucja []models.Area, izabrano int) []CipPopisa {
	if sektor == "" {
		return nil
	}
	osnova := zadrziParametre(q, "q", "sector")
	out := []CipPopisa{{Naziv: "Sva područja", URL: adresaSIzborom("/sections", osnova), Odabran: izabrano == 0}}
	kratko := models.Terms().AreaShort
	for _, a := range podrucja {
		izbor := maps.Clone(osnova)
		izbor.Set("area", strconv.Itoa(a.ID))
		out = append(out, CipPopisa{Naziv: kratko + " " + strconv.Itoa(a.ID), URL: adresaSIzborom("/sections", izbor), Odabran: a.ID == izabrano})
	}
	return out
}

// izgledPopisaDionica upisuje čipove i adresu izvoza po istim filtrima
func izgledPopisaDionica(d *SectionsPageData, q url.Values) {
	d.CipoviSektora = cipoviSektoraPo("/sections", "sector", zadrziParametre(q, "q"), d.Sectors, d.SelectedSector)
	d.CipoviPodrucja = cipoviPodrucjaDionica(q, d.SelectedSector, d.Areas, d.SelectedArea)
	d.IzvozURL = adresaSIzborom("/sections/dionice.xlsx", zadrziParametre(q, "sector", "area", "q"))
}
