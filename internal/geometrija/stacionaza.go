package geometrija

import (
	"errors"
	"math"
)

const polumjerZemljeKM = 6371.0088

// Projekcija opisuje položaj zemljopisne točke uzduž polilinije. UzduzKM se
// mjeri od prve točke linije, UdaljenostKM je najkraća udaljenost od linije,
// a UkupnoKM duljina cijele polilinije.
type Projekcija struct {
	UzduzKM      float64
	UdaljenostKM float64
	UkupnoKM     float64
}

// DuljinaKM vraća geodetsku duljinu polilinije zadane parovima [lon, lat].
func DuljinaKM(linija [][2]float64) float64 {
	var ukupno float64
	for i := 0; i+1 < len(linija); i++ {
		ukupno += haversineKM(linija[i], linija[i+1])
	}
	return ukupno
}

// Projektiraj pronalazi najbliže mjesto točke na poliliniji. Udio unutar
// segmenta računa se u lokalnoj ravnini, a duljina toka Haversineovom
// formulom; tako duga rijeka ne nasljeđuje izobličenje jedne projekcije.
func Projektiraj(linija [][2]float64, tocka [2]float64) (Projekcija, error) {
	if len(linija) < 2 {
		return Projekcija{}, errors.New("linija mora imati barem dvije točke")
	}

	najbliza := math.Inf(1)
	var uzduz, prijedeno, ukupno float64
	for i := 0; i+1 < len(linija); i++ {
		a, b := linija[i], linija[i+1]
		segment := haversineKM(a, b)
		ukupno += segment

		srednjaSirina := (a[1] + b[1] + tocka[1]) / 3 * math.Pi / 180
		k := math.Cos(srednjaSirina)
		dx, dy := (b[0]-a[0])*k, b[1]-a[1]
		px, py := (tocka[0]-a[0])*k, tocka[1]-a[1]
		nazivnik := dx*dx + dy*dy
		udio := 0.0
		if nazivnik > 0 {
			udio = (px*dx + py*dy) / nazivnik
			udio = math.Max(0, math.Min(1, udio))
		}
		q := [2]float64{a[0] + udio*(b[0]-a[0]), a[1] + udio*(b[1]-a[1])}
		udaljenost := haversineKM(tocka, q)
		if udaljenost < najbliza {
			najbliza = udaljenost
			uzduz = prijedeno + udio*segment
		}
		prijedeno += segment
	}
	return Projekcija{UzduzKM: uzduz, UdaljenostKM: najbliza, UkupnoKM: ukupno}, nil
}

func haversineKM(a, b [2]float64) float64 {
	lat1, lat2 := a[1]*math.Pi/180, b[1]*math.Pi/180
	dlat := lat2 - lat1
	dlon := (b[0] - a[0]) * math.Pi / 180
	x := math.Sin(dlat/2)*math.Sin(dlat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dlon/2)*math.Sin(dlon/2)
	return 2 * polumjerZemljeKM * math.Asin(math.Sqrt(x))
}
