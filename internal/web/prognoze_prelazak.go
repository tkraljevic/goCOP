package web

import (
	"fmt"

	"gocop/internal/models"
)

// Prelazak je oznaka ispod sadašnjeg vodostaja na kartici letve: od kojeg
// termina prognoza doseže višu fazu obrane nego što je sada, npr.
// „↑ pripremno od uto 3.11.”. Pad u nižu fazu se ne označava.
type Prelazak struct {
	Razina string // prep | regular | emerg | crit, boja oznake
	Tekst  string
}

// redFaze je poredak faza obrane, od bez obrane do izvanrednog stanja.
var redFaze = map[string]int{"": 0, "prep": 1, "regular": 2, "emerg": 3, "crit": 4}

// nazivFaze je faza kako stoji u oznaci prelaska, malim slovima.
var nazivFaze = map[string]string{
	"prep": "pripremno", "regular": "redovna obrana", "emerg": "izvanredna obrana", "crit": "izvanredno stanje",
}

// terminRazine je jedan termin kartice: naslov i faza obrane prognoze u njemu.
type terminRazine struct{ naslov, razina string }

// terminiRazina su termini kartice redom kako stoje: +6 h, +12 h, pa dani.
// Blizi dosezi nemaju fazu u ćeliji, pa im se računa istim pragovima letve.
func terminiRazina(lp LetvaPrognoze, st models.Station) []terminRazine {
	out := make([]terminRazine, 0, len(lp.Vrijednosti)+len(lp.Dani))
	for _, v := range lp.Vrijednosti {
		if v.CmV != nil {
			out = append(out, terminRazine{fmt.Sprintf("+%d h", v.DosegH), razinaObrane(st, *v.CmV)})
		}
	}
	for _, d := range lp.Dani {
		out = append(out, terminRazine{d.Naslov, d.Razina})
	}
	return out
}

// oznakaPrelaska je prvi termin čija je faza viša od faze sadašnjeg
// vodostaja, i ta faza. Bez sadašnjeg vodostaja nema s čime usporediti, a
// razini akumulacije (kota nad morem) pragovi letve ne pripadaju.
func oznakaPrelaska(lp LetvaPrognoze, st models.Station) *Prelazak {
	if lp.SadaCmV == nil || lp.SadaJed != "" {
		return nil
	}
	sada := redFaze[razinaObrane(st, *lp.SadaCmV)]
	for _, t := range terminiRazina(lp, st) {
		if redFaze[t.razina] > sada {
			return &Prelazak{Razina: t.razina, Tekst: "↑ " + nazivFaze[t.razina] + " od " + t.naslov}
		}
	}
	return nil
}

// oznaciPrelaske stavlja oznaku prelaska na kartice svih letvi.
func oznaciPrelaske(letve []LetvaPrognoze, postaje map[string]models.Station) {
	for i := range letve {
		letve[i].Prelazak = oznakaPrelaska(letve[i], postaje[letve[i].Kod])
	}
}
