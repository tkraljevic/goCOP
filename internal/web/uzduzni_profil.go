package web

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Uzdužni profil: vodno lice uzduž toka, od uzvodne letve prema ušću.
//
// Crta se u apsolutnim kotama, ne u centimetrima na letvi. Svaka letva ima
// svoju nulu — Donja Dubrava 129,54 m, Osijek 81,26 — pa bi crta kroz
// centimetre pokazivala razliku nula, a ne nagib vodnog lica. Sve kote idu u
// HVRS71: miješanje sa starim sustavom nosi razliku od dvadesetak centimetara,
// a upravo se o toliko i radi kad se gleda je li val prešao prag.
//
// Val se na ovakvom crtežu vidi kako putuje: crta za sutra leži iznad današnje
// ondje gdje val stiže, a ispod nje ondje odakle je otišao.

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
	Opis                    string
}

func (p *UzduzniProfil) LijevoX() float64 { return p.Lijevo }
func (p *UzduzniProfil) DesnoX() float64  { return float64(p.Width) - p.Desno }
func (p *UzduzniProfil) DnoY() float64    { return float64(p.Height) - p.Dno }
func (p *UzduzniProfil) OsY() float64     { return p.Lijevo - 8 }
func (p *UzduzniProfil) OsX() float64     { return float64(p.Height) - 8 }

// dosezi su prognoze koje se crtaju uz današnje stanje. Više od dvije crte
// pretvara profil u klupko.
var dosezniProfila = []int{24, 48}

var pragoviProfila = []struct{ kljuc, naziv, class string }{
	{"prep", "pripremna", "prep"},
	{"regular", "redovna", "regular"},
	{"emerg", "izvanredna", "emerg"},
}

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

	p := &UzduzniProfil{Ime: ime, Width: 1600, Height: 420,
		Lijevo: 86, Desno: 96, Vrh: 24, Dno: 56}
	najn, najv := math.Inf(1), math.Inf(-1)
	uzmi := func(kota float64) {
		najn, najv = math.Min(najn, kota), math.Max(najv, kota)
	}
	for _, l := range korisne {
		uzmi(l.KotaNule + l.SadaCm/100)
		for _, d := range dosezniProfila {
			if v, ima := l.Cm[d]; ima {
				uzmi(l.KotaNule + v/100)
			}
			if g, ima := l.Granice[d]; ima {
				uzmi(l.KotaNule + g[0]/100)
				uzmi(l.KotaNule + g[1]/100)
			}
		}
		// Pragovi ulaze u raspon samo kad su blizu: jedna visoka izvanredna
		// obrana inače spljošti cijeli profil i val se više ne vidi.
		for _, pr := range pragoviProfila {
			if cm, ima := l.Pragovi[pr.kljuc]; ima {
				k := l.KotaNule + cm/100
				if k-(l.KotaNule+l.SadaCm/100) < 4 {
					uzmi(k)
				}
			}
		}
	}
	if najv <= najn {
		najv = najn + 1
	}
	pad := (najv - najn) / 12
	najn, najv = najn-pad, najv+pad

	plotW := float64(p.Width) - p.Lijevo - p.Desno
	plotH := float64(p.Height) - p.Vrh - p.Dno
	odRkm, doRkm := korisne[0].Rkm, korisne[len(korisne)-1].Rkm
	raspon := odRkm - doRkm
	if raspon <= 0 {
		raspon = 1
	}
	xOf := func(rkm float64) float64 { return p.Lijevo + (odRkm-rkm)/raspon*plotW }
	yOf := func(kota float64) float64 { return p.Vrh + plotH - (kota-najn)/(najv-najn)*plotH }

	crta := func(vrijednost func(LetvaProfila) (float64, bool)) string {
		var b strings.Builder
		potez := "M"
		for _, l := range korisne {
			v, ima := vrijednost(l)
			if !ima {
				potez = "M"
				continue
			}
			fmt.Fprintf(&b, "%s%.1f %.1f", potez, xOf(l.Rkm), yOf(l.KotaNule+v/100))
			potez = " L"
		}
		return b.String()
	}

	p.Crte = append(p.Crte, ProfilCrta{Naziv: "sad", Class: "sad",
		Put: crta(func(l LetvaProfila) (float64, bool) { return l.SadaCm, true })})
	for i, d := range dosezniProfila {
		put := crta(func(l LetvaProfila) (float64, bool) { v, ima := l.Cm[d]; return v, ima })
		if put == "" {
			continue
		}
		c := ProfilCrta{Naziv: fmt.Sprintf("za %d h", d), Class: fmt.Sprintf("prog%d", i+1), Put: put}
		c.Pojas = plohaGranica(korisne, d, xOf, yOf)
		p.Crte = append(p.Crte, c)
	}
	for _, pr := range pragoviProfila {
		put := crta(func(l LetvaProfila) (float64, bool) { v, ima := l.Pragovi[pr.kljuc]; return v, ima })
		if put != "" {
			p.Pragovi = append(p.Pragovi, ProfilCrta{Naziv: pr.naziv, Class: pr.class, Put: put})
		}
	}

	for i, l := range korisne {
		kota := l.KotaNule + l.SadaCm/100
		sidro := "middle"
		if i == 0 {
			sidro = "start"
		} else if i == len(korisne)-1 {
			sidro = "end"
		}
		p.Tocke = append(p.Tocke, TockaUzduznog{
			Naziv: l.Naziv, X: xOf(l.Rkm), Y: yOf(kota),
			Kota: brojHRf(kota, 2), Cm: brojHRf(l.SadaCm, 0), Sidro: sidro,
		})
		p.XTicks = append(p.XTicks, ChartTick{Pos: xOf(l.Rkm), Label: brojHRf(l.Rkm, 1), Anchor: sidro})
	}

	korak := niceStep((najv - najn) / 6)
	for v := math.Ceil(najn/korak) * korak; v <= najv; v += korak {
		p.YTicks = append(p.YTicks, ChartTick{Pos: yOf(v), Label: brojHRf(v, 1)})
	}
	p.Opis = "Uzdužni profil " + ime + ": kota vodnog lica od " +
		korisne[0].Naziv + " do " + korisne[len(korisne)-1].Naziv + ", u metrima HVRS71"
	return p
}

// plohaGranica gradi pojas između donje i gornje granice prognoze.
func plohaGranica(letve []LetvaProfila, doseg int, xOf, yOf func(float64) float64) string {
	var gornji, donji []string
	for _, l := range letve {
		g, ima := l.Granice[doseg]
		if !ima {
			return "" // pojas s rupom bio bi kriv, pa se ne crta nijedan
		}
		gornji = append(gornji, fmt.Sprintf("%.1f %.1f", xOf(l.Rkm), yOf(l.KotaNule+g[1]/100)))
		donji = append(donji, fmt.Sprintf("%.1f %.1f", xOf(l.Rkm), yOf(l.KotaNule+g[0]/100)))
	}
	if len(gornji) < 2 {
		return ""
	}
	for i, j := 0, len(donji)-1; i < j; i, j = i+1, j-1 {
		donji[i], donji[j] = donji[j], donji[i]
	}
	return "M" + strings.Join(gornji, " L") + " L" + strings.Join(donji, " L") + " Z"
}
