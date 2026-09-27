package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
	"time"
)

// Uzdužni profil: kako se voda mijenja uzduž toka, od uzvodne letve prema ušću.
//
// Crta se odstupanje od uobičajene vode svake letve, u centimetrima, a ne
// apsolutna kota. U kotama se val ne vidi: Drava pada 49 metara od Donje
// Dubrave do Osijeka, a najveća promjena u dva dana nosi tridesetak
// centimetara. Uobičajena voda je medijan dnevnih vodostaja zadnjih deset
// godina; ona je nula crteža.
//
// Na tu nulu legne jutarnja crta: stanje u 7 sati, koje stoji cijeli dan. Val
// koji je jutros u Budimpešti na njoj je brijeg iznad Budimpešte, a crte
// prognoze pokazuju kako se taj brijeg pomiče prema Batini i kako se usput
// razvlači. Jutro stoji zato da se u svako doba dana vidi koliko je koja
// letva od jutra porasla ili pala — to piše uz svaku točku klizača.
//
// Svaki doseg ima svoju crtu, od najsvjetlije (za 6 h) do najtamnije (šesti
// dan), a uz crtež ide i klizač vremena kojim se ista slika gleda sat po sat,
// od mjerenja prije dva dana do kraja prognoze — tako se vidi da val doista
// putuje nizvodno.

// LetvaProfila je jedna letva na uzdužnom profilu.
type LetvaProfila struct {
	Letva, Naziv  string
	Rkm           float64
	KotaNule      float64 // m, Trst: zajednički sustav svih zemalja, za pad vodnog lica
	SadaCm        float64 // zadnje izmjereno
	ImaSada       bool
	JutroCm       float64 // jutarnje stanje, u 7 h (dok ga nema, od 4 h)
	ImaJutro      bool
	UobicajenoCm  float64 // nula crteža: medijan vode zadnjih 30 dana
	DugiMedijanCm float64 // uobičajena voda: medijan dnevnih vodostaja zadnjih deset godina (0 = nema)
	SrednjakCm    float64 // srednji vodostaj (SV) istih deset godina (0 = nema)
	ImaUobicajeno bool
	Cm            map[int]float64    // doseg u satima → prognozirani vodostaj
	Granice       map[int][2]float64 // doseg → donja i gornja granica
	Pragovi       map[string]float64 // prep | regular | emerg → cm
	Niz           map[int]float64    // sat prema izdanju (SatOd … SatDo) → vodostaj, za klizač
	Razina        string             // faza obrane danas: prep | regular | emerg | crit
	Usce          bool               // nije letva nego kraj pritoke s vrijednostima letve glavnog toka: u krivulji jest, natpisa nema
	Akumulacija   bool               // razina akumulacije uz branu: kota nad morem, točka koja se s klizačem diže i spušta, u krivulju vala ne ulazi
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
	Zemlja string // HU, RS, SK… uz tuđu letvu; ime ide bez zemlje u zagradi
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
	NulaY                   float64 // uobičajena voda: vodoravna crta na nuli
	JutroPut                string  // jutarnje stanje, crta koja stoji cijeli dan
	JutroOpis               string  // „jutro 27. 9. u 7 h”, za legendu
	DugiPut                 string  // uobičajena voda (medijan deset godina) svake letve
	SrednjakPut             string  // srednji vodostaj (SV) deset godina svake letve
	NulaUSlici              bool    // crta nule (voda zadnjih 30 dana) stane u crtež
	NulaNapomena            string  // što ne stane u crtež i gdje je: „↑ uobičajena voda (10 g.) … iznad crteža”
	NapomenaIspod           bool    // ono što ne stane je ispod crteža (velika voda), napomena ide dolje
	UsceY                   float64 // visina oznake ušća: na nuli, ili pri vrhu kad nule nema
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

// dosezniProfila su prognoze koje se crtaju uz jutarnje stanje: satni lanac do
// 96 sati, pa dnevni model za peti i šesti dan. Dosezi se broje od izdanja,
// pa crte idu naprijed sa svakim novim izdanjem, a jutro stoji. Boje idu od
// najsvjetlije do najtamnije, kako doseg raste.
var dosezniProfila = []int{6, 12, 24, 48, 72, 96, 120, 144}

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

// KolikoDalekoNula kaže koliko daleko nula, dugogodišnji medijan i srednjak
// smiju biti da se još crtaju, u širinama vala. Dalje od toga spljoštili bi val.
const KolikoDalekoNula = 0.5

// NulaUvijekDo je udaljenost uobičajene vode (cm) do koje se nula crta uvijek,
// i kad je val sitan: tridesetak centimetara nije daleko.
const NulaUvijekDo = 50.0

// razmakNatpisa je najmanji razmak dviju letvi (u točkama crteža) pri kojem
// im imena još stanu jedno uz drugo u istom redu.
const razmakNatpisa = 124.0

// SatniLanacDo je zadnji doseg satnog lanca; dalje je dnevni model, koji
// nema vrijednosti na svakoj letvi.
const SatniLanacDo = 96

// UdioJednakogRazmaka je koliki dio širine crteža letve dijele jednako; ostatak
// ide razmjerno kilometrima. Tako i gusto posađene letve dobiju mjesta za
// natpis, a duga dionica ostaje nešto šira od kratke.
const UdioJednakogRazmaka = 0.7

// osRazmaka slaže vodoravnu os: između dvaju susjednih čvorova (letvi) svaki
// razmak dobije jednak dio i dio razmjeran kilometrima; između čvorova
// kilometar se preslikava ravnomjerno, a izvan njih nastavlja zadnjim
// razmakom. Čvorovi mogu doći bilo kojim redom.
func osRazmaka(cvorovi []float64, lijevo, sirina float64) func(float64) float64 {
	k := append([]float64(nil), cvorovi...)
	sort.Sort(sort.Reverse(sort.Float64Slice(k)))
	var u []float64
	for _, v := range k {
		if len(u) == 0 || u[len(u)-1]-v > 1e-6 {
			u = append(u, v)
		}
	}
	if len(u) < 2 {
		return func(float64) float64 { return lijevo + sirina/2 }
	}
	n := len(u) - 1
	ukupno := u[0] - u[n]
	x := make([]float64, len(u))
	x[0] = lijevo
	for i := 0; i < n; i++ {
		w := sirina * UdioJednakogRazmaka / float64(n)
		w += sirina * (1 - UdioJednakogRazmaka) * (u[i] - u[i+1]) / ukupno
		x[i+1] = x[i] + w
	}
	return func(rkm float64) float64 {
		i := 0
		for i < n-1 && rkm < u[i+1] {
			i++
		}
		return x[i] + (u[i]-rkm)/(u[i]-u[i+1])*(x[i+1]-x[i])
	}
}

// spojiIli spaja nazive: „a”, „a i b”, „a, b i c”.
func spojiIli(n []string) string {
	if len(n) <= 1 {
		return strings.Join(n, "")
	}
	return strings.Join(n[:len(n)-1], ", ") + " i " + n[len(n)-1]
}

// kratkoIme skida zemlju iz zagrade: „Komárom (Mađarska)” → „Komárom”, „HU”.
func kratkoIme(naziv string) (string, string) {
	i := strings.LastIndex(naziv, " (")
	if i < 0 || !strings.HasSuffix(naziv, ")") {
		return naziv, ""
	}
	zemlja := naziv[i+2 : len(naziv)-1]
	oznaka, ima := oznakeZemalja[zemlja]
	if !ima {
		return naziv, ""
	}
	return naziv[:i], oznaka
}

var oznakeZemalja = map[string]string{
	"Mađarska": "HU", "Srbija": "RS", "Slovačka": "SK", "Slovenija": "SI", "Austrija": "AT",
	"Bosna i Hercegovina": "BA",
}

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
		if (l.KotaNule != 0 || l.Akumulacija) && (l.Rkm != 0 || l.Usce) && l.ImaJutro && l.ImaUobicajeno {
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

	najOd, najDo := math.Inf(1), math.Inf(-1)
	uzmi := func(cm float64) {
		najOd, najDo = math.Min(najOd, cm), math.Max(najDo, cm)
	}
	for _, l := range korisne {
		// Razina akumulacije ne ravna mjerilo: elektrana je u danu diže i
		// spušta metar i više, pa bi rijeka stala u tanku traku. Kad ne stane,
		// točka joj stoji na rubu crteža.
		if l.Akumulacija {
			for h := range l.Niz {
				p.SatDo = max(p.SatDo, h)
			}
			continue
		}
		uzmi(l.JutroCm - l.UobicajenoCm)
		for _, d := range dosezniProfila {
			if v, ima := l.Cm[d]; ima {
				uzmi(v - l.UobicajenoCm)
			}
			if g, ima := l.Granice[d]; ima {
				uzmi(g[0] - l.UobicajenoCm)
				uzmi(g[1] - l.UobicajenoCm)
			}
		}
		// I ono što klizač pokazuje mora stati u sliku: val koji je prošao
		// prije dva dana dio je iste priče.
		for h, v := range l.Niz {
			uzmi(v - l.UobicajenoCm)
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
	// Nula (voda zadnjih 30 dana), dugogodišnji medijan i srednjak ulaze u
	// sliku samo kad je voda blizu njih. Kad je Dunav dva metra ispod
	// uobičajenog, ta bi crta uzela pola crteža, a val od dvadeset centimetara
	// bio bi sitan; gore piše gdje je ona.
	dopustNule := math.Max(KolikoDalekoNula*sirina, NulaUvijekDo)
	blizu := func(o float64) bool { return o >= valOd-dopustNule && o <= valDo+dopustNule }
	p.NulaUSlici = blizu(0)
	if p.NulaUSlici {
		uzmi(0)
	}
	referenca := func(vrijednost func(LetvaProfila) float64) bool {
		ima := false
		for _, l := range korisne {
			if v := vrijednost(l); v != 0 && !l.Akumulacija {
				if !blizu(v - l.UobicajenoCm) {
					return false
				}
				ima = true
			}
		}
		return ima
	}
	dugiUSlici := referenca(func(l LetvaProfila) float64 { return l.DugiMedijanCm })
	svUSlici := referenca(func(l LetvaProfila) float64 { return l.SrednjakCm })
	for _, l := range korisne {
		if l.Akumulacija {
			continue
		}
		if dugiUSlici && l.DugiMedijanCm != 0 {
			uzmi(l.DugiMedijanCm - l.UobicajenoCm)
		}
		if svUSlici && l.SrednjakCm != 0 {
			uzmi(l.SrednjakCm - l.UobicajenoCm)
		}
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
	// Razmak letvi nije u mjerilu: naša dionica ima letvu svakih desetak
	// kilometara, mađarska svakih sedamdeset, pa bi u mjerilu devet naših
	// letvi dobilo petinu širine, a natpisi bi se preklapali.
	cvorovi := []float64{}
	for _, l := range korisne {
		if !l.Usce {
			cvorovi = append(cvorovi, l.Rkm)
		}
	}
	for _, u := range uscaUSlici {
		if u.Rkm > prava(0).Rkm || u.Rkm < prava(len(korisne)-1).Rkm {
			cvorovi = append(cvorovi, u.Rkm)
		}
	}
	xOf := osRazmaka(cvorovi, p.Lijevo, plotW)
	yOf := func(cm float64) float64 { return p.Vrh + (najDo-cm)/(najDo-najOd)*plotH }
	p.NulaY = yOf(0)
	p.UsceY = p.NulaY
	if !p.NulaUSlici {
		p.UsceY = p.Vrh + 18
	}
	p.NapomenaIspod = najOd > 0
	var izvan []string
	if !p.NulaUSlici {
		izvan = append(izvan, "voda zadnjih 30 dana (0)")
	}
	if !dugiUSlici {
		izvan = append(izvan, "uobičajena voda (10 g.)")
	}
	if !svUSlici {
		izvan = append(izvan, "srednji vodostaj")
	}
	if len(izvan) > 0 {
		smjer, gdje := "↑", "iznad"
		if p.NapomenaIspod {
			smjer, gdje = "↓", "ispod"
		}
		glagol := "je"
		if len(izvan) > 1 {
			glagol = "su"
		}
		p.NulaNapomena = smjer + " " + spojiIli(izvan) + " " + glagol + " " + gdje + " crteža"
	}
	p.JutroPut = crtaOdstupanja(korisne, xOf, yOf,
		func(l LetvaProfila) (float64, bool) { return l.JutroCm - l.UobicajenoCm, true })
	// Dugogodišnji medijan i srednjak razine akumulacije nisu voda rijeke: u
	// crte ne ulaze.
	if dugiUSlici {
		p.DugiPut = crtaOdstupanja(korisne, xOf, yOf, func(l LetvaProfila) (float64, bool) {
			return l.DugiMedijanCm - l.UobicajenoCm, l.DugiMedijanCm != 0
		})
	}
	if svUSlici {
		p.SrednjakPut = crtaOdstupanja(korisne, xOf, yOf, func(l LetvaProfila) (float64, bool) {
			return l.SrednjakCm - l.UobicajenoCm, l.SrednjakCm != 0
		})
	}

	for i, d := range dosezniProfila {
		c := ProfilCrta{Naziv: nazivDosega(d), Class: fmt.Sprintf("h%d", i+1), Doseg: d}
		vrijednost := func(l LetvaProfila) (float64, bool) { v, ima := l.Cm[d]; return v - l.UobicajenoCm, ima }
		if d > SatniLanacDo {
			// Dnevni model ima vrijednosti samo na dijelu letvi: crta se
			// prekida gdje ih nema, inače bi ravan potez preko Batine i
			// Vukovara izgledao kao val kojega nema.
			c.Put = crtaUDijelovima(korisne, xOf, yOf, vrijednost)
		} else {
			c.Put = crtaOdstupanja(korisne, xOf, yOf, vrijednost)
		}
		if c.Put == "" {
			continue
		}
		if d <= SatniLanacDo {
			c.Pojas = plohaGranica(korisne, d, xOf, yOf)
		}
		p.Crte = append(p.Crte, c)
	}
	for _, i := range uSlici {
		pr := pragoviProfila[i]
		put := crtaOdstupanja(korisne, xOf, yOf, func(l LetvaProfila) (float64, bool) {
			v, ima := l.Pragovi[pr.kljuc]
			return v - l.UobicajenoCm, ima
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
		ime, zemlja := kratkoIme(l.Naziv)
		t := TockaUzduznog{
			Naziv: ime, Zemlja: zemlja, X: x, Y: math.Max(p.Vrh, math.Min(p.DnoY(), yOf(l.JutroCm-l.UobicajenoCm))),
			Sidro: sidro, Dolje: dolje, Razina: l.Razina,
			Cm: brojHRf(l.JutroCm, 0),
		}
		switch {
		case l.Akumulacija:
			t.Cm, t.Kota = "", brojHRf(l.JutroCm/100, 2) // kota nad morem bez nule letve: samo metri
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
	pad := (prva.KotaNule + prva.JutroCm/100) - (zadnja.KotaNule + zadnja.JutroCm/100)
	p.Pad = fmt.Sprintf("%s, %s → %s: vodno lice jutros pada %s m na %s km", ime, prva.Naziv, zadnja.Naziv,
		brojHRf(pad, 1), brojHRf(prva.Rkm-zadnja.Rkm, 0))
	p.Opis = "Uzdužni profil " + ime + ": vodostaj od " + prva.Naziv + " do " + zadnja.Naziv +
		", u centimetrima iznad ili ispod vode zadnjih 30 dana, jutros i u prognozi"
	p.Niz = nizZaKlizac(korisne, p, xOf, yOf)
	return p
}

// nizProfila je ono što klizač vremena treba: za svaki sat i svaku letvu
// odstupanje od uobičajene vode, i jutro i uobičajena voda svake letve, da se
// uz točku može napisati koliko je od jutra poraslo ili palo.
type nizProfila struct {
	Sati  []int        `json:"sati"`
	Letve []string     `json:"letve"`
	X     []float64    `json:"x"`
	Sada  []*float64   `json:"sada"`  // po letvi: zadnje izmjereno, cm
	Jutro []float64    `json:"jutro"` // po letvi: jutarnje stanje, cm
	Uob   []float64    `json:"uob"`   // po letvi: nula, medijan vode zadnjih 30 dana, cm
	Dugi  []float64    `json:"dugi"`  // po letvi: uobičajena voda deset godina (medijan), cm
	SV    []float64    `json:"sv"`    // po letvi: srednji vodostaj, cm
	NulaY float64      `json:"nulaY"`
	PoCm  float64      `json:"poCm"` // točaka crteža po centimetru
	Usce  []bool       `json:"usce"` // po letvi: točka ušća, koja krivulju samo produžuje
	Akum  []bool       `json:"akum"` // po letvi: razina akumulacije, točka izvan krivulje
	V     [][]*float64 `json:"v"`    // po letvi, po satu: odstupanje od uobičajene vode; null gdje nema
	// PrekidOd je sat iza kojega crta klizača prekida ondje gdje letva nema
	// vrijednosti (dnevni model), umjesto da spoji susjede.
	PrekidOd int `json:"prekidOd"`
	// Vrh i Dno omeđuju crtež: točka akumulacije izvan njih stoji na rubu.
	Vrh float64 `json:"vrh"`
	Dno float64 `json:"dno"`
}

func nizZaKlizac(korisne []LetvaProfila, p *UzduzniProfil, xOf, yOf func(float64) float64) template.JS {
	n := nizProfila{NulaY: p.NulaY, PoCm: yOf(0) - yOf(1), PrekidOd: SatniLanacDo, Vrh: p.Vrh, Dno: p.DnoY()}
	for h := p.SatOd; h <= p.SatDo; h++ {
		n.Sati = append(n.Sati, h)
	}
	for _, l := range korisne {
		n.Letve = append(n.Letve, l.Naziv)
		n.Usce = append(n.Usce, l.Usce)
		n.Akum = append(n.Akum, l.Akumulacija)
		n.X = append(n.X, math.Round(xOf(l.Rkm)*10)/10)
		n.Jutro = append(n.Jutro, l.JutroCm)
		n.Uob = append(n.Uob, math.Round(l.UobicajenoCm*10)/10)
		n.Dugi = append(n.Dugi, math.Round(l.DugiMedijanCm*10)/10)
		n.SV = append(n.SV, math.Round(l.SrednjakCm*10)/10)
		var sada *float64
		if l.ImaSada {
			v := l.SadaCm
			sada = &v
		}
		n.Sada = append(n.Sada, sada)
		red := make([]*float64, len(n.Sati))
		for i, h := range n.Sati {
			v, ima := l.Niz[h]
			if h == 0 {
				v, ima = l.SadaCm, l.ImaSada
			}
			if ima {
				d := math.Round((v-l.UobicajenoCm)*10) / 10
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

// opisJutra piše dan jutarnjeg stanja za legendu: „jutro 27. 9. u 7 h”.
func opisJutra(dan time.Time) string {
	if dan.IsZero() {
		return "jutro u 7 h"
	}
	return fmt.Sprintf("jutro %d. %d. u %d h", dan.Day(), int(dan.Month()), JutroSat)
}

// oznakaImena pravi id kartice iz imena toka: "Drava i Mura" → "drava-i-mura".
func oznakaImena(ime string) string {
	zamjene := strings.NewReplacer("č", "c", "ć", "c", "š", "s", "đ", "d", "ž", "z",
		"Č", "c", "Ć", "c", "Š", "s", "Đ", "d", "Ž", "z", " ", "-")
	return strings.ToLower(zamjene.Replace(ime))
}

// odmaciPraga vraća koliko je prag udaljen od uobičajene vode na svakoj letvi, i
// ima li ga svaka. Prag koji nedostaje makar jednoj letvi ne crta se uopće:
// crta s rupom tvrdila bi da ondje praga nema, a on samo nije upisan.
func odmaciPraga(letve []LetvaProfila, kljuc string) ([]float64, bool) {
	out := make([]float64, 0, len(letve))
	for _, l := range letve {
		v, ima := l.Pragovi[kljuc]
		if !ima {
			return nil, false
		}
		out = append(out, v-l.UobicajenoCm)
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
		if l.Akumulacija {
			continue // razina akumulacije je bazen uz branu, ne rijeka: u krivulju vala ne ulazi
		}
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

// crtaUDijelovima povlači crtu samo kroz neprekinute nizove letvi s
// vrijednošću; prava letva bez vrijednosti prekida crtu. Usamljena letva
// dobije kružić, da se njezina vrijednost ipak vidi. Ušće i akumulacija crtu
// ne prekidaju.
func crtaUDijelovima(letve []LetvaProfila, xOf, yOf func(float64) float64,
	vrijednost func(LetvaProfila) (float64, bool)) string {
	var dijelovi []string
	var tocke [][2]float64
	var prava [2]float64
	pravih := 0
	zatvori := func() {
		switch {
		case pravih >= 2:
			dijelovi = append(dijelovi, krivulja(tocke))
		case pravih == 1:
			dijelovi = append(dijelovi, fmt.Sprintf("M%.1f %.1f m-3.5 0 a3.5 3.5 0 1 0 7 0 a3.5 3.5 0 1 0 -7 0", prava[0], prava[1]))
		}
		tocke, pravih = nil, 0
	}
	for _, l := range letve {
		if l.Akumulacija {
			continue
		}
		v, ima := vrijednost(l)
		if !ima {
			if !l.Usce && !l.Akumulacija {
				zatvori()
			}
			continue
		}
		t := [2]float64{xOf(l.Rkm), yOf(v)}
		tocke = append(tocke, t)
		if !l.Usce && !l.Akumulacija {
			prava = t
			pravih++
		}
	}
	zatvori()
	return strings.Join(dijelovi, " ")
}

// plohaGranica gradi pojas između donje i gornje granice prognoze: gornjim
// rubom naprijed, donjim natrag, oba glatka kao i crta. Letva bez granica se
// preskače, kao i u crti.
func plohaGranica(letve []LetvaProfila, doseg int, xOf, yOf func(float64) float64) string {
	var gornji, donji [][2]float64
	pravih := 0
	for _, l := range letve {
		g, ima := l.Granice[doseg]
		if !ima || l.Akumulacija {
			continue
		}
		gornji = append(gornji, [2]float64{xOf(l.Rkm), yOf(g[1] - l.UobicajenoCm)})
		donji = append(donji, [2]float64{xOf(l.Rkm), yOf(g[0] - l.UobicajenoCm)})
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
