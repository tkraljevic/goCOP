package web

import (
	"math"
	"sort"
	"strings"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Crtež lanca postaja za stranicu „O prognozi”: jedna slika umjesto
// odlomaka o tome tko je vrh, tko ulaz i odakle vrhu budućnost. Crta se iz
// namještenih pojasa i iz istih popisa po kojima osvježavanje bira vrhove,
// pa ne može zaostati za računom.

// CvorLanca je jedna postaja na crtežu.
type CvorLanca struct {
	Kod, Naziv string
	X, Y       float64
	Dolje      bool   // natpis u drugom redu
	Vrsta      string // razred boje: karika | vrh-operater | vrh-dnevni | vrh-hu | vrh-at | vrh-hibrid | vrh-mjerenje
	Izvor      string // opis odakle vrhu budućnost; prazno za kariku
}

// VezaLanca je strelica od ulaza prema postaji.
type VezaLanca struct {
	X1, Y1, X2, Y2 float64
	Sporedna       bool // pritoka ili drugi ulaz, isprekidano
}

// LegendaLanca je jedna stavka legende.
type LegendaLanca struct{ Vrsta, Tekst string }

// SlikaLanca je gotov crtež.
type SlikaLanca struct {
	Width, Height int
	Rijeke        []RijekaLanca
	Cvorovi       []CvorLanca
	Veze          []VezaLanca
	Legenda       []LegendaLanca
}

// RijekaLanca je natpis reda.
type RijekaLanca struct {
	Naziv string
	Y     float64
}

var opisiVrsta = map[string]string{
	"vrh-operater": "vrh: model ispuštanja elektrane",
	"vrh-dnevni":   "vrh: naš dnevni model s kišom",
	"vrh-hibrid":   "mađarska prognoza dok je svježa, inače lanac",
	"vrh-hu":       "vrh: mađarska prognoza",
	"vrh-at":       "vrh: austrijska prognoza",
	"vrh-mjerenje": "vrh: drži zadnje mjerenje",
	"karika":       "karika: računa se iz ulaza",
}

// vrstaCvora kaže odakle postaji budućnost, po istim popisima po kojima bira
// osvježavanje.
func vrstaCvora(kod string, imaRacun bool) string {
	if _, ima := prognoza.TudaIspredRacuna[kod]; ima {
		return "vrh-hibrid"
	}
	if imaRacun {
		return "karika"
	}
	for _, o := range prognoza.Operateri {
		if o.Letva == kod {
			return "vrh-operater"
		}
	}
	if prognoza.VrhoviIzDnevnog[kod] {
		return "vrh-dnevni"
	}
	switch prognoza.VrhoviSTudomPrognozom[kod] {
	case prognoza.Podrijetlo:
		return "vrh-hu"
	case prognoza.PodrijetloNOEL:
		return "vrh-at"
	}
	return "vrh-mjerenje"
}

// redUzTok slaže postaje jedne rijeke uzvodno → nizvodno: ulaz uvijek
// prije postaje koju hrani (topološki), a među slobodnima prvi je onaj s
// većim riječnim kilometrom. Postaja bez kilometra dobiva kilometar postaje
// koju hrani, pa stane tik ispred nje; bez ičega ide na kraj.
func redUzTok(kodovi []string, pojasi map[string][]prognoza.Pojas, rkm func(string) float64) []string {
	skup := map[string]bool{}
	for _, k := range kodovi {
		skup[k] = true
	}
	ulazi := map[string][]string{} // postaja → njezini ulazi u istoj rijeci
	hrani := map[string][]string{} // ulaz → postaje koje hrani
	preostalo := map[string]int{}  // koliko ulaza još nije smješteno
	for _, k := range kodovi {
		if ps, ima := pojasi[k]; ima {
			for _, u := range ps[0].Ulazi {
				if skup[u.Letva] && u.Letva != k {
					ulazi[k] = append(ulazi[k], u.Letva)
					hrani[u.Letva] = append(hrani[u.Letva], k)
				}
			}
		}
		preostalo[k] = len(ulazi[k])
	}
	km := map[string]float64{}
	var kmOd func(string, int) float64
	kmOd = func(k string, dubina int) float64 {
		if v, ima := km[k]; ima {
			return v
		}
		v := rkm(k)
		if v < 0 && dubina < 10 {
			for _, d := range hrani[k] {
				if x := kmOd(d, dubina+1); x >= 0 {
					v = x + 0.001
					break
				}
			}
		}
		km[k] = v
		return v
	}
	for _, k := range kodovi {
		kmOd(k, 0)
	}
	var out []string
	smjesteno := map[string]bool{}
	for len(out) < len(kodovi) {
		var kandidati []string
		for _, k := range kodovi {
			if !smjesteno[k] && preostalo[k] == 0 {
				kandidati = append(kandidati, k)
			}
		}
		if len(kandidati) == 0 { // krug u ulazima: uzmi sve preostale po kilometru
			for _, k := range kodovi {
				if !smjesteno[k] {
					kandidati = append(kandidati, k)
				}
			}
		}
		sort.Slice(kandidati, func(i, j int) bool {
			a, b := km[kandidati[i]], km[kandidati[j]]
			if (a < 0) != (b < 0) {
				return a >= 0
			}
			if a != b {
				return a > b
			}
			return kandidati[i] < kandidati[j]
		})
		k := kandidati[0]
		out = append(out, k)
		smjesteno[k] = true
		for _, d := range hrani[k] {
			preostalo[d]--
		}
	}
	return out
}

// slikaLanca slaže crtež: rijeke u redove, postaje uzvodno → nizvodno po
// riječnom kilometru, strelice od ulaza prema postaji.
func slikaLanca(pojasi map[string][]prognoza.Pojas, postaje map[string]models.Station) *SlikaLanca {
	if len(pojasi) == 0 {
		return nil
	}
	letve := map[string]bool{}
	for l, ps := range pojasi {
		letve[l] = true
		for _, u := range ps[0].Ulazi {
			letve[u.Letva] = true
		}
	}
	rijeka := func(kod string) string {
		if st, ima := postaje[kod]; ima && st.Watercourse != "" {
			return st.Watercourse
		}
		return "ostalo"
	}
	ime := func(kod string) string {
		if st, ima := postaje[kod]; ima && st.Name != "" {
			n := st.Name
			if i := strings.Index(n, " ("); i > 0 {
				n = n[:i] // bez države u zagradi, da natpisi stanu
			}
			return n
		}
		return kod
	}
	poRijeci := map[string][]string{}
	for l := range letve {
		poRijeci[rijeka(l)] = append(poRijeci[rijeka(l)], l)
	}
	// Redoslijed rijeka: pritoke iznad glavnih tokova, kako voda teče.
	prednost := map[string]int{"Bednja": 0, "Plitvica": 1, "Mura": 2, "Drava": 3, "Karašica": 4, "Vučica": 5, "Dunav": 6}
	rijeke := make([]string, 0, len(poRijeci))
	for r := range poRijeci {
		rijeke = append(rijeke, r)
	}
	sort.Slice(rijeke, func(i, j int) bool {
		a, ia := prednost[rijeke[i]]
		b, ib := prednost[rijeke[j]]
		if ia != ib {
			return ia
		}
		if a != b {
			return a < b
		}
		return rijeke[i] < rijeke[j]
	})
	rkm := func(kod string) float64 {
		if st, ima := postaje[kod]; ima {
			if v, ok := rijecniKm(st.Stationing); ok {
				return v
			}
		}
		return -1
	}
	s := &SlikaLanca{Width: 1600}
	const lijevo, desno, prviRed, red = 120.0, 60.0, 70.0, 110.0
	poz := map[string][2]float64{}
	y := prviRed
	for _, r := range rijeke {
		kodovi := redUzTok(poRijeci[r], pojasi, rkm)
		s.Rijeke = append(s.Rijeke, RijekaLanca{Naziv: r, Y: y})
		n := len(kodovi)
		korak := 150.0 // rijeka s malo postaja ne rasteže se preko cijele širine
		if n > 1 {
			korak = math.Min(korak, (float64(s.Width)-lijevo-desno)/float64(n-1))
		}
		for i, kod := range kodovi {
			x := lijevo + korak*float64(i)
			poz[kod] = [2]float64{x, y}
			_, imaRacun := pojasi[kod]
			v := vrstaCvora(kod, imaRacun)
			c := CvorLanca{Kod: kod, Naziv: ime(kod), X: x, Y: y, Dolje: i%2 == 1 && n > 8, Vrsta: v}
			if v != "karika" {
				c.Izvor = opisiVrsta[v]
			}
			s.Cvorovi = append(s.Cvorovi, c)
		}
		y += red
	}
	s.Height = int(y - red + 60)
	for l, ps := range pojasi {
		do := poz[l]
		for j, u := range ps[0].Ulazi {
			od := poz[u.Letva]
			s.Veze = append(s.Veze, VezaLanca{X1: od[0], Y1: od[1], X2: do[0], Y2: do[1],
				Sporedna: j > 0 || rijeka(u.Letva) != rijeka(l)})
		}
	}
	vidjeno := map[string]bool{}
	for _, v := range []string{"karika", "vrh-operater", "vrh-dnevni", "vrh-hibrid", "vrh-hu", "vrh-at", "vrh-mjerenje"} {
		for _, c := range s.Cvorovi {
			if c.Vrsta == v && !vidjeno[v] {
				vidjeno[v] = true
				s.Legenda = append(s.Legenda, LegendaLanca{Vrsta: v, Tekst: opisiVrsta[v]})
			}
		}
	}
	return s
}
