// Package propisi čita propise iz Narodnih novina i iz zalijepljenog teksta,
// dijeli ih na dijelove i slaže alate za pretragu i stalne adrese dijelova.
package propisi

import (
	"strings"
	"unicode"
)

// svedeniZnak pretvara slova s kvačicama i ćirilične dvojnike latiničnih
// slova u obična latinična slova. Ćirilični dvojnici dolaze u tekst NN
// kopiranjem i izgledaju isto kao latinica, a pretraga ih inače ne bi našla.
var svedeniZnak = map[rune]rune{
	'č': 'c', 'ć': 'c', 'š': 's', 'ž': 'z', 'đ': 'd',
	'о': 'o', 'а': 'a', 'е': 'e', 'с': 'c', 'р': 'p', 'х': 'x', 'і': 'i',
}

// Svedi priprema tekst za pretragu: mala slova, bez kvačica (đ postaje d) i
// bez ćiriličnih dvojnika. Svaki znak postaje točno jedan znak, pa položaj
// pogotka u svedenom tekstu vrijedi i u izvornom.
func Svedi(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if z, ok := svedeniZnak[r]; ok {
			return z
		}
		return r
	}, s)
}

// nastavci su padežni nastavci koje Korijen skida, najdulji prvi.
var nastavci = []string{"ama", "ima", "oga", "ome", "emu", "ega", "om", "em", "og", "eg", "oj", "ih", "im", "a", "e", "i", "o", "u"}

// najkraciKorijen je broj slova koji riječi ostaje nakon skidanja nastavka.
const najkraciKorijen = 4

// Korijen skida padežni nastavak sa svedene riječi, da „pripremnog stanja”
// nađe i „pripremno stanje”. Riječ od četiri slova ili kraća ostaje cijela,
// a nakon skidanja ostaju barem četiri slova.
func Korijen(r string) string {
	duljina := len([]rune(r))
	for _, n := range nastavci {
		if strings.HasSuffix(r, n) && duljina-len(n) >= najkraciKorijen {
			return strings.TrimSuffix(r, n)
		}
	}
	return r
}

// Korijeni rastavlja upit na svedene korijene riječi od barem tri znaka,
// bez ponavljanja, redom kako su upisane.
func Korijeni(upit string) []string {
	var out []string
	vidjeno := map[string]bool{}
	for _, r := range strings.FieldsFunc(Svedi(upit), nijeDioRijeci) {
		if len([]rune(r)) < 3 {
			continue
		}
		k := Korijen(r)
		if !vidjeno[k] {
			vidjeno[k] = true
			out = append(out, k)
		}
	}
	return out
}

func nijeDioRijeci(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// KomadIsjecka je komad isječka iz teksta; Istaknuto je pogodak upita.
type KomadIsjecka struct {
	Tekst     string
	Istaknuto bool
}

// raspon su položaji [od, do) u runama.
type raspon struct{ od, do int }

// Isjecak vadi iz teksta oko sirina znakova oko prvog pogotka i ističe svaki
// pogodak korijena, do kraja riječi. Isticanje je na izvornom tekstu, s
// kvačicama. Bez pogotka isječak je početak teksta; odrezan rub dobiva „…”.
func Isjecak(tekst string, korijeni []string, sirina int) []KomadIsjecka {
	izvorno := []rune(tekst)
	pogoci := naciPogotke([]rune(Svedi(tekst)), korijeni)
	prvi := 0
	if len(pogoci) > 0 {
		prvi = pogoci[0].od
	}
	p := prozor(izvorno, prvi, sirina)
	return slaziKomade(izvorno, p, pogoci)
}

// naciPogotke vraća poredane raspone riječi koje sadrže korijen, od mjesta
// korijena do kraja riječi, bez preklapanja.
func naciPogotke(sv []rune, korijeni []string) []raspon {
	oznaceno := make([]bool, len(sv))
	for _, k := range korijeni {
		oznaciKorijen(sv, []rune(k), oznaceno)
	}
	var out []raspon
	for i := 0; i < len(sv); i++ {
		if !oznaceno[i] {
			continue
		}
		od := i
		for i < len(sv) && oznaceno[i] {
			i++
		}
		out = append(out, raspon{od, i})
	}
	return out
}

// oznaciKorijen označi svako pojavljivanje korijena i ostatak njegove riječi.
func oznaciKorijen(sv, k []rune, oznaceno []bool) {
	if len(k) == 0 {
		return
	}
	for i := 0; i+len(k) <= len(sv); i++ {
		if string(sv[i:i+len(k)]) != string(k) {
			continue
		}
		kraj := i + len(k)
		for kraj < len(sv) && !nijeDioRijeci(sv[kraj]) {
			kraj++
		}
		for j := i; j < kraj; j++ {
			oznaceno[j] = true
		}
	}
}

// prozor bira dio teksta od oko sirina znakova: počinje malo prije prvog
// pogotka i reže se na razmaku, da riječ ne ostane prepolovljena.
func prozor(t []rune, prvi, sirina int) raspon {
	if sirina <= 0 || len(t) <= sirina {
		return raspon{0, len(t)}
	}
	od := max(prvi-sirina/3, 0)
	do := min(od+sirina, len(t))
	od = max(do-sirina, 0)
	if od > 0 {
		od = iza(t, od, prvi)
	}
	if do < len(t) {
		do = ispred(t, do, prvi)
	}
	return raspon{od, do}
}

// iza pomiče početak isječka iza prvog razmaka, ali ne preko pogotka.
func iza(t []rune, od, granica int) int {
	for i := od; i < granica; i++ {
		if unicode.IsSpace(t[i]) {
			return i + 1
		}
	}
	return od
}

// ispred vraća kraj isječka na zadnji razmak, ali ne ispred pogotka.
func ispred(t []rune, do, granica int) int {
	for i := do; i > granica; i-- {
		if unicode.IsSpace(t[i-1]) {
			return i - 1
		}
	}
	return do
}

// slaziKomade reže prozor na obične i istaknute komade.
func slaziKomade(t []rune, p raspon, pogoci []raspon) []KomadIsjecka {
	var out []KomadIsjecka
	dodaj := func(od, do int, istaknuto bool) {
		if od < do {
			out = append(out, KomadIsjecka{Tekst: string(t[od:do]), Istaknuto: istaknuto})
		}
	}
	if p.od > 0 {
		out = append(out, KomadIsjecka{Tekst: "…"})
	}
	poz := p.od
	for _, r := range pogoci {
		od, do := max(r.od, p.od), min(r.do, p.do)
		if od >= do {
			continue
		}
		dodaj(poz, od, false)
		dodaj(od, do, true)
		poz = do
	}
	dodaj(poz, p.do, false)
	if p.do < len(t) {
		out = append(out, KomadIsjecka{Tekst: "…"})
	}
	return out
}
