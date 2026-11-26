package propisi

import (
	"regexp"
	"strings"
	"unicode"
)

// razmaciURetku su razmaci koje Go-ov \s ne hvata ili koje NN piše dvaput:
// tvrdi razmak (U+00A0) i obični razmaci, tabulatori i CR.
var razmaciURetku = regexp.MustCompile(`[ \t\r\f\v\x{00A0}]+`)

// ocisti priprema tekst odlomka za uzorke: miče meku crticu (U+00AD),
// tvrdi razmak pretvara u obični, sažima razmake i miče prazne retke.
// Prijelom retka (<br>) ostaje kao „\n”.
func ocisti(s string) string {
	s = strings.ReplaceAll(s, "\u00ad", "")
	var retci []string
	for _, r := range strings.Split(s, "\n") {
		if r = strings.TrimSpace(razmaciURetku.ReplaceAllString(r, " ")); r != "" {
			retci = append(retci, r)
		}
	}
	return strings.Join(retci, "\n")
}

// nadznaci su znamenke napisane kao eksponent: <sup>3</sup> u „m3” je „³”.
var nadznaci = strings.NewReplacer("0", "⁰", "1", "¹", "2", "²", "3", "³", "4", "⁴", "5", "⁵", "6", "⁶", "7", "⁷", "8", "⁸", "9", "⁹")

// nadznak piše znamenke iz <sup> kao eksponent, a ostale znakove ostavlja.
func nadznak(s string) string {
	return nadznaci.Replace(s)
}

// sumKlase su klase odlomaka NN koje služe samo za izgled ili za stranicu
// tiskanog izdanja, pa ne govore ništa o vrsti odlomka.
var sumKlase = regexp.MustCompile(`^(box_\d+|pleft|pcenter|pright|pjustify|f_\d+|donositelj_\d+|broj_\d+)$`)

// kopijaKlase je nastavak kojim NN označava preslikanu klasu („T-9-8-Copy-3”).
var kopijaKlase = regexp.MustCompile(`-copy-\d+$`)

// klasaOdlomka vraća prvu značajnu klasu odlomka, malim slovima i bez
// završnih crtica i nastavka „-copy-N”: „Box_1 T-9-8 pleft” je „t-9-8”, a
// „Clanak--” je „clanak”. Odlomak bez značajne klase daje prazno.
func klasaOdlomka(attr string) string {
	for _, k := range strings.Fields(strings.ToLower(attr)) {
		if sumKlase.MatchString(k) {
			continue
		}
		k = strings.TrimRight(kopijaKlase.ReplaceAllString(k, ""), "-")
		if k != "" {
			return k
		}
	}
	return ""
}

// MijesanoPismo javlja ima li tekst i latinična i ćirilična slova. U NN se
// to događa kad se u riječ kopira ćirilično slovo koje izgleda kao
// latinično („оd” s ćiriličnim „о”).
func MijesanoPismo(s string) bool {
	latinica, cirilica := false, false
	for _, r := range s {
		latinica = latinica || unicode.Is(unicode.Latin, r)
		cirilica = cirilica || unicode.Is(unicode.Cyrillic, r)
	}
	return latinica && cirilica
}
