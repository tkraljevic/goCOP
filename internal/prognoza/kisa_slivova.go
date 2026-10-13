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

// PragoviIzNiza računa pragove iz dnevnog niza međusliva. Prag koji se iz
// niza ne da odrediti ostaje 0 mm, a to znači da praga nema (vidi prijeden).
func PragoviIzNiza(n DnevniNiz) (PragoviKise, bool) {
	if len(n) < 3650 {
		return PragoviKise{}, false
	}
	jedan, tri := zbrojeviKise(n)
	p := PragoviKise{Godina: int(math.Round(float64(len(jedan)) / 365.25))}
	for i, r := range RazineKise {
		p.Dan1[i] = pragUcestalosti(jedan, r)
		p.Dan3[i] = pragUcestalosti(tri, r)
	}
	return p, true
}

// zbrojeviKise vraća kišu svakog dana niza i zbroj triju dana za svaki dan
// kojem niz ima i oba dana prije.
func zbrojeviKise(n DnevniNiz) (jedan, tri []float64) {
	dani := make([]int64, 0, len(n))
	for d := range n {
		dani = append(dani, d)
	}
	sort.Slice(dani, func(a, b int) bool { return dani[a] < dani[b] })
	jedan = make([]float64, 0, len(dani))
	tri = make([]float64, 0, len(dani))
	for _, d := range dani {
		jedan = append(jedan, n[d])
		a, ok1 := n[d-1]
		b, ok2 := n[d-2]
		if ok1 && ok2 {
			tri = append(tri, n[d]+a+b)
		}
	}
	return jedan, tri
}

// pragUcestalosti vraća kišu koju niz dosegne u prosjeku puta godišnje, a
// najmanje jednom u cijelom nizu. Niz bez nijednog zbroja triju dana (npr.
// kad fali svaki drugi dan) prag ne daje, pa ostaje 0 mm.
func pragUcestalosti(v []float64, puta float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	k := max(int(math.Round(puta*float64(len(s))/365.25)), 1)
	return s[len(s)-k]
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
		st := kisaSliva(oborine[OborinaKljuc(s)])
		st.Sliv, st.Letve, st.Pragovi = s, letve[s], p
		if imaP && st.Ima {
			st.Razina, st.Zasto, st.Ucestalost = razinaKise(p, st.Palo24, st.Palo72, st.Palo24+st.Dolazi48)
		}
		out = append(out, st)
	}
	return out
}

// kisaSliva vraća kišu međusliva oko sada: palo u zadnja 24 i 72 sata i što
// se očekuje u sljedećih 48 sati, na desetinku milimetra.
func kisaSliva(n DnevniNiz) StanjeSliva {
	v0, ok0 := n[0]
	v1, ok1 := n[-1]
	v2, ok2 := n[-2]
	f1, okf1 := n[1]
	f2, okf2 := n[2]
	st := StanjeSliva{Ima: ok0, Palo24: math.Round(v0*10) / 10}
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
	return st
}

// razinaKise vraća najvišu razinu čiji je prag prijeđen (0 kad nijedan nije),
// zašto i koliko je takva kiša rijetka. Prvo se gleda 24 sata, pa 72 sata
// palo, pa palo s onim što dolazi.
func razinaKise(p PragoviKise, palo24, palo72, ukupno float64) (int, string, string) {
	for i := len(RazineKise) - 1; i >= 0; i-- {
		var zasto string
		switch {
		case prijeden(palo24, p.Dan1[i]):
			zasto = "palo u zadnja 24 sata"
		case prijeden(palo72, p.Dan3[i]):
			zasto = "palo u zadnja 72 sata"
		case prijeden(ukupno, p.Dan3[i]):
			zasto = "palo i očekuje se u 72 sata"
		default:
			continue
		}
		return i + 1, zasto, ucestalostKise(RazineKise[i])
	}
	return 0, "", ""
}

// prijeden kaže je li kiša dosegla prag. Prag od 0 mm znači da praga nema
// (Odluke (13. 10. 2026.)): ispadne u suhom ili nepotpunom nizu, npr. prag
// triju dana kad niz nema nijedna tri uzastopna dana, a dizao bi razinu i
// kad nije palo ništa.
func prijeden(kisa, prag float64) bool {
	return prag > 0 && kisa >= prag
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
