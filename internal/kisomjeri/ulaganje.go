package kisomjeri

import (
	"fmt"
	"math"
	"sort"
	"time"

	"gocop/internal/arhiva"
)

// SlivStabla je mapa arhivskog stabla u kojoj stoje sve meteorološke postaje
const SlivStabla = "drava"

// Ulozi dopunjuje arhivsko stablo mjerenjima od zadanog trenutka. Satna
// mjerenja idu u satni niz, dnevni zbrojevi u dnevni — ali samo dani koji su
// završili, jer se dnevni zbroj mijenja dok dan traje. Vraća letve kojima se
// niz promijenio, da se mogu izgraditi.
func Ulozi(koren string, s *Spremiste, postaje []Postaja, od, sada time.Time) ([]string, error) {
	var promijenjene []string
	l := sada.In(Zagreb)
	ponoc := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, Zagreb)
	for _, p := range postaje {
		m, err := s.Od(p.Code, od)
		if err != nil {
			return promijenjene, err
		}
		type kljuc struct {
			izvor, vrsta string
		}
		nizovi := map[kljuc][]arhiva.Redak{}
		// 12-satni zbrojevi (06 i 18) slažu se u dan kao u DHMZ-ovu dnevnom
		// nizu: dan D su oznake D 06 i D 18, a ulazi tek kad ima obje
		type polovice struct {
			zbroj float64
			n     int
			izvor string
		}
		dvanaest := map[string]*polovice{}
		for _, x := range m {
			switch x.Sati {
			case 12:
				d := x.Kraj.In(Zagreb).Format("2006-01-02")
				if dvanaest[d] == nil {
					dvanaest[d] = &polovice{izvor: x.Izvor}
				}
				dvanaest[d].zbroj += x.Oborina
				dvanaest[d].n++
			case 1:
				if x.Kraj.After(sada) {
					continue
				}
				k := kljuc{x.Izvor, "satni"}
				nizovi[k] = append(nizovi[k], arhiva.Redak{Vrijeme: x.Kraj, Vrijednost: x.Oborina})
			case 24:
				if x.Kraj.After(ponoc) {
					continue // dan još traje
				}
				dan := x.Kraj.In(Zagreb).AddDate(0, 0, -1)
				k := kljuc{x.Izvor, "dnevni"}
				nizovi[k] = append(nizovi[k], arhiva.Redak{PoDanu: true, Vrijednost: x.Oborina,
					Vrijeme: time.Date(dan.Year(), dan.Month(), dan.Day(), 0, 0, 0, 0, time.UTC)})
			}
		}
		for d, pol := range dvanaest {
			if pol.n < 2 {
				continue
			}
			dan, _ := time.Parse("2006-01-02", d)
			k := kljuc{pol.izvor, "dnevni"}
			nizovi[k] = append(nizovi[k], arhiva.Redak{PoDanu: true, Vrijeme: dan, Vrijednost: math.Round(pol.zbroj*10) / 10})
		}
		kljucevi := make([]kljuc, 0, len(nizovi))
		for k := range nizovi {
			kljucevi = append(kljucevi, k)
		}
		sort.Slice(kljucevi, func(a, b int) bool { return kljucevi[a].vrsta < kljucevi[b].vrsta })
		for _, k := range kljucevi {
			if _, err := arhiva.Dopuni(koren, SlivStabla, p.Code, k.izvor, "oborina", k.vrsta, nizovi[k]); err != nil {
				return promijenjene, fmt.Errorf("%s: %w", p.Code, err)
			}
		}
		if len(nizovi) > 0 {
			promijenjene = append(promijenjene, p.Code)
		}
	}
	return promijenjene, nil
}
