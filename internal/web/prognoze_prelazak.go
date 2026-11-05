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

// terminRazine je jedan termin kartice: naslov, koliko je sati poslije
// izdanja i faza obrane prognoze u njemu.
type terminRazine struct {
	naslov, razina string
	sat            int
}

// kasnije je sat termina koji se ne zna: iza svih ostalih.
const kasnije = 1 << 30

// terminiRazina su termini kartice: +6 h, +12 h, pa dani, a doDana su sati
// od izdanja do dana. Blizi dosezi nemaju fazu u ćeliji, pa im se računa
// istim pragovima letve.
func terminiRazina(lp LetvaPrognoze, st models.Station, doDana []int) []terminRazine {
	out := make([]terminRazine, 0, len(lp.Vrijednosti)+len(lp.Dani))
	for _, v := range lp.Vrijednosti {
		if v.CmV != nil {
			out = append(out, terminRazine{fmt.Sprintf("+%d h", v.DosegH), razinaObrane(st, *v.CmV), v.DosegH})
		}
	}
	for k, d := range lp.Dani {
		sat := kasnije
		if k < len(doDana) {
			sat = doDana[k]
		}
		out = append(out, terminRazine{d.Naslov, d.Razina, sat})
	}
	return out
}

// oznakaPrelaska je najraniji termin čija je faza viša od faze sadašnjeg
// vodostaja, i ta faza. Termini se uspoređuju po satu, ne po redu na
// kartici: prvi dan (7 h) može doći prije +6 h ili između +6 h i +12 h.
// Bez sadašnjeg vodostaja nema s čime usporediti, a razini akumulacije
// (kota nad morem) pragovi letve ne pripadaju.
func oznakaPrelaska(lp LetvaPrognoze, st models.Station, doDana []int) *Prelazak {
	if lp.SadaCmV == nil || lp.SadaJed != "" {
		return nil
	}
	sada := redFaze[razinaObrane(st, *lp.SadaCmV)]
	var prvi *terminRazine
	for _, t := range terminiRazina(lp, st, doDana) {
		if redFaze[t.razina] > sada && (prvi == nil || t.sat < prvi.sat) {
			prvi = &t
		}
	}
	if prvi == nil {
		return nil
	}
	return &Prelazak{Razina: prvi.razina, Tekst: "↑ " + nazivFaze[prvi.razina] + " od " + prvi.naslov}
}

// oznaciPrelaske stavlja oznaku prelaska na kartice svih letvi; ciljevi su
// sati dana od epohe, izdanoH sat izdanja.
func oznaciPrelaske(letve []LetvaPrognoze, postaje map[string]models.Station, ciljevi []int64, izdanoH int64) {
	doDana := make([]int, len(ciljevi))
	for k, c := range ciljevi {
		doDana[k] = int(c - izdanoH)
	}
	for i := range letve {
		letve[i].Prelazak = oznakaPrelaska(letve[i], postaje[letve[i].Kod], doDana)
	}
}
