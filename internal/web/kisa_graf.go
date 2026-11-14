package web

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Zbirna krivulja kiše na stranici Slivovi (crtež 17e, Odluke 14. 11. 2026.):
// za svaki međusliv iznad praga zbroj kiše od prekjučer do prekosutra, uz
// vodoravne crte pragova za 72 h. SVG se slaže ovdje, pa stranica ne treba
// biblioteku za grafove.

// mjere grafa u jedinicama viewBoxa (330 × 170)
const (
	grafSirina, grafVisina = 330.0, 170.0
	grafLijevo, grafGore   = 8.0, 12.0
	grafDolje              = 24.0 // mjesto za oznake dana
	grafDesno              = 70.0 // mjesto za natpise pragova
)

// TockaGrafa je kraj jednog dana na krivulji
type TockaGrafa struct {
	X, Y      float64
	Ocekivano bool
}

// PragGrafa je vodoravna crta praga s natpisom
type PragGrafa struct {
	Y             float64
	Natpis, Klasa string
}

// DanGrafa je oznaka dana ispod krivulje
type DanGrafa struct {
	X     float64
	Tekst string
	Danas bool
}

// GrafKise je zbirna krivulja jednog međusliva
type GrafKise struct {
	Naziv, URL, Klasa, Rijetko string
	Palo, Ocekivano            string // točke za polyline („x,y x,y …”)
	Tocke                      []TockaGrafa
	Pragovi                    []PragGrafa
	Dani                       []DanGrafa
	SadX, OsY, PragX           float64
	Zbroj, Opis                string
}

var kratkiPragovi = [3]string{"3× god.", "1× god.", "1× u 5 g."}

// grafoviKise: graf za svaki međusliv iznad praga, jača kiša prva
func grafoviKise(k *kisaSlivovaPodaci, sad time.Time) []GrafKise {
	if k == nil {
		return nil
	}
	izdano := k.Izdano
	if izdano.IsZero() {
		izdano = sad
	}
	slivovi := slices.Clone(k.Upozorenja)
	slices.SortStableFunc(slivovi, func(a, b SlivUKisi) int { return cmp.Compare(b.Razina, a.Razina) })
	var out []GrafKise
	for _, s := range slivovi {
		out = append(out, grafKise(s, izdano))
	}
	return out
}

// grafKise slaže krivulju: počinje od nule prije prekjučer, a dan koji niz
// nema dodaje 0 mm (crta ostaje vodoravna)
func grafKise(s SlivUKisi, izdano time.Time) GrafKise {
	g := GrafKise{Naziv: imeSliva(s), URL: "#sliv-" + s.Sliv, Klasa: s.Klasa,
		Rijetko: strings.TrimPrefix(s.Ucestalost, "toliko padne u prosjeku "),
		OsY:     grafVisina - grafDolje, PragX: grafSirina - grafDesno}
	palo, zbroj := zbrojeviDana(s)
	mjerilo := max(najveciPrag(s.Pragovi.Dan3)*1.15, zbroj*1.05, 1)
	y := func(v float64) float64 { return zaokruziGraf(grafGore + (g.OsY-grafGore)*(1-v/mjerilo)) }
	for j, p := range s.Pragovi.Dan3 {
		if p > 0 {
			g.Pragovi = append(g.Pragovi, PragGrafa{Y: y(p), Natpis: kratkiPragovi[j] + " " + brojHRf(p, 0), Klasa: razineKise[j+1][0]})
		}
	}
	krivuljaGrafa(&g, s, izdano, y)
	g.Zbroj = "palo " + brojHRf(palo, 0) + " mm, s očekivanim " + brojHRf(zbroj, 0) + " mm"
	if p := porastRetka(s.Letve); p != "" {
		g.Zbroj += " · " + p
	}
	g.Opis = g.Naziv + ": " + g.Zbroj
	if p := pragRetka(s.Pragovi.Dan3[0]); p != "" {
		g.Opis += "; " + p
	}
	return g
}

// zbrojeviDana: palo od prekjučer do danas i sve s očekivanim
func zbrojeviDana(s SlivUKisi) (palo, zbroj float64) {
	for i, v := range s.Dani {
		if !s.ImaDan[i] {
			continue
		}
		zbroj += v
		if i <= 2 {
			palo += v
		}
	}
	return palo, zbroj
}

// grafX je vodoravni položaj kraja dana j (0 je početak prije prekjučer);
// zadnja točka ne smije dirati natpise pragova
func grafX(j int) float64 {
	return zaokruziGraf(grafLijevo + float64(j)*(grafSirina-grafLijevo-grafDesno-12)/5)
}

// krivuljaGrafa: točke krajeva dana, crta palo do danas, crtkano dalje, i
// oznake dana
func krivuljaGrafa(g *GrafKise, s SlivUKisi, izdano time.Time, y func(float64) float64) {
	c := 0.0
	palo, ocek := []string{fmt.Sprintf("%g,%g", grafX(0), y(0))}, []string(nil)
	for i, v := range s.Dani {
		if s.ImaDan[i] {
			c += v
		}
		t := TockaGrafa{X: grafX(i + 1), Y: y(c), Ocekivano: i > 2}
		g.Tocke = append(g.Tocke, t)
		xy := fmt.Sprintf("%g,%g", t.X, t.Y)
		if i <= 2 {
			palo = append(palo, xy)
		}
		if i >= 2 {
			ocek = append(ocek, xy)
		}
		g.Dani = append(g.Dani, DanGrafa{X: t.X, Tekst: danKise(i-2, izdano), Danas: i == 2})
	}
	g.Palo, g.Ocekivano, g.SadX = strings.Join(palo, " "), strings.Join(ocek, " "), grafX(3)
}

func zaokruziGraf(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
