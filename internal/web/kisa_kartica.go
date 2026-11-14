package web

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
)

// Kartica Jače oborine na Naslovnoj (Odluke 14. 11. 2026., crtež 11a):
// lanac međuslivova niz Dravu, Muru i Dunav s kišom u 72 h, a ispod svaki
// međusliv iznad praga s trakom prema pragu i kišom po danima.

// poljeLanca je jedno polje lanca: jedan ili više međuslivova (uzvodni
// Dunav je jedno polje) i kratko ime letve na kojoj završava
type poljeLanca struct {
	slivovi []string
	ime     string
}

// rijekaLanca je red lanca; pritok je red s jednim poljem i opisom gdje
// ulazi u rijeku
type rijekaLanca struct {
	rijeka, pritok string
	polja          []poljeLanca
}

// lanciKise su međuslivovi redom kako voda teče (oznake kao u
// prognoza.DnevniCiljevi)
var lanciKise = []rijekaLanca{
	{rijeka: "Drava", polja: []poljeLanca{{[]string{"A"}, "Borl"}, {[]string{"C"}, "Botovo"}, {[]string{"D"}, "T. Polje"},
		{[]string{"E"}, "Miholjac"}, {[]string{"F"}, "Belišće"}, {[]string{"G"}, "Osijek"}}},
	{rijeka: "Mura", pritok: "ulazi u Dravu kod Botova", polja: []poljeLanca{{[]string{"B"}, "Letenye"}}},
	{rijeka: "Dunav", polja: []poljeLanca{{[]string{"O", "K", "L", "M", "N"}, "uzvodno"}, {[]string{"H"}, "Budimpešta"},
		{[]string{"I"}, "Mohács"}, {[]string{"J"}, "Aljmaš"}}},
}

// PoljeKise je polje lanca na kartici
type PoljeKise struct {
	Ime, Mm, Klasa, URL, Naslov string
}

// RijekaKise je red lanca na kartici
type RijekaKise struct {
	Rijeka, Pritok string
	Polja          []PoljeKise
}

// DioTrake je dan kiše na traci, u postocima širine trake
type DioTrake struct {
	Lijevo, Sirina float64
	Ocekivano      bool
	Naslov         string
}

// CrticaTrake je prag za 72 h na traci
type CrticaTrake struct {
	Lijevo        float64
	Klasa, Naslov string
}

// RedakJaceKise je međusliv iznad praga na kartici
type RedakJaceKise struct {
	Naziv, URL, Klasa, Rijetko string
	Dijelovi                   []DioTrake
	Crtice                     []CrticaTrake
	Opis                       string // traka riječima, za čitač zaslona
	Palo, Ocekivano, Porast    string
}

// KisaNaslovne su podaci tijela kartice Jače oborine
type KisaNaslovne struct {
	Lanac             []RijekaKise
	Retci             []RedakJaceKise
	Vise, ViseMobitel string
}

// danKise je oznaka dana d (-2…2): prozor od 24 h koji završava d dana od
// sata izdanja nosi kraticu dana u tjednu, a dan izdanja je „danas”
func danKise(d int, izdano time.Time) string {
	if d == 0 {
		return "danas"
	}
	return [...]string{"ned", "pon", "uto", "sri", "čet", "pet", "sub"}[izdano.AddDate(0, 0, d).In(models.Zagreb).Weekday()]
}

// lanacKise slaže lanac iz svih međuslivova, i onih bez razine
func lanacKise(slivovi []SlivUKisi) []RijekaKise {
	po := map[string]SlivUKisi{}
	for _, s := range slivovi {
		po[s.Sliv] = s
	}
	var out []RijekaKise
	for _, r := range lanciKise {
		red := RijekaKise{Rijeka: r.rijeka, Pritok: r.pritok}
		for _, p := range r.polja {
			red.Polja = append(red.Polja, poljeKise(p, po))
		}
		out = append(out, red)
	}
	return out
}

// poljeKise: najveća kiša u 72 h i najviša razina međuslivova polja; bez
// podataka crtica
func poljeKise(p poljeLanca, po map[string]SlivUKisi) PoljeKise {
	f := PoljeKise{Ime: p.ime, Mm: "—", URL: "/slivovi#kisa"}
	if len(p.slivovi) == 1 {
		f.URL = "/slivovi#sliv-" + p.slivovi[0]
	}
	najvise, razina, ima := 0.0, 0, false
	var nazivi []string
	for _, o := range p.slivovi {
		s, ok := po[o]
		if !ok || !s.Ima || !s.ImaDan[0] || !s.ImaDan[1] {
			continue
		}
		ima = true
		najvise, razina = math.Max(najvise, s.Palo72), max(razina, s.Razina)
		nazivi = append(nazivi, imeSliva(s))
	}
	if ima {
		f.Mm = brojHRf(najvise, 0)
		f.Klasa = razineKise[razina][0]
		f.Naslov = strings.Join(nazivi, ", ") + ": " + f.Mm + " mm u 72 h"
	}
	return f
}

// imeSliva: naziv iz registra, a bez njega oznaka
func imeSliva(s SlivUKisi) string {
	if s.Naziv != "" {
		return s.Naziv
	}
	return s.Sliv
}

// redakKise slaže redak međusliva iznad praga
func redakKise(s SlivUKisi, izdano time.Time) RedakJaceKise {
	r := RedakJaceKise{Naziv: imeSliva(s), URL: "/slivovi#sliv-" + s.Sliv, Klasa: s.Klasa,
		Rijetko: strings.TrimPrefix(s.Ucestalost, "toliko padne u prosjeku ")}
	r.Dijelovi, r.Crtice = trakaKise(s, izdano)
	r.Palo, r.Ocekivano = kisaRetka(s, izdano)
	r.Porast = porastRetka(s.Letve)
	r.Opis = strings.Join(slices.DeleteFunc([]string{r.Palo, r.Ocekivano, pragRetka(s.Pragovi.Dan3[0])},
		func(t string) bool { return t == "" }), "; ")
	return r
}

// kisaRetka: „palo 52 mm u 72 h: čet 18 · pet 33 · danas 1” i „očekivano
// 15 mm u 48 h: ned 0 · pon 15”; bez jučer ili prekjučer samo 24 h, bez
// sutra ništa očekivano
func kisaRetka(s SlivUKisi, izdano time.Time) (palo, ocekivano string) {
	palo = "palo " + brojHRf(s.Palo24, 0) + " mm u 24 h"
	if s.ImaDan[0] && s.ImaDan[1] {
		palo = "palo " + brojHRf(s.Palo72, 0) + " mm u 72 h: " + daniKise(s, -2, 0, izdano)
	}
	if s.ImaDan[3] {
		ocekivano = "očekivano " + brojHRf(s.Dolazi48, 0) + " mm u 48 h: " + daniKise(s, 1, 2, izdano)
	}
	return palo, ocekivano
}

// daniKise: „čet 18 · pet 33 · danas 1” za dane od–do koje niz ima
func daniKise(s SlivUKisi, od, do int, izdano time.Time) string {
	var d []string
	for i := od; i <= do; i++ {
		if s.ImaDan[i+2] {
			d = append(d, danKise(i, izdano)+" "+brojHRf(s.Dani[i+2], 0))
		}
	}
	return strings.Join(d, " · ")
}

// porastRetka: prva letva međusliva s porastom u dnevnoj prognozi
func porastRetka(letve []PorastLetve) string {
	for _, l := range letve {
		if l.Ima && l.Cm >= 1 {
			return fmt.Sprintf("porast u prognozi: %s +%s cm", l.Naziv, brojHRf(l.Cm, 0))
		}
	}
	return ""
}

// pragRetka: najniži prag za 72 h riječima, za čitač zaslona
func pragRetka(p float64) string {
	if p <= 0 {
		return ""
	}
	return "prag za 72 h " + brojHRf(p, 0) + " mm"
}

// trakaKise: dani jedan za drugim (palo pa očekivano) i crtice pragova za
// 72 h; mjerilo je najviši prag uz malo zraka, ili zbroj dana kad je veći
func trakaKise(s SlivUKisi, izdano time.Time) ([]DioTrake, []CrticaTrake) {
	zbroj := 0.0
	for i, v := range s.Dani {
		if s.ImaDan[i] {
			zbroj += v
		}
	}
	mjerilo := math.Max(najveciPrag(s.Pragovi.Dan3)*1.12, zbroj)
	if mjerilo <= 0 {
		return nil, nil
	}
	posto := func(v float64) float64 { return math.Round(v/mjerilo*1000) / 10 }
	var dijelovi []DioTrake
	lijevo := 0.0
	for i, v := range s.Dani {
		if !s.ImaDan[i] || v <= 0 {
			continue
		}
		d := i - 2
		dijelovi = append(dijelovi, DioTrake{Lijevo: posto(lijevo), Sirina: posto(v), Ocekivano: d > 0,
			Naslov: danKise(d, izdano) + " " + brojHRf(v, 0) + " mm"})
		lijevo += v
	}
	var crtice []CrticaTrake
	for j, p := range s.Pragovi.Dan3 {
		if p <= 0 {
			continue // prag 0 mm znači da praga nema
		}
		crtice = append(crtice, CrticaTrake{Lijevo: posto(p), Klasa: razineKise[j+1][0],
			Naslov: "prag za 72 h: " + rijecimaRazine[j] + ", " + brojHRf(p, 0) + " mm"})
	}
	return dijelovi, crtice
}

var rijecimaRazine = [3]string{"tri puta godišnje", "jednom godišnje", "jednom u pet godina"}

func najveciPrag(v [3]float64) float64 { return math.Max(v[0], math.Max(v[1], v[2])) }

// viseKise: „Više: još 2 · Donji Miholjac → Belišće, Mohács → Aljmaš” za
// retke iza prvih n; prazno kad sve stane
func viseKise(retci []RedakJaceKise, n int) string {
	if len(retci) <= n {
		return ""
	}
	var imena []string
	for _, r := range retci[n:] {
		imena = append(imena, r.Naziv)
	}
	return fmt.Sprintf("Više: još %d · %s", len(retci)-n, strings.Join(imena, ", "))
}

// kisaNaslovne slaže tijelo kartice; upozorenja su već složena jača prva
func kisaNaslovne(p kisaSlivovaPodaci, upozorenja []SlivUKisi, sad time.Time) KisaNaslovne {
	izdano := p.Izdano
	if izdano.IsZero() {
		izdano = sad
	}
	k := KisaNaslovne{Lanac: lanacKise(p.Slivovi)}
	for _, s := range upozorenja {
		k.Retci = append(k.Retci, redakKise(s, izdano))
	}
	k.Vise, k.ViseMobitel = viseKise(k.Retci, NajviseRedaka), viseKise(k.Retci, NajviseRedakaMobitel)
	return k
}
