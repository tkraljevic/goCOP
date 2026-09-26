package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
)

// Uzdužni profil: kako se voda mijenja uzduž toka, od uzvodne letve prema ušću.
//
// Crta se odstupanje od današnjeg stanja, u centimetrima, a ne apsolutna kota.
// U kotama se val ne vidi: Drava pada 49 metara od Donje Dubrave do Osijeka, a
// najveća promjena u dva dana nosi tridesetak centimetara — sedam tisućinki
// visine crteža. Crta za sutra legne na današnju i crtež ne pokazuje ono zbog
// čega postoji. Oduzme li se pad, isti val zauzme pola plohe.
//
// Kota se pritom ne gubi: stoji uz svaku letvu, ispod njezina imena.
//
// Val se vidi kako putuje: crta za sutra leži iznad nule ondje gdje val stiže,
// a ispod nje ondje odakle je otišao. Svaki doseg ima svoju crtu, od
// najsvjetlije (sutra) do najtamnije (šesti dan), a uz crtež ide i klizač
// vremena kojim se ista slika gleda sat po sat, od mjerenja prije dva dana do
// kraja prognoze — tako se vidi da val doista putuje nizvodno.

// LetvaProfila je jedna letva na uzdužnom profilu.
type LetvaProfila struct {
	Letva, Naziv string
	Rkm          float64
	KotaNule     float64 // m, HVRS71
	SadaCm       float64 // zadnje izmjereno
	ImaSada      bool
	Cm           map[int]float64    // doseg u satima → prognozirani vodostaj
	Granice      map[int][2]float64 // doseg → donja i gornja granica
	Pragovi      map[string]float64 // prep | regular | emerg → cm
	Niz          map[int]float64    // sat prema izdanju (SatOd … SatDo) → vodostaj, za klizač
	Razina       string             // faza obrane danas: prep | regular | emerg | crit
	Usce         bool               // nije letva nego kraj pritoke s vrijednostima letve glavnog toka: u krivulji jest, natpisa nema
	Akumulacija  bool               // razina akumulacije uz branu: kota nad morem, točka koja se s klizačem diže i spušta, u krivulju vala ne ulazi
}

// ProfilCrta je jedna crta uzduž toka.
type ProfilCrta struct {
	Naziv string
	Class string
	Doseg int
	Put   string
	Pojas string // granice prognoze, kao zatvorena ploha
}

// TockaUzduznog je jedna letva na crtežu, s natpisom.
type TockaUzduznog struct {
	Naziv  string
	X, Y   float64
	Kota   string
	Cm     string
	Sidro  string
	Dolje  bool   // natpis u drugom redu, da se susjedi ne preklapaju
	Razina string // faza obrane danas, boja točke
}

// UzduzniProfil je gotov crtež jednog toka.
type UzduzniProfil struct {
	Ime                     string
	Oznaka                  string // za id kartice: ime bez razmaka i dijakritike
	Width, Height           int
	Lijevo, Desno, Vrh, Dno float64
	Crte                    []ProfilCrta
	Pragovi                 []ProfilCrta
	Tocke                   []TockaUzduznog
	YTicks                  []ChartTick
	XTicks                  []ChartTick
	NulaY                   float64 // današnje stanje: vodoravna crta na nuli
	Opis                    string
	Pad                     string // koliko voda pada od prve do zadnje letve
	Usca                    []UsceProfila
	Brane                   []BranaProfila
	SatOd, SatDo            int // raspon klizača, sati prema izdanju
	Niz                     template.JS
}

func (p *UzduzniProfil) LijevoX() float64 { return p.Lijevo }
func (p *UzduzniProfil) DesnoX() float64  { return float64(p.Width) - p.Desno }
func (p *UzduzniProfil) DnoY() float64    { return float64(p.Height) - p.Dno }
func (p *UzduzniProfil) OsY() float64     { return p.Lijevo - 8 }
func (p *UzduzniProfil) OsX() float64     { return float64(p.Height) - 8 }

// dosezniProfila su prognoze koje se crtaju uz današnje stanje: satni lanac do
// 96 sati, pa dnevni model za peti i šesti dan. Boje idu od najsvjetlije do
// najtamnije, kako doseg raste.
var dosezniProfila = []int{24, 48, 72, 96, 120, 144}

// nazivDosega piše doseg kako ga dežurni izgovara: sati do četvrtog dana, dalje dani.
func nazivDosega(d int) string {
	if d > 96 && d%24 == 0 {
		return fmt.Sprintf("za %d d", d/24)
	}
	return fmt.Sprintf("za %d h", d)
}

var pragoviProfila = []struct{ kljuc, naziv, class string }{
	{"prep", "pripremna", "prep"},
	{"regular", "redovna", "regular"},
	{"emerg", "izvanredna", "emerg"},
}

// NajmanjiRaspon drži mjerilo poštenim na mirnoj vodi: bez njega bi dva
// centimetra šuma ispunila plohu i izgledala kao val.
const NajmanjiRaspon = 20.0

// KolikoDalekoPrag kaže koliko daleko prag smije biti da ga se još crta,
// mjereno u širinama vala. Pri maloj vodi pripremna obrana je tri metra iznad,
// pa bi jedna njezina crta spljoštila cijeli crtež; kad voda naraste, sama uđe
// u sliku — i to je trenutak kad je i treba vidjeti.
const KolikoDalekoPrag = 2.0

// KlizacOd je koliko sati unatrag klizač vremena seže: dva dana mjerenja,
// da se vidi odakle je val došao.
const KlizacOd = -48

// razmakNatpisa je najmanji razmak dviju letvi (u točkama crteža) pri kojem
// im imena još stanu jedno uz drugo u istom redu.
const razmakNatpisa = 170.0

// crtajUzduzni slaže profil jednog toka. Letve bez kote nule ili bez
// stacionaže ispadaju: bez njih se ne zna ni gdje su ni koliko visoko.
// BranaUlaz je brana na toku koji se crta: okomita crta na svojem
// kilometru, da se vidi gdje val prolazi kroz akumulaciju i elektranu.
type BranaUlaz struct {
	Naziv string
	Rkm   float64
}

// BranaProfila je brana na crtežu.
type BranaProfila struct {
	Naziv string
	X     float64
	Sidro string
	Dolje bool // natpis u drugom redu, da se susjedne brane ne preklapaju
}

func crtajUzduzni(ime string, letve []LetvaProfila, usca []UsceUlaz, brane ...BranaUlaz) *UzduzniProfil {
	var korisne []LetvaProfila
	var pravih int
	for _, l := range letve {
		if (l.KotaNule != 0 || l.Akumulacija) && (l.Rkm != 0 || l.Usce) && l.ImaSada {
			korisne = append(korisne, l)
			if !l.Usce && !l.Akumulacija {
				pravih++
			}
		}
	}
	if pravih < 2 {
		return nil
	}
	// Nizvodno ide udesno, a rkm nizvodno pada.
	sort.Slice(korisne, func(i, j int) bool { return korisne[i].Rkm > korisne[j].Rkm })
	// Akumulacija ulazi samo među letvama: ona iznad prve letve ne pripada crtežu.
	{
		var prvaRkm, zadnjaRkm float64
		for i := range korisne {
			if !korisne[i].Usce && !korisne[i].Akumulacija {
				if prvaRkm == 0 {
					prvaRkm = korisne[i].Rkm
				}
				zadnjaRkm = korisne[i].Rkm
			}
		}
		var ost []LetvaProfila
		for _, l := range korisne {
			if l.Akumulacija && (l.Rkm > prvaRkm || l.Rkm < zadnjaRkm) {
				continue
			}
			ost = append(ost, l)
		}
		korisne = ost
	}

	p := &UzduzniProfil{Ime: ime, Oznaka: oznakaImena(ime), Width: 1600, Height: 560,
		Lijevo: 86, Desno: 96, Vrh: 64, Dno: 76, SatOd: KlizacOd}

	najOd, najDo := 0.0, 0.0
	uzmi := func(cm float64) {
		najOd, najDo = math.Min(najOd, cm), math.Max(najDo, cm)
	}
	for _, l := range korisne {
		for _, d := range dosezniProfila {
			if v, ima := l.Cm[d]; ima {
				uzmi(v - l.SadaCm)
			}
			if g, ima := l.Granice[d]; ima {
				uzmi(g[0] - l.SadaCm)
				uzmi(g[1] - l.SadaCm)
			}
		}
		// I ono što klizač pokazuje mora stati u sliku: val koji je prošao
		// prije dva dana dio je iste priče.
		for h, v := range l.Niz {
			uzmi(v - l.SadaCm)
			if h > p.SatDo {
				p.SatDo = h
			}
		}
	}
	if najDo-najOd < NajmanjiRaspon {
		sredina := (najOd + najDo) / 2
		najOd, najDo = sredina-NajmanjiRaspon/2, sredina+NajmanjiRaspon/2
	}
	// Prag ulazi u sliku tek kad je blizu. Mjeri se prema samom valu, a ne
	// prema rasponu koji raste kako pragovi ulaze — inače svaki primljeni prag
	// propusti sljedeći, pa pri srednjoj vodi uđu sva tri i crtež se opet
	// spljošti. Redom od najnižeg: kad pripremna ne stane, ne stanu ni ostali.
	valOd, valDo := najOd, najDo
	sirina := valDo - valOd
	var uSlici []int
	for i, pr := range pragoviProfila {
		odmaci, sve := odmaciPraga(korisne, pr.kljuc)
		if !sve || len(odmaci) == 0 {
			continue
		}
		staje := true
		for _, o := range odmaci {
			if o < valOd-KolikoDalekoPrag*sirina || o > valDo+KolikoDalekoPrag*sirina {
				staje = false
				break
			}
		}
		if !staje {
			break
		}
		for _, o := range odmaci {
			uzmi(o)
		}
		uSlici = append(uSlici, i)
	}

	rub := (najDo - najOd) / 10
	najOd, najDo = najOd-rub, najDo+rub

	plotW := float64(p.Width) - p.Lijevo - p.Desno
	plotH := float64(p.Height) - p.Vrh - p.Dno
	prava := func(i int) LetvaProfila { // krajnja prava letva, bez točke ušća i akumulacije
		for ; i >= 0 && i < len(korisne); i += 1 - 2*boolInt(i == len(korisne)-1) {
			if !korisne[i].Usce && !korisne[i].Akumulacija {
				return korisne[i]
			}
		}
		return korisne[0]
	}
	odRkm, doRkm := prava(0).Rkm, prava(len(korisne)-1).Rkm
	// Ušće među letvama uvijek je u slici. Ušće malo izvan krajnjih letvi
	// uđe u sliku i produži crtež do sebe samo kad s druge strane ima letvi
	// na pregledu — inače bi Plitvica i Bednja iznad Botova samo gomilale
	// natpise. Daleko ušće ne pripada ovom crtežu.
	dopust := (odRkm - doRkm) * KolikoIzvanZaUsce
	var uscaUSlici []UsceUlaz
	for _, u := range usca {
		izvan := u.Rkm > odRkm || u.Rkm < doRkm
		if u.Rkm > odRkm+dopust || u.Rkm < doRkm-dopust || (izvan && !u.Vezano) {
			continue
		}
		odRkm, doRkm = math.Max(odRkm, u.Rkm), math.Min(doRkm, u.Rkm)
		uscaUSlici = append(uscaUSlici, u)
	}
	raspon := odRkm - doRkm
	if raspon <= 0 {
		raspon = 1
	}
	xOf := func(rkm float64) float64 { return p.Lijevo + (odRkm-rkm)/raspon*plotW }
	yOf := func(cm float64) float64 { return p.Vrh + (najDo-cm)/(najDo-najOd)*plotH }
	p.NulaY = yOf(0)

	for i, d := range dosezniProfila {
		c := ProfilCrta{Naziv: nazivDosega(d), Class: fmt.Sprintf("h%d", i+1), Doseg: d}
		c.Put = crtaOdstupanja(korisne, xOf, yOf,
			func(l LetvaProfila) (float64, bool) { v, ima := l.Cm[d]; return v - l.SadaCm, ima })
		if c.Put == "" {
			continue
		}
		c.Pojas = plohaGranica(korisne, d, xOf, yOf)
		p.Crte = append(p.Crte, c)
	}
	for _, i := range uSlici {
		pr := pragoviProfila[i]
		put := crtaOdstupanja(korisne, xOf, yOf, func(l LetvaProfila) (float64, bool) {
			v, ima := l.Pragovi[pr.kljuc]
			return v - l.SadaCm, ima
		})
		if put != "" {
			p.Pragovi = append(p.Pragovi, ProfilCrta{Naziv: pr.naziv, Class: pr.class, Put: put})
		}
	}

	// Natpisi: letve koje stoje preblizu (Sotin, Mohovo, Ilok) dobiju drugi
	// red, da se imena ne preklapaju.
	zadnjiGore, zadnjiDolje := math.Inf(-1), math.Inf(-1)
	for i, l := range korisne {
		if l.Usce {
			continue
		}
		sidro := "middle"
		if i == 0 {
			sidro = "start"
		} else if i == len(korisne)-1 || korisne[i+1].Usce {
			sidro = "end"
		}
		x := xOf(l.Rkm)
		// Prvi red dok stane; inače drugi; kad ne stane ni u jedan, onaj u
		// kojem je susjed dalje (Sotin, Mohovo, Ilok na dvadeset kilometara).
		dolje := false
		switch {
		case x-zadnjiGore >= razmakNatpisa:
		case x-zadnjiDolje >= razmakNatpisa:
			dolje = true
		default:
			dolje = x-zadnjiDolje > x-zadnjiGore
		}
		if dolje {
			zadnjiDolje = x
		} else {
			zadnjiGore = x
		}
		t := TockaUzduznog{
			Naziv: l.Naziv, X: x, Y: p.NulaY, Sidro: sidro, Dolje: dolje, Razina: l.Razina,
			Kota: brojHRf(l.KotaNule+l.SadaCm/100, 2), Cm: brojHRf(l.SadaCm, 0),
		}
		if l.Akumulacija {
			t.Cm = "" // kota nad morem bez nule letve: samo metri
		}
		p.Tocke = append(p.Tocke, t)
		if !l.Akumulacija {
			p.XTicks = append(p.XTicks, ChartTick{Pos: x, Label: brojHRf(l.Rkm, 1), Anchor: sidro})
		}
	}

	sort.Slice(uscaUSlici, func(i, j int) bool { return uscaUSlici[i].Rkm > uscaUSlici[j].Rkm })
	zadnjiGore, zadnjiDolje = math.Inf(-1), math.Inf(-1)
	for _, u := range uscaUSlici {
		x := xOf(u.Rkm)
		sidro := "middle"
		if x-p.Lijevo < 60 {
			sidro = "start"
		} else if float64(p.Width)-p.Desno-x < 60 {
			sidro = "end"
		}
		dolje := false
		switch {
		case x-zadnjiGore >= razmakNatpisa:
		case x-zadnjiDolje >= razmakNatpisa:
			dolje = true
		default:
			dolje = x-zadnjiDolje > x-zadnjiGore
		}
		if dolje {
			zadnjiDolje = x
		} else {
			zadnjiGore = x
		}
		p.Usca = append(p.Usca, UsceProfila{Naziv: u.Naziv, Tekst: u.Tekst, X: x, Sidro: sidro, Dolje: dolje})
	}

	// Brana ulazi u sliku samo među letvama: brana iznad prve letve ne
	// pripada crtežu, jer voda iznad nje nije na njemu.
	sort.Slice(brane, func(i, j int) bool { return brane[i].Rkm > brane[j].Rkm })
	zadnjiGore, zadnjiDolje = math.Inf(-1), math.Inf(-1)
	for _, b := range brane {
		if b.Rkm > odRkm || b.Rkm < doRkm {
			continue
		}
		x := xOf(b.Rkm)
		sidro := "middle"
		if x-p.Lijevo < 60 {
			sidro = "start"
		} else if float64(p.Width)-p.Desno-x < 60 {
			sidro = "end"
		}
		dolje := x-zadnjiGore < razmakNatpisa && x-zadnjiDolje >= razmakNatpisa
		if dolje {
			zadnjiDolje = x
		} else {
			zadnjiGore = x
		}
		p.Brane = append(p.Brane, BranaProfila{Naziv: b.Naziv, X: x, Sidro: sidro, Dolje: dolje})
	}

	korak := niceStep((najDo - najOd) / 5)
	for v := math.Ceil(najOd/korak) * korak; v <= najDo; v += korak {
		p.YTicks = append(p.YTicks, ChartTick{Pos: yOf(v), Label: brojHRf(v, 0)})
	}
	prva, zadnja := prava(0), prava(len(korisne)-1)
	pad := (prva.KotaNule + prva.SadaCm/100) - (zadnja.KotaNule + zadnja.SadaCm/100)
	p.Pad = fmt.Sprintf("%s, %s → %s: vodno lice pada %s m na %s km", ime, prva.Naziv, zadnja.Naziv,
		brojHRf(pad, 1), brojHRf(prva.Rkm-zadnja.Rkm, 0))
	p.Opis = "Uzdužni profil " + ime + ": promjena vodostaja od " +
		prva.Naziv + " do " + zadnja.Naziv + ", u centimetrima prema danas"
	p.Niz = nizZaKlizac(korisne, p, xOf, yOf)
	return p
}

// nizProfila je ono što klizač vremena treba: za svaki sat i svaku letvu
// odstupanje od današnjeg stanja, već preslikano u koordinate crteža.
type nizProfila struct {
	Sati  []int        `json:"sati"`
	Letve []string     `json:"letve"`
	X     []float64    `json:"x"`
	Sada  []float64    `json:"sada"`
	NulaY float64      `json:"nulaY"`
	PoCm  float64      `json:"poCm"` // točaka crteža po centimetru
	Usce  []bool       `json:"usce"` // po letvi: točka ušća, koja krivulju samo produžuje
	Akum  []bool       `json:"akum"` // po letvi: razina akumulacije, točka izvan krivulje
	V     [][]*float64 `json:"v"`    // po letvi, po satu; null gdje nema
}

func nizZaKlizac(korisne []LetvaProfila, p *UzduzniProfil, xOf, yOf func(float64) float64) template.JS {
	n := nizProfila{NulaY: p.NulaY, PoCm: yOf(0) - yOf(1)}
	for h := p.SatOd; h <= p.SatDo; h++ {
		n.Sati = append(n.Sati, h)
	}
	for _, l := range korisne {
		n.Letve = append(n.Letve, l.Naziv)
		n.Usce = append(n.Usce, l.Usce)
		n.Akum = append(n.Akum, l.Akumulacija)
		n.X = append(n.X, math.Round(xOf(l.Rkm)*10)/10)
		n.Sada = append(n.Sada, l.SadaCm)
		red := make([]*float64, len(n.Sati))
		for i, h := range n.Sati {
			if h == 0 {
				nula := 0.0
				red[i] = &nula
				continue
			}
			if v, ima := l.Niz[h]; ima {
				d := math.Round((v-l.SadaCm)*10) / 10
				red[i] = &d
			}
		}
		n.V = append(n.V, red)
	}
	b, err := json.Marshal(n)
	if err != nil {
		return "null"
	}
	return template.JS(b)
}

// oznakaImena pravi id kartice iz imena toka: "Drava i Mura" → "drava-i-mura".
func oznakaImena(ime string) string {
	zamjene := strings.NewReplacer("č", "c", "ć", "c", "š", "s", "đ", "d", "ž", "z",
		"Č", "c", "Ć", "c", "Š", "s", "Đ", "d", "Ž", "z", " ", "-")
	return strings.ToLower(zamjene.Replace(ime))
}

// odmaciPraga vraća koliko je prag udaljen od današnje vode na svakoj letvi, i
// ima li ga svaka. Prag koji nedostaje makar jednoj letvi ne crta se uopće:
// crta s rupom tvrdila bi da ondje praga nema, a on samo nije upisan.
func odmaciPraga(letve []LetvaProfila, kljuc string) ([]float64, bool) {
	out := make([]float64, 0, len(letve))
	for _, l := range letve {
		v, ima := l.Pragovi[kljuc]
		if !ima {
			return nil, false
		}
		out = append(out, v-l.SadaCm)
	}
	return out, true
}

// crtaOdstupanja povlači glatku crtu kroz letve; letva bez vrijednosti se
// preskače, a susjedi se spoje — dežurni gleda kako val putuje, a rupa u
// jednom dosegu (dnevni model samo na dijelu letvi) ne smije prekinuti val.
func crtaOdstupanja(letve []LetvaProfila, xOf, yOf func(float64) float64,
	vrijednost func(LetvaProfila) (float64, bool)) string {
	var tocke [][2]float64
	pravih := 0
	for _, l := range letve {
		if v, ima := vrijednost(l); ima {
			tocke = append(tocke, [2]float64{xOf(l.Rkm), yOf(v)})
			if !l.Usce {
				pravih++
			}
		}
	}
	// Točka ušća smije krivulju samo produžiti, ne i sama tvoriti: kad
	// vrijednost ima jedna letva, potez od nje do ušća ne bi bio val nego
	// razapeta crta između dvije rijeke.
	if pravih < 2 {
		return ""
	}
	return krivulja(tocke)
}

// plohaGranica gradi pojas između donje i gornje granice prognoze: gornjim
// rubom naprijed, donjim natrag, oba glatka kao i crta. Letva bez granica se
// preskače, kao i u crti.
func plohaGranica(letve []LetvaProfila, doseg int, xOf, yOf func(float64) float64) string {
	var gornji, donji [][2]float64
	pravih := 0
	for _, l := range letve {
		g, ima := l.Granice[doseg]
		if !ima {
			continue
		}
		gornji = append(gornji, [2]float64{xOf(l.Rkm), yOf(g[1] - l.SadaCm)})
		donji = append(donji, [2]float64{xOf(l.Rkm), yOf(g[0] - l.SadaCm)})
		if !l.Usce {
			pravih++
		}
	}
	if pravih < 2 {
		return ""
	}
	for i, j := 0, len(donji)-1; i < j; i, j = i+1, j-1 {
		donji[i], donji[j] = donji[j], donji[i]
	}
	return krivulja(gornji) + " L" + strings.TrimPrefix(krivulja(donji), "M") + " Z"
}

// krivulja provlači glatku krivulju kroz zadane točke: centripetalni
// Catmull–Rom pretvoren u kubične Bézierove lukove. Centripetalna inačica ne
// pravi petlje ni izbočine između udaljenih točaka, a letve stoje neravnomjerno
// — Sotin, Mohovo i Ilok na dvadeset kilometara, Aljmaš i Batina na četrdeset
// pet. Crta i dalje prolazi točno kroz svaku letvu; između njih samo kaže da se
// voda ne lomi.
func krivulja(t [][2]float64) string {
	if len(t) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "M%.1f %.1f", t[0][0], t[0][1])
	if len(t) == 1 {
		return b.String()
	}
	if len(t) == 2 {
		fmt.Fprintf(&b, " L%.1f %.1f", t[1][0], t[1][1])
		return b.String()
	}
	udalj := func(a, c [2]float64) float64 {
		return math.Sqrt(math.Hypot(c[0]-a[0], c[1]-a[1])) // α = 0,5: korijen udaljenosti
	}
	for i := 0; i+1 < len(t); i++ {
		p1, p2 := t[i], t[i+1]
		p0, p3 := p1, p2
		if i > 0 {
			p0 = t[i-1]
		}
		if i+2 < len(t) {
			p3 = t[i+2]
		}
		d1, d2, d3 := udalj(p0, p1), udalj(p1, p2), udalj(p2, p3)
		var b1, b2 [2]float64
		for k := 0; k < 2; k++ {
			if d1 < 1e-9 {
				b1[k] = p1[k]
			} else {
				b1[k] = (p2[k]*d1*d1 - p0[k]*d2*d2 + p1[k]*(2*d1*d1+3*d1*d2+d2*d2)) / (3 * d1 * (d1 + d2))
			}
			if d3 < 1e-9 {
				b2[k] = p2[k]
			} else {
				b2[k] = (p1[k]*d3*d3 - p3[k]*d2*d2 + p2[k]*(2*d3*d3+3*d3*d2+d2*d2)) / (3 * d3 * (d3 + d2))
			}
		}
		fmt.Fprintf(&b, " C%.1f %.1f %.1f %.1f %.1f %.1f", b1[0], b1[1], b2[0], b2[1], p2[0], p2[1])
	}
	return b.String()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
