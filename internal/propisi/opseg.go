package propisi

import (
	"fmt"
	"strings"

	"gocop/internal/models"
)

// Opseg je broj glava, odjeljaka, dijelova Plana, članaka, točaka Plana i
// priloga u propisu.
type Opseg struct {
	Glave, Odjeljci, Dijelovi, Clanci, Tocke, Prilozi int
}

// OpsegIz broji dijelove propisa po vrsti.
func OpsegIz(d []models.DioPropisa) Opseg {
	var o Opseg
	for _, dio := range d {
		o.Dodaj(dio.Vrsta, 1)
	}
	return o
}

// Dodaj pribroji n dijelova zadane vrste. Uvod, završetak i odlomak se ne
// broje. Koristi ga i popis propisa, koji broji dijelove u bazi.
func (o *Opseg) Dodaj(vrsta string, n int) {
	switch vrsta {
	case models.DioGlava:
		o.Glave += n
	case models.DioOdjeljak:
		o.Odjeljci += n
	case models.DioPlana:
		o.Dijelovi += n
	case models.DioClanak:
		o.Clanci += n
	case models.DioTocka:
		o.Tocke += n
	case models.DioPrilog:
		o.Prilozi += n
	}
}

// Opis je opseg riječima, samo s vrstama kojih ima: „10 dijelova, 45 točaka”
// ili „16 glava, 33 odjeljka, 254 članka”. Prazan opseg daje prazno.
func (o Opseg) Opis() string {
	var komadi []string
	for _, v := range []struct {
		n                         int
		jednina, dvojina, mnozina string
	}{
		{o.Glave, "glava", "glave", "glava"},
		{o.Odjeljci, "odjeljak", "odjeljka", "odjeljaka"},
		{o.Dijelovi, "dio", "dijela", "dijelova"},
		{o.Clanci, "članak", "članka", "članaka"},
		{o.Tocke, "točka", "točke", "točaka"},
		{o.Prilozi, "prilog", "priloga", "priloga"},
	} {
		if v.n > 0 {
			komadi = append(komadi, fmt.Sprintf("%d %s", v.n, models.UzBrojHR(v.n, v.jednina, v.dvojina, v.mnozina)))
		}
	}
	return strings.Join(komadi, ", ")
}
