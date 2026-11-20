package web

import (
	"fmt"
	"math"
	"slices"
	"strconv"
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

// PoljeKise je polje lanca na kartici. Ocekivano („+36”) je kiša u
// sljedećih 48 h kad je tek ona podigla razinu polja: boja tada ne dolazi od
// pale kiše, pa broj uz nju mora reći zašto je polje obojeno.
type PoljeKise struct {
	Ime, Mm, Ocekivano, Klasa, URL, Naslov string
}

// zastoOcekivano je razlog razine kad prag prijeđe pala kiša zajedno s
// očekivanom, a zasto24 kad ga prijeđe već kiša zadnja 24 h
// (prognoza.razinaKise)
const (
	zastoOcekivano = "palo i očekuje se u 72 sata"
	zasto24        = "palo u zadnja 24 sata"
)

// prozoriOznake kratko kažu iz kojeg je razdoblja razina na oznaci retka:
// bez toga „jednom godišnje” uz traku za 72 h koja ne doseže nijednu crticu
// izgleda kao greška (oznaku je dala kiša u 24 h)
var prozoriOznake = map[string]string{zasto24: "u 24 h", "palo u zadnja 72 sata": "u 72 h", zastoOcekivano: "u 72 h s prognozom"}

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

// CrticaTrake je prag na traci: za 72 h, a za 24 h kad je razinu dala kiša
// zadnja 24 h
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

// oznakaProzora je oznaka prozora od 24 h u retku kartice: prošli dani
// kraticom dana, zadnja 24 h tako, a sljedeći satom kraja („do ned 13 h”).
// Prozor ne završava u ponoć, pa je „ned 0 mm” zvučalo kao da u nedjelju
// neće kišiti, a kiša je dolazila u nedjelju navečer.
func oznakaProzora(d int, izdano time.Time) string {
	switch {
	case d == 0:
		return "zadnja 24 h"
	case d < 0:
		return danKise(d, izdano)
	}
	kraj := izdano.Add(time.Duration(24*d) * time.Hour).In(models.Zagreb)
	return "do " + danKise(d, izdano) + " " + strconv.Itoa(kraj.Hour()) + " h"
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
	var podigla *SlivUKisi // međusliv koji je dao razinu polja
	for _, o := range p.slivovi {
		s, ok := po[o]
		if !ok || !s.Ima || !s.ImaDan[0] || !s.ImaDan[1] {
			continue
		}
		ima = true
		najvise = math.Max(najvise, s.Palo72)
		if s.Razina > razina {
			razina, podigla = s.Razina, &s
		}
		nazivi = append(nazivi, imeSliva(s))
	}
	if ima {
		f.Mm = brojHRf(najvise, 0)
		f.Klasa = razineKise[razina][0]
		f.Naslov = strings.Join(nazivi, ", ") + ": " + f.Mm + " mm u 72 h"
		dodajOcekivano(&f, podigla)
	}
	return f
}

// dodajOcekivano: kad je razinu polja podigla tek očekivana kiša, polje uz
// palu pokazuje i nju
func dodajOcekivano(f *PoljeKise, podigla *SlivUKisi) {
	if podigla == nil || podigla.Zasto != zastoOcekivano {
		return
	}
	f.Ocekivano = "+" + brojHRf(podigla.Dolazi48, 0)
	f.Naslov += ", očekivano " + brojHRf(podigla.Dolazi48, 0) + " mm u 48 h"
}

// ImaOcekivano kaže ima li u lancu polje obojeno zbog očekivane kiše, da
// natpis ispod lanca objasni broj s plusom
func (k KisaNaslovne) ImaOcekivano() bool {
	for _, r := range k.Lanac {
		for _, p := range r.Polja {
			if p.Ocekivano != "" {
				return true
			}
		}
	}
	return false
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
		Rijetko: spoji(" ", strings.TrimPrefix(s.Ucestalost, "toliko padne u prosjeku "), prozoriOznake[s.Zasto])}
	r.Dijelovi, r.Crtice = trakaKise(s, izdano)
	r.Palo, r.Ocekivano = kisaRetka(s, izdano)
	r.Porast = porastRetka(s)
	r.Opis = strings.Join(slices.DeleteFunc([]string{r.Palo, r.Ocekivano, pragRetka(s)},
		func(t string) bool { return t == "" }), "; ")
	return r
}

// kisaRetka: „palo 52 mm u 72 h: čet 18 mm · pet 33 mm · zadnja 24 h 1 mm” i
// „očekivano 15 mm u 48 h: do ned 14 h 0 mm · do pon 14 h 15 mm”, a uz kišu
// iza 48 h još „, zatim do uto 14 h 25 mm”; bez jučer ili prekjučer samo
// 24 h, bez sutra ništa očekivano
func kisaRetka(s SlivUKisi, izdano time.Time) (palo, ocekivano string) {
	palo = "palo " + brojHRf(s.Palo24, 0) + " mm u 24 h"
	if s.ImaDan[0] && s.ImaDan[1] {
		palo = "palo " + brojHRf(s.Palo72, 0) + " mm u 72 h: " + daniKise(s, -2, 0, izdano)
	}
	if s.ImaDan[3] {
		ocekivano = "očekivano " + brojHRf(s.Dolazi48, 0) + " mm u 48 h: " + daniKise(s, 1, 2, izdano)
		if s.ImaPoslije48 && s.Poslije48 >= 1 {
			ocekivano += ", zatim " + oznakaProzora(3, izdano) + " " + brojHRf(s.Poslije48, 0) + " mm"
		}
	}
	return palo, ocekivano
}

// daniKise: „čet 18 mm · pet 33 mm · zadnja 24 h 1 mm” za dane od–do koje niz ima
func daniKise(s SlivUKisi, od, do int, izdano time.Time) string {
	var d []string
	for i := od; i <= do; i++ {
		if s.ImaDan[i+2] {
			d = append(d, oznakaProzora(i, izdano)+" "+brojHRf(s.Dani[i+2], 0)+" mm")
		}
	}
	return strings.Join(d, " · ")
}

// krajMedjusliva je letva na kraju međusliva, ili prva nizvodno kad na kraju
// nema letve u dnevnoj prognozi (Borl, Mohács, uzvodni Dunav). Kiša međusliva
// ulazi u prognozu više letava, a redak porasta pokazuje samo ovu
// (Odluke 16. 11. 2026.).
var krajMedjusliva = map[string]string{
	"A": "botovo", "B": "letenye", "C": "botovo", "D": "terezino-polje", "E": "donji-miholjac",
	"F": "belisce", "G": "osijek", "H": "budapest", "I": "batina", "J": "aljmas",
	"K": "komarom", "L": "komarom", "M": "komarom", "N": "komarom", "O": "komarom",
}

// porastRetka: porast letve na kraju međusliva u dnevnoj prognozi; bez
// porasta te letve ništa. Kad je letva nepoznata ili je nema u nizu, prva
// letva međusliva s porastom.
func porastRetka(s SlivUKisi) string {
	if kraj, ok := krajMedjusliva[s.Sliv]; ok {
		for _, l := range s.Letve {
			if l.Letva == kraj && l.Ima {
				return opisPorasta(l)
			}
		}
	}
	for _, l := range s.Letve {
		if l.Ima && l.Cm >= 1 {
			return opisPorasta(l)
		}
	}
	return ""
}

// opisPorasta: „u prognozi Batina raste s −55 na 36 cm do čet. 26. 11.”, a
// ispod 1 cm ništa. Kaže od čega se broji i do čega dolazi, a ne „porast u
// prognozi: Batina +91 cm”: uz međusliv je to zvučalo kao da je toliko
// podigla njegova kiša, a porast nosi i voda odozgo.
func opisPorasta(l PorastLetve) string {
	if l.Cm < 1 {
		return ""
	}
	kada := "do " + strconv.Itoa(l.Dan) + ". dana"
	if !l.Kad.IsZero() {
		kada = "do " + strings.TrimPrefix(l.Kada(), "u ")
	}
	return "u prognozi " + l.Naziv + " raste s " + cmSPredznakom(l.Od) + " na " + cmSPredznakom(l.Od+l.Cm) + " cm " + kada
}

// cmSPredznakom: cijeli centimetri s pravim minusom („−55”), bez „−0”
func cmSPredznakom(v float64) string {
	r := math.Round(v)
	if r == 0 {
		return "0"
	}
	return strings.Replace(brojHRf(r, 0), "-", "−", 1)
}

// pragRetka: najniži prag razdoblja trake riječima, za čitač zaslona
func pragRetka(s SlivUKisi) string {
	if s.Zasto == zasto24 {
		return pragZa(s.Pragovi.Dan1[0], "24 h")
	}
	return pragZa(s.Pragovi.Dan3[0], "72 h")
}

// pragZa: „prag za 72 h 42 mm”; bez praga ništa
func pragZa(p float64, za string) string {
	if p <= 0 {
		return ""
	}
	return "prag za " + za + " " + brojHRf(p, 0) + " mm"
}

// trakaKise: dani jedan za drugim (palo pa očekivano) i crtice pragova za
// 72 h; mjerilo je najviši prag uz malo zraka, ili zbroj dana kad je veći.
// Kad je razinu dala kiša zadnja 24 h, traka pokazuje samo nju i pragove za
// 24 h, da crtica koju je prešla bude ispod kraja trake.
func trakaKise(s SlivUKisi, izdano time.Time) ([]DioTrake, []CrticaTrake) {
	pragovi, za, dani := razdobljeTrake(s)
	zbroj := 0.0
	for i := dani[0]; i <= dani[1]; i++ {
		if s.ImaDan[i] {
			zbroj += s.Dani[i]
		}
	}
	mjerilo := math.Max(najveciPrag(pragovi)*1.12, zbroj)
	if mjerilo <= 0 {
		return nil, nil
	}
	posto := func(v float64) float64 { return math.Round(v/mjerilo*1000) / 10 }
	dijelovi := dijeloviTrake(s, dani, izdano, posto)
	var crtice []CrticaTrake
	for j, p := range pragovi {
		if p <= 0 {
			continue // prag 0 mm znači da praga nema
		}
		crtice = append(crtice, CrticaTrake{Lijevo: posto(p), Klasa: razineKise[j+1][0],
			Naslov: "prag za " + za + ": " + rijecimaRazine[j] + ", " + brojHRf(p, 0) + " mm"})
	}
	return dijelovi, crtice
}

// razdobljeTrake: pragovi, njihovo razdoblje i dani (indeksi u Dani) koje
// traka pokazuje: svi dani i pragovi za 72 h, a samo zadnja 24 h i pragovi
// za 24 h kad je razinu dala kiša zadnja 24 h
func razdobljeTrake(s SlivUKisi) ([3]float64, string, [2]int) {
	if s.Zasto == zasto24 {
		return s.Pragovi.Dan1, "24 h", [2]int{2, 2}
	}
	return s.Pragovi.Dan3, "72 h", [2]int{0, len(s.Dani) - 1}
}

// dijeloviTrake: dani razdoblja jedan za drugim, bez dana bez kiše
func dijeloviTrake(s SlivUKisi, dani [2]int, izdano time.Time, posto func(float64) float64) []DioTrake {
	var out []DioTrake
	lijevo := 0.0
	for i := dani[0]; i <= dani[1]; i++ {
		v := s.Dani[i]
		if !s.ImaDan[i] || v <= 0 {
			continue
		}
		d := i - 2
		out = append(out, DioTrake{Lijevo: posto(lijevo), Sirina: posto(v), Ocekivano: d > 0,
			Naslov: oznakaProzora(d, izdano) + " " + brojHRf(v, 0) + " mm"})
		lijevo += v
	}
	return out
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
