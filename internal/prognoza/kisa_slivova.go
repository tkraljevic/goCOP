package prognoza

// Kiša po slivovima za naslovnu: koliko je palo na kojem međuslivu i koliko
// se još očekuje, prema onome što je za taj međusliv uobičajeno, i na kojim
// će letvama zbog toga porasti voda. Uobičajeno je ERA5 od 1990. (isti niz
// na kojem dnevni model uči); uživo su zadnja 24 i 72 sata do sada i
// prognoza sljedećih 48 sati s Open-Meteo, kao u dnevnom modelu.
//
// Najveća vrijednost u provjeri dnevnog modela je kiša koja tek dolazi, pa
// upozorenje gleda i palo i palo s prognozom zajedno.

import (
	"database/sql"
	"math"
	"sort"
	"sync"
	"time"
)

// RazineKise su koliko dana godišnje u prosjeku padne toliko kiše ili više:
// žuto, narančasto, crveno.
var RazineKise = [3]float64{3, 1, 0.2}

// PragoviKise su pragovi jednog međusliva u mm za zbroj jednog i triju dana,
// po razinama RazineKise.
type PragoviKise struct {
	Dan1, Dan3 [3]float64
	Godina     int // koliko je godina niza iza pragova
}

// StanjeSliva je kiša jednog međusliva sada.
type StanjeSliva struct {
	Sliv                     string
	Palo24, Palo72, Dolazi48 float64
	Ima                      bool // ima li uopće podataka uživo
	Razina                   int  // 0 ništa neuobičajeno, 1 žuto, 2 narančasto, 3 crveno
	Zasto                    string
	Ucestalost               string // koliko je to rijetko, riječima
	Letve                    []string
	Pragovi                  PragoviKise
}

// PragoviIzNiza računa pragove iz dnevnog niza međusliva.
func PragoviIzNiza(n DnevniNiz) (PragoviKise, bool) {
	if len(n) < 3650 {
		return PragoviKise{}, false
	}
	dani := make([]int64, 0, len(n))
	for d := range n {
		dani = append(dani, d)
	}
	sort.Slice(dani, func(a, b int) bool { return dani[a] < dani[b] })
	jedan := make([]float64, 0, len(dani))
	tri := make([]float64, 0, len(dani))
	for _, d := range dani {
		jedan = append(jedan, n[d])
		a, ok1 := n[d-1]
		b, ok2 := n[d-2]
		if ok1 && ok2 {
			tri = append(tri, n[d]+a+b)
		}
	}
	godina := float64(len(jedan)) / 365.25
	p := PragoviKise{Godina: int(math.Round(godina))}
	prag := func(v []float64, puta float64) float64 {
		s := append([]float64(nil), v...)
		sort.Float64s(s)
		k := int(math.Round(puta * float64(len(s)) / 365.25))
		if k < 1 {
			k = 1
		}
		return s[len(s)-k]
	}
	for i, r := range RazineKise {
		p.Dan1[i] = prag(jedan, r)
		p.Dan3[i] = prag(tri, r)
	}
	return p, true
}

// pragoviPredmemorija drži pragove jedan dan: arhiva se mijenja rijetko.
var pragoviPredmemorija struct {
	sync.Mutex
	dan     string
	pragovi map[string]PragoviKise
}

// PragoviSlivova vraća pragove za međuslivove iz arhive (ERA5), računate
// jednom dnevno.
func PragoviSlivova(arhiva *sql.DB, tocke []OborinskaTocka) (map[string]PragoviKise, error) {
	danas := time.Now().Format("2006-01-02")
	pragoviPredmemorija.Lock()
	defer pragoviPredmemorija.Unlock()
	if pragoviPredmemorija.dan == danas && pragoviPredmemorija.pragovi != nil {
		return pragoviPredmemorija.pragovi, nil
	}
	ob, err := DnevneOborine(arhiva, tocke)
	if err != nil {
		return nil, err
	}
	out := map[string]PragoviKise{}
	for s := range SlivoviUPrognozi() {
		if p, ok := PragoviIzNiza(ob[OborinaKljuc(s)]); ok {
			out[s] = p
		}
	}
	pragoviPredmemorija.dan, pragoviPredmemorija.pragovi = danas, out
	return out, nil
}

// StanjeSlivova uspoređuje kišu oko sada (OborineOkoSada: ključ 0 su 24 sata
// do sada, -1 i -2 dani prije, 1 i 2 sljedeća 24 i 48 sati) s pragovima.
// Međuslivovi idu redom kako se pojavljuju u dnevnom modelu (A, B, C…), a
// letve redom niz tok.
func StanjeSlivova(oborine map[string]DnevniNiz, pragovi map[string]PragoviKise) []StanjeSliva {
	letve := SlivoviUPrognozi()
	slivovi := make([]string, 0, len(letve))
	for s := range letve {
		slivovi = append(slivovi, s)
	}
	sort.Strings(slivovi)
	var out []StanjeSliva
	for _, s := range slivovi {
		p, imaP := pragovi[s]
		st := StanjeSliva{Sliv: s, Letve: letve[s], Pragovi: p}
		n := oborine[OborinaKljuc(s)]
		v0, ok0 := n[0]
		v1, ok1 := n[-1]
		v2, ok2 := n[-2]
		f1, okf1 := n[1]
		f2, okf2 := n[2]
		st.Ima = ok0
		st.Palo24 = math.Round(v0*10) / 10
		if ok0 && ok1 && ok2 {
			st.Palo72 = math.Round((v0+v1+v2)*10) / 10
		}
		if okf1 {
			st.Dolazi48 = f1
			if okf2 {
				st.Dolazi48 += f2
			}
			st.Dolazi48 = math.Round(st.Dolazi48*10) / 10
		}
		if imaP && st.Ima {
			ukupno := st.Palo24 + st.Dolazi48
			for i := 2; i >= 0; i-- {
				switch {
				case st.Palo24 >= p.Dan1[i]:
					st.Razina, st.Zasto = i+1, "palo u zadnja 24 sata"
				case st.Palo72 >= p.Dan3[i]:
					st.Razina, st.Zasto = i+1, "palo u zadnja 72 sata"
				case ukupno >= p.Dan3[i]:
					st.Razina, st.Zasto = i+1, "palo i očekuje se u 72 sata"
				default:
					continue
				}
				st.Ucestalost = ucestalostKise(RazineKise[i])
				break
			}
		}
		out = append(out, st)
	}
	return out
}

// ucestalostKise kaže riječima koliko je kiša rijetka
func ucestalostKise(puta float64) string {
	switch {
	case puta >= 2:
		return "toliko padne u prosjeku " + map[bool]string{true: "tri", false: "dva"}[puta >= 3] + " puta godišnje"
	case puta >= 1:
		return "toliko padne u prosjeku jednom godišnje"
	default:
		return "toliko padne u prosjeku jednom u " + map[bool]string{true: "pet", false: "nekoliko"}[math.Abs(1/puta-5) < 0.5] + " godina"
	}
}
