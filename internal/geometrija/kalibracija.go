package geometrija

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Sidro je javno poznata točka na toku. Rkm je službena stacionaža, a Tocka
// je njezin zemljopisni položaj. Sidra su namjerno odvojena od postaja: tako
// se geometrija može kalibrirati prije nego što se provjeravaju položaji
// pojedinih uređaja.
type Sidro struct {
	Rkm   float64    `json:"rkm"`
	Tocka [2]float64 `json:"tocka"`
	Izvor string     `json:"izvor"`
}

type kalibracijskoSidro struct {
	uzduz float64
	rkm   float64
}

// Kalibracija preslikava duljinu po OSM liniji u službeni rkm. Između svakog
// para susjednih sidara koristi se zasebna linearna interpolacija. Izvan prvog
// i zadnjeg sidra vrijednost se ne nagađa.
type Kalibracija struct {
	sidra []kalibracijskoSidro
}

// NovaKalibracija provjerava sidra i priprema segmentni model. Linija mora
// biti u smjeru toka, ali rkm može rasti ili padati (Dunav/Drava imaju rkm koji
// padaju prema ušću u OSM smjeru izvora prema ušću).
func NovaKalibracija(linija [][2]float64, sidra []Sidro) (Kalibracija, error) {
	if len(linija) < 2 {
		return Kalibracija{}, errors.New("linija mora imati barem dvije točke")
	}
	if len(sidra) < 2 {
		return Kalibracija{}, errors.New("potrebna su barem dva rkm sidra")
	}

	for _, xy := range linija {
		if !valjanaTocka(xy) {
			return Kalibracija{}, errors.New("neispravna koordinata linije")
		}
	}
	model := Kalibracija{}
	for _, sidro := range sidra {
		if math.IsNaN(sidro.Rkm) || math.IsInf(sidro.Rkm, 0) || !valjanaTocka(sidro.Tocka) {
			return Kalibracija{}, errors.New("neispravno rkm sidro")
		}
		p, err := Projektiraj(linija, sidro.Tocka)
		if err != nil {
			return Kalibracija{}, err
		}
		model.sidra = append(model.sidra, kalibracijskoSidro{uzduz: p.UzduzKM, rkm: sidro.Rkm})
	}
	sort.Slice(model.sidra, func(i, j int) bool { return model.sidra[i].uzduz < model.sidra[j].uzduz })
	for i := 1; i < len(model.sidra); i++ {
		prethodno, sada := model.sidra[i-1], model.sidra[i]
		if sada.uzduz-prethodno.uzduz < 0.001 {
			return Kalibracija{}, fmt.Errorf("rkm sidra su preblizu na liniji: %.3f km", sada.uzduz)
		}
		if math.Abs(sada.rkm-prethodno.rkm) < 0.001 {
			return Kalibracija{}, fmt.Errorf("susjedna rkm sidra imaju jednaku vrijednost %.3f", sada.rkm)
		}
		if i > 1 && (sada.rkm-prethodno.rkm)*(prethodno.rkm-model.sidra[i-2].rkm) <= 0 {
			return Kalibracija{}, errors.New("rkm sidra nisu monotona duž linije")
		}
	}
	return model, nil
}

// RKM vraća kalibrirani rkm za položaj po liniji. Drugi rezultat je false
// izvan područja koje pokrivaju sidra.
func (k Kalibracija) RKM(uzduzKM float64) (float64, bool) {
	if math.IsNaN(uzduzKM) || math.IsInf(uzduzKM, 0) || len(k.sidra) < 2 || uzduzKM < k.sidra[0].uzduz || uzduzKM > k.sidra[len(k.sidra)-1].uzduz {
		return 0, false
	}
	i := sort.Search(len(k.sidra), func(i int) bool { return k.sidra[i].uzduz >= uzduzKM })
	if i == 0 {
		return k.sidra[0].rkm, true
	}
	if i == len(k.sidra) {
		i = len(k.sidra) - 1
	}
	a, b := k.sidra[i-1], k.sidra[i]
	udio := (uzduzKM - a.uzduz) / (b.uzduz - a.uzduz)
	return a.rkm + udio*(b.rkm-a.rkm), true
}

// Uzduz vraća položaj na OSM liniji za zadani kalibrirani rkm. Ne ekstrapolira
// izvan prvog i zadnjeg sidra.
func (k Kalibracija) Uzduz(rkm float64) (float64, bool) {
	if math.IsNaN(rkm) || math.IsInf(rkm, 0) || len(k.sidra) < 2 {
		return 0, false
	}
	for i := 1; i < len(k.sidra); i++ {
		a, b := k.sidra[i-1], k.sidra[i]
		minR, maxR := a.rkm, b.rkm
		if minR > maxR {
			minR, maxR = maxR, minR
		}
		if rkm < minR || rkm > maxR {
			continue
		}
		udio := (rkm - a.rkm) / (b.rkm - a.rkm)
		return a.uzduz + udio*(b.uzduz-a.uzduz), true
	}
	return 0, false
}

func valjanaTocka(xy [2]float64) bool {
	return !math.IsNaN(xy[0]) && !math.IsNaN(xy[1]) && math.Abs(xy[0]) <= 180 && math.Abs(xy[1]) <= 90
}

// TockaNaUzduz vraća točku na izvornoj liniji za njezinu udaljenost od prvog
// vrha. Ako je položaj izvan linije, vraća najbliži kraj.
func TockaNaUzduz(linija [][2]float64, uzduzKM float64) [2]float64 {
	if len(linija) == 0 {
		return [2]float64{}
	}
	if uzduzKM <= 0 {
		return linija[0]
	}
	preostalo := uzduzKM
	for i := 0; i+1 < len(linija); i++ {
		segment := haversineKM(linija[i], linija[i+1])
		if preostalo <= segment {
			udio := 0.0
			if segment > 0 {
				udio = preostalo / segment
			}
			return [2]float64{
				linija[i][0] + udio*(linija[i+1][0]-linija[i][0]),
				linija[i][1] + udio*(linija[i+1][1]-linija[i][1]),
			}
		}
		preostalo -= segment
	}
	return linija[len(linija)-1]
}
