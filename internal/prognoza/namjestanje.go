package prognoza

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
)

// Namještanje računa iz arhive: za svaku letvu koja se prognozira traže se
// kašnjenja ulaza pri kojima se nizovi najbolje slažu, pa se kroz sve pojase
// vodnosti povuče jedan izlomljen, ali neprekinut pravac.
//
// Pojasi se dijele po percentilima glavnog ulaza, ne po apsolutnim brojkama —
// tako granice odgovaraju njegovu režimu, a ne našoj predodžbi o tome što je
// velika voda.
//
// Kašnjenje se traži po pojasu jer se doista mijenja s razinom. Nagib se ne
// traži po pojasu odvojeno: pojas je uzak, pa bi regresija na njemu namještala
// šum, a pravci susjednih pojasa se na granici ne bi sastali — val koji raste
// dobio bi skok u prognozi kakvog u rijeci nema.

// NajveciPomak je dokle se traži za glavni tok; dulje od toga nijedna naša
// dionica ne traje. Pritoci se traže kraće jer su bliže.
var (
	NajveciPomak        = 72
	NajveciPomakPritoka = 48
)

// NajvecaSirina je najširi prozor kojim se ulaz može zagladiti. Rijeka kratke
// valove guši: dnevni val hidroelektrane nosi na Donjoj Dubravi 21,6 m³/s
// promjene po satu, na Terezinu Polju još 4,1, a na Vrbovki 1,3 cm — Belišće se
// nikad nije pomaknulo više od 9 cm u satu. Pomak i množenje val samo prenesu,
// pa bi prognoza Belišću davala poskoke od pola metra kakve ta letva ne poznaje.
// Prozor gleda unatrag, kako i spremnica radi — i ne troši doseg.
const NajvecaSirina = 49

// Pojasi su granice po percentilima. Gusti su pri vrhu jer se ondje ponašanje
// mijenja: na Batini → Aljmaš pomak raste s 4 na 38 sati, jer se Kopački rit
// puni i val uspori.
var Pojasi = [][2]float64{
	{0.00, 0.33}, {0.33, 0.67}, {0.67, 0.85}, {0.85, 0.95}, {0.95, 1.00},
}

// NamjestiDo je prvi sat (od epohe) koji namještanje više ne vidi; nula znači
// bez granice. Služi usporedbi s tuđim prognozama: model koji je namješten i
// na valu na kojem se uspoređuje unaprijed zna odgovor.
var NamjestiDo int64

// NamjestiBez su razdoblja (sat od epohe, [od, do)) koja namještanje ne vidi.
// Tako se model provjerava na valu koji nije vidio, a da mu se ne uzme sve
// što je došlo poslije — u valovima je pojas visoke vode, a njih je malo.
var NamjestiBez [][2]int64

func doGranice(n map[int64]float64) map[int64]float64 {
	if NamjestiDo == 0 && len(NamjestiBez) == 0 {
		return n
	}
	for t := range n {
		if NamjestiDo != 0 && t >= NamjestiDo {
			delete(n, t)
			continue
		}
		for _, r := range NamjestiBez {
			if t >= r[0] && t < r[1] {
				delete(n, t)
				break
			}
		}
	}
	return n
}

// NizIzArhive čita satni niz iz spojenog niza arhive: sat od epohe → vrijednost.
func NizIzArhive(db *sql.DB, letva, velicina string) (map[int64]float64, error) {
	r, err := db.Query(`SELECT vrijeme, vrijednost FROM spoj
		WHERE letva = ? AND velicina = ? AND korak = 'satni'`, letva, velicina)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := map[int64]float64{}
	for r.Next() {
		var t int64
		var v float64
		if err := r.Scan(&t, &v); err != nil {
			return nil, err
		}
		out[t/3600] = v
	}
	return out, r.Err()
}

func koliko(db *sql.DB, letva, velicina string) int {
	var n int
	db.QueryRow(`SELECT count(*) FROM spoj WHERE letva=? AND velicina=? AND korak='satni'`,
		letva, velicina).Scan(&n)
	return n
}

type ulazNiz struct {
	ime      string
	velicina string
	najdulje int
	od       int64     // prvi sat koji niz pokriva
	zbroj    []float64 // prefiksni zbroj, zbog prosjeka po prozoru
	broj     []int     // koliko ih je u tom rasponu doista bilo
}

// noviUlazNiz slaže niz u gusti oblik, da se prosjek po prozoru dobije u dva
// oduzimanja umjesto u onoliko čitanja kolika je širina.
func noviUlazNiz(ime, velicina string, niz map[int64]float64, najdulje int) ulazNiz {
	u := ulazNiz{ime: ime, velicina: velicina, najdulje: najdulje}
	if len(niz) == 0 {
		return u
	}
	prvi, zadnji := int64(math.MaxInt64), int64(math.MinInt64)
	for t := range niz {
		if t < prvi {
			prvi = t
		}
		if t > zadnji {
			zadnji = t
		}
	}
	n := int(zadnji-prvi) + 1
	u.od = prvi
	u.zbroj = make([]float64, n+1)
	u.broj = make([]int, n+1)
	for i := 0; i < n; i++ {
		v, ima := niz[prvi+int64(i)]
		u.zbroj[i+1] = u.zbroj[i]
		u.broj[i+1] = u.broj[i]
		if ima {
			u.zbroj[i+1] += v
			u.broj[i+1]++
		}
	}
	return u
}

// prosjek je srednja vrijednost prozora koji završava u satu t. Prozor mora
// biti pun: rupa u njemu značila bi da se prosjek računa iz manje vode nego
// što je bilo.
func (u ulazNiz) prosjek(t int64, sirina int) (float64, bool) {
	a := int(t-u.od) - sirina + 1
	b := int(t - u.od)
	if sirina < 1 || a < 0 || b+1 >= len(u.zbroj) {
		return 0, false
	}
	if u.broj[b+1]-u.broj[a] != sirina {
		return 0, false
	}
	return (u.zbroj[b+1] - u.zbroj[a]) / float64(sirina), true
}

// pun javlja pokriva li niz bez rupe sve sate koje bi ijedno kašnjenje i ijedna
// širina prozora zatražili. Bez toga se kašnjenja uspoređuju na različitim
// skupovima sati, pa pobijedi ono kojem su sati lakši, a ne ono koje val doista
// treba: Letenye je tako sjeo na 48 sati, točno na rub pretrage.
func (u ulazNiz) pun(t int64) bool {
	a := int(t-u.od) - u.najdulje - NajvecaSirina + 1
	b := int(t - u.od)
	if a < 0 || b+1 >= len(u.broj) {
		return false
	}
	return u.broj[b+1]-u.broj[a] == b+1-a
}

// samoPuni zadržava sate u kojima zadani ulaz podnosi svako kašnjenje i svaku
// širinu prozora.
func samoPuni(u ulazNiz, sati []int64) []int64 {
	out := make([]int64, 0, len(sati))
	for _, t := range sati {
		if u.pun(t) {
			out = append(out, t)
		}
	}
	return out
}

// Izvor je jedna uzvodna letva i veličina u kojoj se uzima.
//
// Veličina se zadaje, ne pogađa. Protok je na gornjoj Dravi neusporedivo
// bolji jer se korito ispod lanca hidroelektrana produbljuje, pa vodostaj kroz
// desetljeća mijenja značenje: Novo Virje iz vodostaja drži r 0,49–0,61, a iz
// protoka 0,87–0,97. Na Dunavu je obrnuto — ondje je vodostaj bolji. A Vrbovka,
// Moslavina i Osijek protok uopće nemaju, pa im izbora ni nema.
type Izvor struct {
	Letva    string
	Velicina string
}

// NamjestiLetvu mjeri kako se jedna letva slaže sa svojim uzvodnim ulazima.
// Prvi ulaz je glavni tok: po njemu se dijele pojasi i po njemu se pravac
// lomi. Ostali ulaze pravocrtno, svaki sa svojim kašnjenjem.
func NamjestiLetvu(arhiva *sql.DB, ciljna, vel string, izvori []Izvor) ([]Pojas, error) {
	if len(izvori) == 0 {
		return nil, fmt.Errorf("%s: nijedan ulaz", ciljna)
	}
	cilj, err := NizIzArhive(arhiva, ciljna, vel)
	if err != nil {
		return nil, err
	}
	cilj = doGranice(cilj)
	if len(cilj) < 5000 {
		return nil, fmt.Errorf("%s: samo %d satnih vrijednosti u %s", ciljna, len(cilj), vel)
	}
	ulazi := make([]ulazNiz, len(izvori))
	for i, iz := range izvori {
		najdulje := NajveciPomak
		if i > 0 {
			najdulje = NajveciPomakPritoka
		}
		n, err := NizIzArhive(arhiva, iz.Letva, iz.Velicina)
		if err != nil {
			return nil, err
		}
		n = doGranice(n)
		if len(n) < 5000 {
			return nil, fmt.Errorf("%s: %s ima samo %d satnih vrijednosti u %s",
				ciljna, iz.Letva, len(n), iz.Velicina)
		}
		ulazi[i] = noviUlazNiz(iz.Letva, iz.Velicina, n, najdulje)
	}

	var sati []int64
	for t := range cilj {
		sati = append(sati, t)
	}
	sort.Slice(sati, func(i, j int) bool { return sati[i] < sati[j] })

	// Pojasi se mjere u glavnom ulazu, pomaknutom za jedno kašnjenje koje
	// vrijedi za sve. Po pojasu se kašnjenje poslije traži iznova, ali granice
	// moraju stajati prije toga — inače bi svaki pojas pomicao vlastiti rub.
	pocetni := make([]int, len(ulazi))
	sirine := make([]int, len(ulazi))
	for i := range sirine {
		sirine[i] = 1
	}
	pocetni[0] = najboljiSam(cilj, ulazi[0], samoPuni(ulazi[0], sati))
	var poredak []float64
	for _, t := range sati {
		if v, ima := ulazi[0].prosjek(t-int64(pocetni[0]), 1); ima {
			poredak = append(poredak, v)
		}
	}
	if len(poredak) < 5000 {
		return nil, fmt.Errorf("%s ← %s: samo %d zajedničkih sati", ciljna, izvori[0].Letva, len(poredak))
	}
	sort.Float64s(poredak)
	cvorovi := Cvorovi(poredak)
	if len(cvorovi) < 2 {
		return nil, fmt.Errorf("%s ← %s: glavni ulaz je ravan", ciljna, izvori[0].Letva)
	}

	// Kašnjenja sporednih ulaza traže se jednom, na svim satima. Ona su
	// svojstvo pritoka, a ne vodnosti glavnog toka: traže li se po pojasu,
	// Letenye ispadne 47 sati u jednom pojasu i 0 u sljedećem, jer je njegov
	// vodostaj kroz dva dana toliko autokoreliran da je cilj po tom kašnjenju
	// gotovo ravan — pa se bira šum. Po pojasu ostaje samo glavni tok.
	svi := make([]int, len(ulazi))
	for i := range svi {
		svi[i] = i
	}
	globalni, globalneSirine := najboljiLagovi(cilj, ulazi, sati, pocetni, sirine, svi)
	if globalni == nil {
		return nil, fmt.Errorf("%s: kašnjenja se nisu dala izmjeriti", ciljna)
	}

	// Po pojasu: kašnjenje glavnog toka.
	type pojasSati struct {
		od, do float64
		lagovi []int
		sati   []int64
	}
	var pojasi []pojasSati
	for i := 0; i+1 < len(cvorovi); i++ {
		od, do := cvorovi[i], cvorovi[i+1]
		zadnji := i+2 == len(cvorovi)
		var uPojasu []int64
		for _, t := range sati {
			v, ima := ulazi[0].prosjek(t-int64(pocetni[0]), 1)
			if ima && v >= od && (v < do || (zadnji && v <= do)) {
				uPojasu = append(uPojasu, t)
			}
		}
		if len(uPojasu) < 2000 {
			continue
		}
		// Po pojasu se traži samo kašnjenje glavnog toka. Širina prozora je
		// svojstvo dionice, ne vodnosti, pa ostaje kakvu je dao opći prolaz.
		lagovi, _ := najboljiLagovi(cilj, ulazi, uPojasu, globalni, globalneSirine, []int{0})
		if lagovi == nil {
			continue
		}
		pojasi = append(pojasi, pojasSati{od: od, do: do, lagovi: lagovi, sati: uPojasu})
	}
	if len(pojasi) == 0 {
		return nil, fmt.Errorf("%s: nijedan pojas nije dao račun", ciljna)
	}

	// Pa jedan izlomljen pravac kroz sve pojase odjednom. Svaki pojas nosi
	// svoje sate i svoje kašnjenje, ali čvorove dijele — pa se pravci na
	// granicama sastaju. Sporedni ulazi imaju jedan nagib za sve pojase: to je
	// njihova vlastita krivulja protoka, koja o vodnosti glavnog toka ne ovisi.
	m := len(cvorovi)
	p := m + len(ulazi) - 1
	A, b := prazneJednadzbe(p)
	red := make([]float64, p)
	x := make([]float64, len(ulazi))
	for _, pj := range pojasi {
		for _, t := range pj.sati {
			y, ok := uzorak(cilj, ulazi, pj.lagovi, globalneSirine, t, x)
			if !ok {
				continue
			}
			napuniRed(red, cvorovi, x)
			dodaj(A, b, red, y)
		}
	}
	k := rijesiSRezervom(A, b)

	var out []Pojas
	for _, pj := range pojasi {
		i := sort.SearchFloat64s(cvorovi, pj.od)
		nagib := (k[i+1] - k[i]) / (cvorovi[i+1] - cvorovi[i])
		pojas := Pojas{
			Letva: ciljna, Velicina: vel, Od: pj.od, Do: pj.do,
			Odsjecak: k[i] - nagib*cvorovi[i],
		}
		for j, u := range ulazi {
			n := nagib
			if j > 0 {
				n = k[m+j-1]
			}
			pojas.Ulazi = append(pojas.Ulazi, Ulaz{
				Letva: u.ime, Velicina: u.velicina, PomakH: pj.lagovi[j],
				Sirina: globalneSirine[j], Nagib: n,
			})
		}
		pojas.R, pojas.Rasap, pojas.Sati = kakoDrzi(cilj, ulazi, pj.lagovi, globalneSirine, pj.sati, pojas)
		out = append(out, pojas)
	}
	return out, nil
}

// kakoDrzi mjeri koliko namješteni pojas pogađa: korelacija računatog s
// izmjerenim, i standardno odstupanje promašaja.
func kakoDrzi(cilj map[int64]float64, ulazi []ulazNiz, lagovi, sirine []int, sati []int64, p Pojas) (float64, float64, int) {
	x := make([]float64, len(ulazi))
	var racunato, mjereno []float64
	for _, t := range sati {
		y, ok := uzorak(cilj, ulazi, lagovi, sirine, t, x)
		if !ok {
			continue
		}
		v, err := p.Racunaj(x)
		if err != nil {
			continue
		}
		racunato = append(racunato, v)
		mjereno = append(mjereno, y)
	}
	if len(mjereno) == 0 {
		return 0, 0, 0
	}
	var s float64
	for i := range mjereno {
		o := mjereno[i] - racunato[i]
		s += o * o
	}
	return korelacija(racunato, mjereno), math.Sqrt(s / float64(len(mjereno))), len(mjereno)
}

// uzorak vadi vrijednosti svih ulaza — svaku kao prosjek prozora koji završava
// u njezinu satu — i cilj u ciljnom satu.
func uzorak(cilj map[int64]float64, ulazi []ulazNiz, lagovi, sirine []int, t int64, x []float64) (float64, bool) {
	y, ok := cilj[t]
	if !ok {
		return 0, false
	}
	for i := range ulazi {
		v, ima := ulazi[i].prosjek(t-int64(lagovi[i]), sirine[i])
		if !ima {
			return 0, false
		}
		x[i] = v
	}
	return y, true
}

// najboljiSam traži kašnjenje jednog ulaza samog, po korelaciji.
func najboljiSam(cilj map[int64]float64, u ulazNiz, sati []int64) int {
	najR, naj := math.Inf(-1), 0
	for pom := 0; pom <= u.najdulje; pom++ {
		var a, c []float64
		for _, t := range sati {
			if v, ima := u.prosjek(t-int64(pom), 1); ima {
				a = append(a, v)
				c = append(c, cilj[t])
			}
		}
		if len(a) < 2000 {
			continue
		}
		if r := korelacija(a, c); r > najR {
			najR, naj = r, pom
		}
	}
	return naj
}

// Sirine su širine prozora koje se isprobavaju. Neparne su da prozor ima
// sredinu, a rijetke pri vrhu jer se razlika između 33 i 35 sati ne vidi.
var Sirine = []int{1, 3, 5, 7, 9, 13, 17, 21, 25, 31, 37, 43, 49}

// najboljiLagovi traži kašnjenja i širine prozora zadanih ulaza naizmjence:
// jedno se mijenja dok ostalo stoji, pa se krug ponovi. Potpuna pretraga svih
// kombinacija stajala bi 73^n·13^n prolaza, a dobiva se isto — kašnjenje i
// prigušenje se međusobno slabo vuku.
func najboljiLagovi(cilj map[int64]float64, ulazi []ulazNiz, sati []int64,
	pocetni, pocetneSirine, koje []int) ([]int, []int) {
	lagovi := append([]int(nil), pocetni...)
	sirine := append([]int(nil), pocetneSirine...)
	if _, ok := ocjena(cilj, ulazi, lagovi, sirine, sati); !ok {
		return nil, nil
	}
	for krug := 0; krug < 3; krug++ {
		promjena := false
		for _, i := range koje {
			// Svaki se ulaz traži na satima koje podnosi pri svakom kašnjenju
			// i svakoj širini, pa se kandidati uspoređuju na istome. Inače
			// pobijedi ono kojem su sati lakši, a ne ono koje val doista treba.
			usporedivi := samoPuni(ulazi[i], sati)
			najR2, ok := ocjena(cilj, ulazi, lagovi, sirine, usporedivi)
			if !ok {
				continue
			}
			stariPom, stariSir := lagovi[i], sirine[i]
			najPom := stariPom
			for pom := 0; pom <= ulazi[i].najdulje; pom++ {
				lagovi[i] = pom
				if r2, ok := ocjena(cilj, ulazi, lagovi, sirine, usporedivi); ok && r2 > najR2 {
					najR2, najPom = r2, pom
				}
			}
			lagovi[i] = najPom
			najSir := stariSir
			osnova := pokriva(ulazi[i], sati, najPom, 1)
			for _, sir := range Sirine {
				if pokriva(ulazi[i], sati, najPom, sir)*4 < osnova*3 {
					continue
				}
				sirine[i] = sir
				if r2, ok := ocjena(cilj, ulazi, lagovi, sirine, usporedivi); ok && r2 > najR2 {
					najR2, najSir = r2, sir
				}
			}
			sirine[i] = najSir
			if najPom != stariPom || najSir != stariSir {
				promjena = true
			}
		}
		if !promjena {
			break
		}
	}
	return lagovi, sirine
}

// pokriva broji sate u kojima ulaz uz zadano kašnjenje ima pun prozor.
//
// Širina se bira na satima koji podnose svaku širinu, a to su na Letenyeu samo
// godine s mjerenjem svaki sat — ranije je VITUKI javljao rjeđe. Ondje je
// prozor od 21 sat bio malo bolji, pa je pobijedio, a u pravom računu je
// odbacio četiri petine povijesti: Botovu su ostala dva pojasa od pet, a
// visoka voda nijedan. Zato širina smije uzeti najviše četvrtinu sati koje
// pokriva prozor od jednog sata.
func pokriva(u ulazNiz, sati []int64, pom, sirina int) int {
	n := 0
	for _, t := range sati {
		if _, ima := u.prosjek(t-int64(pom), sirina); ima {
			n++
		}
	}
	return n
}

// ocjena vraća R² pravocrtne regresije cilja na sve ulaze pri zadanim
// kašnjenjima. Služi samo za biranje kašnjenja — konačni pravac se lomi.
func ocjena(cilj map[int64]float64, ulazi []ulazNiz, lagovi, sirine []int, sati []int64) (float64, bool) {
	p := len(ulazi) + 1
	A, b := prazneJednadzbe(p)
	red := make([]float64, p)
	red[p-1] = 1
	x := red[:p-1]
	var sy, syy float64
	n := 0
	for _, t := range sati {
		y, ok := uzorak(cilj, ulazi, lagovi, sirine, t, x)
		if !ok {
			continue
		}
		dodaj(A, b, red, y)
		sy += y
		syy += y * y
		n++
	}
	if n < 2000 {
		return 0, false
	}
	k := rijesiSRezervom(A, b)
	var kb float64
	for i := range k {
		kb += k[i] * b[i]
	}
	sst := syy - sy*sy/float64(n)
	if sst <= 0 {
		return 0, false
	}
	return 1 - (syy-kb)/sst, true
}

// napuniRed gradi red matrice: glavni ulaz kroz šatorske funkcije čvorova, pa
// sporedni ulazi pravocrtno. Šatori zbrojeni daju jedinicu, pa slobodni član
// ne treba posebno.
func napuniRed(red, cvorovi, x []float64) {
	m := len(cvorovi)
	for i := range red {
		red[i] = 0
	}
	i, u := udio(cvorovi, x[0])
	red[i] = 1 - u
	red[i+1] = u
	for j := 1; j < len(x); j++ {
		red[m+j-1] = x[j]
	}
}

func prazneJednadzbe(p int) ([][]float64, []float64) {
	A := make([][]float64, p)
	for i := range A {
		A[i] = make([]float64, p)
	}
	return A, make([]float64, p)
}

func dodaj(A [][]float64, b, red []float64, y float64) {
	for i, ri := range red {
		if ri == 0 {
			continue
		}
		b[i] += ri * y
		for j, rj := range red {
			if rj != 0 {
				A[i][j] += ri * rj
			}
		}
	}
}

// Cvorovi su granice pojasa iz poredanog niza glavnog ulaza. Granice koje se
// poklapaju ispadaju: na letvi koja dugo stoji na istoj vodi dva percentila
// mogu dati isti broj, a pojas bez širine nema nagiba.
func Cvorovi(poredani []float64) []float64 {
	c := []float64{percentil(poredani, Pojasi[0][0])}
	for _, p := range Pojasi {
		v := percentil(poredani, p[1])
		if v > c[len(c)-1] {
			c = append(c, v)
		}
	}
	return c
}

// SpojeniPravac namješta neprekinut izlomljen pravac kroz zadane čvorove i
// vraća njegovu vrijednost u svakom čvoru. Između dva čvora pravac je ravan,
// a u čvoru se lomi — pa je ono što izađe iz jednog pojasa točno ono što ulazi
// u sljedeći.
func SpojeniPravac(cvorovi, x, y []float64) []float64 {
	A, b := prazneJednadzbe(len(cvorovi))
	red := make([]float64, len(cvorovi))
	for k := range x {
		napuniRed(red, cvorovi, x[k:k+1])
		dodaj(A, b, red, y[k])
	}
	return rijesiSRezervom(A, b)
}

// udio kaže u kojem je odsječku vrijednost i koliko je daleko od njegova
// lijevog čvora. Izvan ruba nastavlja se odsječkom koji je najbliži.
func udio(cvorovi []float64, v float64) (int, float64) {
	zadnji := len(cvorovi) - 2
	i := sort.SearchFloat64s(cvorovi, v) - 1
	if i < 0 {
		i = 0
	}
	if i > zadnji {
		i = zadnji
	}
	return i, (v - cvorovi[i]) / (cvorovi[i+1] - cvorovi[i])
}

// rijesiSRezervom rješava A·k = b. Nepoznanica koja nije vidjela dovoljno sati
// inače ostavi sustav bez rješenja; mrvica na dijagonali je veže uz susjede
// umjesto da sve sruši.
//
// Radi na svojim preslikama: eliminacija inače prepiše i lijevu i desnu
// stranu, pa bi pozivatelj koji poslije posegne za b dobio prerađene brojke.
// Upravo se to dogodilo ocjeni — SSE joj je ispadao veći od ukupnog rasapa, R²
// negativan, i kašnjenje je onda biralo najveći broj koji mu je dopušten.
func rijesiSRezervom(ulazA [][]float64, ulazB []float64) []float64 {
	n := len(ulazB)
	A := make([][]float64, n)
	for i := range A {
		A[i] = append([]float64(nil), ulazA[i]...)
	}
	b := append([]float64(nil), ulazB...)
	var trag float64
	for i := 0; i < n; i++ {
		trag += A[i][i]
	}
	for i := 0; i < n; i++ {
		A[i][i] += 1e-9 * trag / float64(n)
	}
	for k := 0; k < n; k++ {
		naj := k
		for i := k + 1; i < n; i++ {
			if math.Abs(A[i][k]) > math.Abs(A[naj][k]) {
				naj = i
			}
		}
		A[k], A[naj] = A[naj], A[k]
		b[k], b[naj] = b[naj], b[k]
		if A[k][k] == 0 {
			continue
		}
		for i := k + 1; i < n; i++ {
			f := A[i][k] / A[k][k]
			if f == 0 {
				continue
			}
			for j := k; j < n; j++ {
				A[i][j] -= f * A[k][j]
			}
			b[i] -= f * b[k]
		}
	}
	v := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		s := b[i]
		for j := i + 1; j < n; j++ {
			s -= A[i][j] * v[j]
		}
		if A[i][i] != 0 {
			v[i] = s / A[i][i]
		}
	}
	return v
}

// korelacija je Pearsonov r.
func korelacija(x, y []float64) float64 {
	n := float64(len(x))
	if n < 2 {
		return 0
	}
	var mx, my float64
	for i := range x {
		mx += x[i]
		my += y[i]
	}
	mx, my = mx/n, my/n
	var sxx, syy, sxy float64
	for i := range x {
		dx, dy := x[i]-mx, y[i]-my
		sxx += dx * dx
		syy += dy * dy
		sxy += dx * dy
	}
	if sxx == 0 || syy == 0 {
		return 0
	}
	return sxy / math.Sqrt(sxx*syy)
}

func percentil(poredani []float64, p float64) float64 {
	if len(poredani) == 0 {
		return 0
	}
	i := int(float64(len(poredani)) * p)
	if i >= len(poredani) {
		i = len(poredani) - 1
	}
	return poredani[i]
}
