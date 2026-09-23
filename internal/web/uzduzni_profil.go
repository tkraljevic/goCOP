package web

import (
	"fmt"
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
// a ispod nje ondje odakle je otišao.

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
}

// ProfilCrta je jedna crta uzduž toka.
type ProfilCrta struct {
	Naziv string
	Class string
	Put   string
	Pojas string // granice prognoze, kao zatvorena ploha
}

// TockaUzduznog je jedna letva na crtežu, s natpisom.
type TockaUzduznog struct {
	Naziv string
	X, Y  float64
	Kota  string
	Cm    string
	Sidro string
}

// UzduzniProfil je gotov crtež jednog toka.
type UzduzniProfil struct {
	Ime                     string
	Width, Height           int
	Lijevo, Desno, Vrh, Dno float64
	Crte                    []ProfilCrta
	Pragovi                 []ProfilCrta
	Tocke                   []TockaUzduznog
	YTicks                  []ChartTick
	XTicks                  []ChartTick
	NulaY                   float64 // današnje stanje: vodoravna crta na nuli
	Opis                    string
}

func (p *UzduzniProfil) LijevoX() float64 { return p.Lijevo }
func (p *UzduzniProfil) DesnoX() float64  { return float64(p.Width) - p.Desno }
func (p *UzduzniProfil) DnoY() float64    { return float64(p.Height) - p.Dno }
func (p *UzduzniProfil) OsY() float64     { return p.Lijevo - 8 }
func (p *UzduzniProfil) OsX() float64     { return float64(p.Height) - 8 }

// dosezniProfila su prognoze koje se crtaju uz današnje stanje. Više od dvije
// crte pretvara profil u klupko.
var dosezniProfila = []int{24, 48}

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

// crtajUzduzni slaže profil jednog toka. Letve bez kote nule ili bez
// stacionaže ispadaju: bez njih se ne zna ni gdje su ni koliko visoko.
func crtajUzduzni(ime string, letve []LetvaProfila) *UzduzniProfil {
	var korisne []LetvaProfila
	for _, l := range letve {
		if l.KotaNule != 0 && l.Rkm != 0 && l.ImaSada {
			korisne = append(korisne, l)
		}
	}
	if len(korisne) < 2 {
		return nil
	}
	// Nizvodno ide udesno, a rkm nizvodno pada.
	sort.Slice(korisne, func(i, j int) bool { return korisne[i].Rkm > korisne[j].Rkm })

	p := &UzduzniProfil{Ime: ime, Width: 1600, Height: 380,
		Lijevo: 86, Desno: 96, Vrh: 40, Dno: 62}

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
	odRkm, doRkm := korisne[0].Rkm, korisne[len(korisne)-1].Rkm
	raspon := odRkm - doRkm
	if raspon <= 0 {
		raspon = 1
	}
	xOf := func(rkm float64) float64 { return p.Lijevo + (odRkm-rkm)/raspon*plotW }
	yOf := func(cm float64) float64 { return p.Vrh + (najDo-cm)/(najDo-najOd)*plotH }
	p.NulaY = yOf(0)

	for i, d := range dosezniProfila {
		c := ProfilCrta{Naziv: fmt.Sprintf("za %d h", d), Class: fmt.Sprintf("prog%d", i+1)}
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

	for i, l := range korisne {
		sidro := "middle"
		if i == 0 {
			sidro = "start"
		} else if i == len(korisne)-1 {
			sidro = "end"
		}
		p.Tocke = append(p.Tocke, TockaUzduznog{
			Naziv: l.Naziv, X: xOf(l.Rkm), Y: p.NulaY,
			Kota: brojHRf(l.KotaNule+l.SadaCm/100, 2), Cm: brojHRf(l.SadaCm, 0), Sidro: sidro,
		})
		p.XTicks = append(p.XTicks, ChartTick{Pos: xOf(l.Rkm), Label: brojHRf(l.Rkm, 1), Anchor: sidro})
	}

	korak := niceStep((najDo - najOd) / 5)
	for v := math.Ceil(najOd/korak) * korak; v <= najDo; v += korak {
		p.YTicks = append(p.YTicks, ChartTick{Pos: yOf(v), Label: brojHRf(v, 0)})
	}
	p.Opis = "Uzdužni profil " + ime + ": promjena vodostaja od " +
		korisne[0].Naziv + " do " + korisne[len(korisne)-1].Naziv + ", u centimetrima prema danas"
	return p
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

// crtaOdstupanja povlači crtu kroz letve; ondje gdje vrijednosti nema, crta se
// prekida umjesto da preskoči letvu.
func crtaOdstupanja(letve []LetvaProfila, xOf, yOf func(float64) float64,
	vrijednost func(LetvaProfila) (float64, bool)) string {
	var b strings.Builder
	potez := "M"
	for _, l := range letve {
		v, ima := vrijednost(l)
		if !ima {
			potez = "M"
			continue
		}
		fmt.Fprintf(&b, "%s%.1f %.1f", potez, xOf(l.Rkm), yOf(v))
		potez = " L"
	}
	return b.String()
}

// plohaGranica gradi pojas između donje i gornje granice prognoze.
func plohaGranica(letve []LetvaProfila, doseg int, xOf, yOf func(float64) float64) string {
	var gornji, donji []string
	for _, l := range letve {
		g, ima := l.Granice[doseg]
		if !ima {
			return "" // pojas s rupom bio bi kriv, pa se ne crta nijedan
		}
		gornji = append(gornji, fmt.Sprintf("%.1f %.1f", xOf(l.Rkm), yOf(g[1]-l.SadaCm)))
		donji = append(donji, fmt.Sprintf("%.1f %.1f", xOf(l.Rkm), yOf(g[0]-l.SadaCm)))
	}
	if len(gornji) < 2 {
		return ""
	}
	for i, j := 0, len(donji)-1; i < j; i, j = i+1, j-1 {
		donji[i], donji[j] = donji[j], donji[i]
	}
	return "M" + strings.Join(gornji, " L") + " L" + strings.Join(donji, " L") + " Z"
}
