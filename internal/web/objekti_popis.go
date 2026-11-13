package web

import (
	"net/url"
	"strconv"

	"gocop/internal/models"
)

// Popis objekata (goCOP-m3, 17d · Registri, 5a): čipovi vode na postojeće
// registre (Nasipi, Crpne stanice…) s brojem zapisa, a jedan popis svih vrsta
// je novo ponašanje. Stupci tablice ovise o vrsti registra.

// cipoviRegistaraObjekata su čipovi registara objekata; čuvaju sektor,
// područje i pretragu, a izabran je registar ove stranice.
func cipoviRegistaraObjekata(trenutni models.StructureRegister, poVrsti map[string]int, q url.Values) []CipPopisa {
	osnova := zadrziParametre(q, "q", "sector", "area")
	out := make([]CipPopisa, 0, len(models.StructureRegisters))
	for _, reg := range models.StructureRegisters {
		n := 0
		for _, k := range reg.Kinds {
			n += poVrsti[k]
		}
		out = append(out, CipPopisa{Naziv: reg.Title + " (" + strconv.Itoa(n) + ")",
			URL: adresaSIzborom(reg.Path, osnova), Odabran: reg.Path == trenutni.Path})
	}
	return out
}
