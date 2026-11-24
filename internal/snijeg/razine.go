// Package snijeg slaže snijeg na tlu u alpskim slivovima za prikaz: iz
// SNOWGRID-a (GeoSphere Austria) vrijednost sliva, koliko je rijetka prema
// zimama od 1961., kako se mijenjala u zadnja tri dana i procjenu za idućih
// pet dana iz prognoze snijega i temperature. Prognoza vodostaja ovo ne
// čita: ona ima svoj model snijega (prognoza.ZalihaSnijega).
package snijeg

import (
	"math"
	"sort"
	"time"

	"gocop/internal/prognoza"
)

// Sliv je alpski međusliv koji se prikazuje: oznaka, ime, rijeka uz koju
// stoji na kartici i kraj koji pokriva.
type Sliv struct {
	Oznaka, Naziv, Rijeka, Podrucje string
}

// Alpski su međuslivovi sa snijegom na kartici, redom kartice. Bavarski
// Dunav (O) se ne prikazuje: SNOWGRID pokriva samo Austriju.
var Alpski = []Sliv{
	{"A", "Gornja Drava", "Drava", "Južni i Istočni Tirol, Koruška, slovenska Drava do Borla"},
	{"B", "Mura", "Drava", "Salzburg (Lungau), Štajerska, Slovenija do Letenyea"},
	{"K", "Inn i Salzach", "Dunav", "Švicarska (Engadin), Tirol, Salzburg, Bavarska"},
	{"L", "Traun i Enns", "Dunav", "Gornja Austrija, Salzkammergut, Štajerska"},
}

// Oznake su oznake alpskih međuslivova.
func Oznake() []string {
	out := make([]string, len(Alpski))
	for i, s := range Alpski {
		out[i] = s.Oznaka
	}
	return out
}

// AlpskiSliv vraća alpski međusliv s oznakom.
func AlpskiSliv(oznaka string) (Sliv, bool) {
	for _, s := range Alpski {
		if s.Oznaka == oznaka {
			return s, true
		}
	}
	return Sliv{}, false
}

// Zime su povratna razdoblja razina: toliko snijega bude jednom u 3, 5 i 10
// zima.
var Zime = [3]int{3, 5, 10}

// Pragovi su najveći snijeg zime (mm vode) koji bude jednom u 3, 5 i 10
// zima.
type Pragovi [3]float64

// najmanjeZima je koliko punih zima treba za pragove.
const najmanjeZima = 20

// danaZaZimu je koliko dana s podatkom zima treba da bi bila puna.
const danaZaZimu = 300

// PovratnaRazina je razina koju najveći snijeg zime prijeđe jednom u T zima:
// k = n/T, između k-tog i (k+1)-og najvećeg zimskog maksimuma.
func PovratnaRazina(maksimumi []float64, t float64) float64 {
	n := len(maksimumi)
	if n == 0 || t <= 0 {
		return 0
	}
	x := append([]float64(nil), maksimumi...)
	sort.Sort(sort.Reverse(sort.Float64Slice(x)))
	k := float64(n) / t
	i := int(math.Floor(k))
	switch {
	case i < 1:
		return x[0]
	case i >= n:
		return x[n-1]
	}
	return x[i-1] + (k-float64(i))*(x[i]-x[i-1])
}

// ZimskiMaksimumi su najveći snijeg svake pune zime (1. 8.–31. 7.) prije
// sezone u kojoj je dan; zima s manje od 300 dana podataka nije puna.
func ZimskiMaksimumi(niz map[int64]float64, dan int64) []float64 {
	tekuca := prognoza.PocetakSezone(dan)
	maks, dana := map[int64]float64{}, map[int64]int{}
	for d, v := range niz {
		z := prognoza.PocetakSezone(d)
		if z >= tekuca {
			continue
		}
		maks[z] = math.Max(maks[z], v)
		dana[z]++
	}
	var out []float64
	for z, n := range dana {
		if n >= danaZaZimu {
			out = append(out, maks[z])
		}
	}
	sort.Float64s(out)
	return out
}

// PragoviIz su pragovi iz zimskih maksimuma; bez dovoljno punih zima ih nema.
func PragoviIz(maksimumi []float64) (Pragovi, bool) {
	if len(maksimumi) < najmanjeZima {
		return Pragovi{}, false
	}
	var p Pragovi
	for i, t := range Zime {
		p[i] = math.Round(PovratnaRazina(maksimumi, float64(t)))
	}
	return p, true
}

// Razina je koliko je snijeg rijedak: 0 ispod praga za 3 zime, 1, 2 i 3 od
// praga za 3, 5 i 10 zima.
func Razina(v float64, p Pragovi) int {
	r := 0
	for i, prag := range p {
		if prag > 0 && v >= prag {
			r = i + 1
		}
	}
	return r
}

// Ukljucen kaže je li sliv na kartici u danu do: uključi se kad snijeg
// dosegne prag za 3 zime, a isključi tek kad padne ispod polovice tog praga,
// da kartica ne trepće oko praga. Stanje se vodi od dana od.
func Ukljucen(niz map[int64]float64, prag float64, od, do int64) bool {
	if prag <= 0 {
		return false
	}
	ukljucen := false
	for d := od; d <= do; d++ {
		v, ima := niz[d]
		switch {
		case !ima:
		case v >= prag:
			ukljucen = true
		case v < prag/2:
			ukljucen = false
		}
	}
	return ukljucen
}

// pragTrenda je promjena (mm vode) u tri dana ispod koje snijeg miruje.
const pragTrenda = 5.0

// Trend je kako se snijeg mijenjao u tri dana do dana: „raste”, „miruje” ili
// „topi se”; bez vrijednosti tri dana prije prazno.
func Trend(niz map[int64]float64, dan int64) string {
	sada, ima := niz[dan]
	prije, imaPrije := niz[dan-3]
	switch {
	case !ima || !imaPrije:
		return ""
	case sada-prije >= pragTrenda:
		return "raste"
	case prije-sada >= pragTrenda:
		return "topi se"
	}
	return "miruje"
}

// NizSliva je težinski srednjak točaka po danu, samo od točaka koje taj dan
// imaju vrijednost (austrijski dio sliva).
func NizSliva(nizovi map[string]map[int64]float64, tezine map[string]float64) map[int64]float64 {
	zbroj, tezina := map[int64]float64{}, map[int64]float64{}
	for t, niz := range nizovi {
		w := tezine[t]
		if w <= 0 {
			continue
		}
		for d, v := range niz {
			zbroj[d] += w * v
			tezina[d] += w
		}
	}
	out := make(map[int64]float64, len(zbroj))
	for d, z := range zbroj {
		out[d] = math.Round(z/tezina[d]*10) / 10
	}
	return out
}

// Procjena nastavlja snijeg od vrijednosti pocetna na kraju dana od kroz
// dane od+1 do do po modelu snijega (palo − otopljeno, prognoza.KorakSnijega).
// Dan bez satnih podataka ne mijenja snijeg.
func Procjena(pocetna float64, od, do int64, dani map[int64]prognoza.DanOborine) map[int64]float64 {
	out := map[int64]float64{}
	v := pocetna
	for d := od + 1; d <= do; d++ {
		v, _, _ = prognoza.KorakSnijega(v, dani[d])
		out[d] = math.Round(v*10) / 10
	}
	return out
}

// Dan je dan od 1970. kao datum UTC.
func Dan(d int64) time.Time { return time.Unix(d*86400, 0).UTC() }
