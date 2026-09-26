// Čitanje hidroloških godišnjaka Republičkog hidrometeorološkog zavoda
// Srbije. Godišnjak je PDF, a u njemu za svaku postaju stoji tablica dnevnih
// vodostaja: dani u redcima, mjeseci u stupcima, dvije postaje jedna uz
// drugu na istoj stranici.
//
//	ДАН       I       II     III     IV  …     ДАН       I       II   …
//	 1       226     56 ●    221    290  …      1       308     113 ●…
//
// Ispod dana stoje mjesečni sažeci (min, сред., max) i godišnji. Njima se
// pročitano provjerava: ako se izračunati mjesečni najmanji, srednji i
// najveći poklope s ispisanima, stupci su pogođeni.
//
// Oznaka ● uz vrijednost znači da je mjerenje pod utjecajem leda ili uspora;
// vrijednost se čita, a oznaka se pamti uz niz, ne uz pojedino očitanje.
package godisnjak

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Tablica je jedna pročitana tablica dnevnih vodostaja jedne postaje.
type Tablica struct {
	Sifra     string
	Naziv     string
	Godina    int
	Dani      map[Dan]int
	Oznacenih int // vrijednosti uz koje stoji oznaka leda ili uspora
	Sazetak   map[int]Mjesec
}

// Dan je jedan dan u godini tablice.
type Dan struct{ Mjesec, Dan int }

// Mjesec je ono što godišnjak ispisuje ispod dana: najmanji, srednji i
// najveći vodostaj mjeseca. Služi za provjeru pročitanog.
type Mjesec struct {
	Min, Sred, Max          int
	ImaMin, ImaSred, ImaMax bool
}

var (
	reNaslov  = regexp.MustCompile(`В\s*О\s*Д\s*О\s*С\s*Т\s*А\s*Ј\s*И\s+ЗА\s+(\d{4})`)
	reSifra   = regexp.MustCompile(`Шифра\s*:\s*(\d{4,6})`)
	reStanica = regexp.MustCompile(`Станица\s*:\s*(.+?)\s{2,}`)
	reBroj    = regexp.MustCompile(`-?\d+`)
	rimski    = []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII"}
)

// Citaj razlaže sve tablice vodostaja iz teksta jednog godišnjaka.
func Citaj(tekst string) []Tablica {
	var out []Tablica
	for _, stranica := range strings.Split(tekst, "\f") {
		out = append(out, citajStranicu(stranica)...)
	}
	return out
}

// citajStranicu vadi tablice s jedne stranice. Na stranici stoje dvije
// postaje jedna uz drugu, pa se redak dijeli na lijevu i desnu polovicu.
func citajStranicu(stranica string) []Tablica {
	redci := strings.Split(stranica, "\n")
	iNaslov, godina := -1, 0
	for i, r := range redci {
		if m := reNaslov.FindStringSubmatch(r); m != nil {
			iNaslov = i
			godina, _ = strconv.Atoi(m[1])
			break
		}
	}
	if iNaslov < 0 {
		return nil
	}
	iZaglavlje := -1
	for i := iNaslov; i < len(redci) && i < iNaslov+6; i++ {
		if strings.Count(redci[i], "ДАН") >= 1 && strings.Contains(redci[i], "III") {
			iZaglavlje = i
			break
		}
	}
	if iZaglavlje < 0 {
		return nil
	}
	granice := podjelaStranice(redci[iZaglavlje])
	sifre := metapodaci(redci[:iZaglavlje], reSifra, granice)
	nazivi := metapodaci(redci[:iZaglavlje], reStanica, granice)

	var out []Tablica
	for i, g := range granice {
		zaglavlje := isjeci(redci[iZaglavlje], g)
		stupci := stupciMjeseci(zaglavlje)
		if stupci == nil {
			continue
		}
		t := Tablica{Godina: godina, Dani: map[Dan]int{}, Sazetak: map[int]Mjesec{}}
		if i < len(sifre) {
			t.Sifra = sifre[i]
		}
		if i < len(nazivi) {
			t.Naziv = nazivi[i]
		}
		for _, r := range redci[iZaglavlje+1:] {
			dioRetka := isjeci(r, g)
			if pokupiDan(&t, dioRetka, stupci) {
				continue
			}
			pokupiSazetak(&t, dioRetka, stupci)
		}
		if len(t.Dani) > 0 {
			out = append(out, t)
		}
	}
	return out
}

// podjela je raspon stupaca jedne tablice unutar retka.
type podjela struct{ od, do int }

// podjelaStranice dijeli redak na tablice po pojavama riječi ДАН na početku
// svake; zadnja ide do kraja retka.
func podjelaStranice(zaglavlje string) []podjela {
	z := []rune(zaglavlje)
	var poc []int
	for i := 0; i+3 <= len(z); {
		j := indeksRune(z[i:], []rune("ДАН"))
		if j < 0 {
			break
		}
		p := i + j
		// drugi ДАН u istoj tablici zatvara dane s desne strane; tablicom se
		// smatra tek onaj iza kojeg opet dolaze mjeseci
		if len(poc) == 0 || p-poc[len(poc)-1] > 40 {
			poc = append(poc, p)
		}
		i = p + 3
	}
	var out []podjela
	for i, p := range poc {
		kraj := len(z)
		if i+1 < len(poc) {
			kraj = poc[i+1]
		}
		out = append(out, podjela{p, kraj})
	}
	return out
}

// isjeci vadi dio retka po stupcima. Broji se u znakovima, ne bajtovima:
// ćirilica u zaglavlju zauzima dva bajta po znaku, pa bi po bajtovima
// stupci zaglavlja i stupci brojeva pali na različita mjesta.
func isjeci(r string, g podjela) string {
	z := []rune(r)
	if g.od >= len(z) {
		return ""
	}
	do := g.do
	if do > len(z) {
		do = len(z)
	}
	return string(z[g.od:do])
}

// metapodaci traže vrijednost uzorka po tablicama, po stupcu u kojem stoji.
func metapodaci(redci []string, uzorak *regexp.Regexp, granice []podjela) []string {
	out := make([]string, len(granice))
	for _, r := range redci {
		mjesta := uzorak.FindAllStringSubmatchIndex(r, -1)
		if len(mjesta) == 0 {
			continue
		}
		for _, m := range mjesta {
			vrijednost := strings.TrimSpace(r[m[2]:m[3]])
			for i, g := range granice {
				if out[i] != "" {
					continue
				}
				// metapodatak stoji lijevo od svoje tablice, ali bliže njoj
				// nego sljedećoj
				if m[0] >= g.od-90 && m[0] < g.do {
					out[i] = vrijednost
					break
				}
			}
		}
	}
	return out
}

// stupciMjeseci nalazi stupac u kojem završava naziv svakog mjeseca.
func stupciMjeseci(zaglavlje string) []int {
	var out []int
	od := 0
	for _, mj := range rimski {
		i := nadjiRijec(zaglavlje, mj, od)
		if i < 0 {
			return nil
		}
		od = i + len(mj)
		out = append(out, od)
	}
	return out
}

func nadjiRijec(s, rijec string, od int) int {
	z, r := []rune(s), []rune(rijec)
	for i := od; i+len(r) <= len(z); i++ {
		if string(z[i:i+len(r)]) != rijec {
			continue
		}
		if i > 0 && z[i-1] != ' ' {
			continue
		}
		if i+len(r) < len(z) && z[i+len(r)] != ' ' {
			continue
		}
		return i
	}
	return -1
}

// indeksRune traži niz znakova u nizu znakova; vraća položaj u znakovima
func indeksRune(z, trazi []rune) int {
	for i := 0; i+len(trazi) <= len(z); i++ {
		if string(z[i:i+len(trazi)]) == string(trazi) {
			return i
		}
	}
	return -1
}

// pokupiDan čita redak s danom u mjesecu; javlja je li redak bio takav.
func pokupiDan(t *Tablica, r string, stupci []int) bool {
	glava := strings.TrimSpace(prvihZnakova(r, 6))
	dan, err := strconv.Atoi(glava)
	if err != nil || dan < 1 || dan > 31 {
		return false
	}
	for mj, v := range poStupcima(r, stupci) {
		if !v.ima {
			continue
		}
		if dan > daniUMjesecu(t.Godina, mj+1) {
			continue
		}
		t.Dani[Dan{mj + 1, dan}] = v.broj
		if v.oznaka {
			t.Oznacenih++
		}
	}
	return true
}

// pokupiSazetak čita mjesečne sažetke ispod dana.
func pokupiSazetak(t *Tablica, r string, stupci []int) {
	glava := strings.ToLower(strings.TrimSpace(prvihZnakova(r, 6)))
	var postavi func(m *Mjesec, v int)
	switch {
	case strings.HasPrefix(glava, "min"):
		postavi = func(m *Mjesec, v int) { m.Min, m.ImaMin = v, true }
	case strings.HasPrefix(glava, "сред"):
		postavi = func(m *Mjesec, v int) { m.Sred, m.ImaSred = v, true }
	case strings.HasPrefix(glava, "max"):
		postavi = func(m *Mjesec, v int) { m.Max, m.ImaMax = v, true }
	default:
		return
	}
	for mj, v := range poStupcima(r, stupci) {
		if !v.ima {
			continue
		}
		s := t.Sazetak[mj+1]
		postavi(&s, v.broj)
		t.Sazetak[mj+1] = s
	}
}

func prvihZnakova(r string, n int) string {
	z := []rune(r)
	if len(z) < n {
		return r
	}
	return string(z[:n])
}

type polje struct {
	broj   int
	ima    bool
	oznaka bool
}

// poStupcima razvrstava brojeve iz retka po mjesecima. Brojevi su poravnati
// desno prema nazivu mjeseca, a uz njih zna stajati oznaka, pa se svaki broj
// pripisuje mjesecu čiji mu je stupac najbliži.
func poStupcima(r string, stupci []int) []polje {
	out := make([]polje, len(stupci))
	z := []rune(r)
	for _, m := range brojeviURetku(z) {
		if m[0] < 6 { // stupac s danom
			continue
		}
		najbolji, razmak := -1, 1<<30
		for i, s := range stupci {
			d := m[1] - s
			if d < 0 {
				d = -d
			}
			if d < razmak {
				najbolji, razmak = i, d
			}
		}
		if najbolji < 0 || razmak > 8 {
			continue
		}
		v, err := strconv.Atoi(string(z[m[0]:m[1]]))
		if err != nil {
			continue
		}
		oznaka := false
		for i := m[1]; i < len(z) && i <= m[1]+2; i++ {
			if z[i] == ' ' {
				continue
			}
			oznaka = z[i] != '-' && (z[i] < '0' || z[i] > '9')
			break
		}
		out[najbolji] = polje{broj: v, ima: true, oznaka: oznaka}
	}
	return out
}

// brojeviURetku nalazi brojeve i njihov položaj u znakovima
func brojeviURetku(z []rune) [][2]int {
	var out [][2]int
	for i := 0; i < len(z); {
		if z[i] != '-' && (z[i] < '0' || z[i] > '9') {
			i++
			continue
		}
		j := i
		if z[j] == '-' {
			j++
		}
		poc := j
		for j < len(z) && z[j] >= '0' && z[j] <= '9' {
			j++
		}
		if j > poc {
			out = append(out, [2]int{i, j})
		}
		i = j + 1
	}
	return out
}

func daniUMjesecu(godina, mjesec int) int {
	switch mjesec {
	case 4, 6, 9, 11:
		return 30
	case 2:
		if godina%4 == 0 && (godina%100 != 0 || godina%400 == 0) {
			return 29
		}
		return 28
	}
	return 31
}

// Provjeri usporedi pročitane dane s mjesečnim sažecima koje godišnjak sam
// ispisuje. Vraća broj provjerenih mjeseci i popis onih koji se ne slažu.
func (t Tablica) Provjeri() (provjereno int, greske []string) {
	for mj := 1; mj <= 12; mj++ {
		s, ima := t.Sazetak[mj]
		if !ima {
			continue
		}
		var v []int
		for d := 1; d <= 31; d++ {
			if x, ok := t.Dani[Dan{mj, d}]; ok {
				v = append(v, x)
			}
		}
		if len(v) == 0 {
			continue
		}
		provjereno++
		min, max, zbroj := v[0], v[0], 0
		for _, x := range v {
			if x < min {
				min = x
			}
			if x > max {
				max = x
			}
			zbroj += x
		}
		sred := int(float64(zbroj)/float64(len(v)) + 0.5)
		// Ispisani mjesečni najmanji i najveći su trenutni vodostaji, uz njih
		// stoji i sat, pa su izvan raspona dnevnih vrijednosti. Ne traži se
		// jednakost nego da ih dnevne vrijednosti ne probijaju.
		if s.ImaMin && min < s.Min {
			greske = append(greske, fmt.Sprintf("%d. mjesec: dnevna vrijednost %d ispod ispisanog najmanjeg %d", mj, min, s.Min))
		}
		if s.ImaMax && max > s.Max {
			greske = append(greske, fmt.Sprintf("%d. mjesec: dnevna vrijednost %d iznad ispisanog najvećeg %d", mj, max, s.Max))
		}
		if s.ImaSred && abs(s.Sred-sred) > 1 {
			greske = append(greske, fmt.Sprintf("%d. mjesec: srednji %d, ispisano %d", mj, sred, s.Sred))
		}
	}
	return provjereno, greske
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
