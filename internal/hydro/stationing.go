// Paket hydro sadrži parsere i heuristike nad hidrološkom dokumentacijom
// Hrvatskih voda: stacionaže, pragove obrane, nazive vodomjera i vodotoka.
//
// Sve ovdje je čisto — bez baze i bez mreže — pa isti kod koriste seed,
// obrasci u sučelju i, kasnije, uvoz i sinkronizacija. Kad korisnik upiše
// "rkm 1.424,85", to se čita istim parserom kojim je pročitana dokumentacija.
package hydro

import (
	"regexp"
	"strconv"
	"strings"
)

// StationingTolerance je dopušteno odstupanje u kilometrima pri usporedbi
// stacionaže vodomjera s rasponom dionice — vodomjer zna stajati na samoj međi.
const StationingTolerance = 0.5

var (
	// stacionaža kao broj: "rkm 271+900", "rkm 1.424,85", "km 19,10"
	reStationingValue = regexp.MustCompile(`(?i)(?:r|p|k)?km\s*([0-9][0-9.,+]*)`)
	// raspon stacionaže dionice: "rkm 212+080 - 230+700"
	reStationingRange = regexp.MustCompile(`(?i)(?:r|p|k)?km\s*([0-9][0-9.+]*)\s*[-–—]\s*([0-9][0-9.+]*)`)
	reNonSlug         = regexp.MustCompile(`[^a-z0-9]+`)

	diacriticsReplacer = strings.NewReplacer(
		"č", "c", "ć", "c", "ž", "z", "š", "s", "đ", "d",
		"Č", "C", "Ć", "C", "Ž", "Z", "Š", "S", "Đ", "D",
	)
)

// FoldDiacritics svodi hrvatske dijakritike na ASCII radi usporedbe naziva
func FoldDiacritics(s string) string {
	return diacriticsReplacer.Replace(s)
}

// CapitalizeFirst diže prvo SLOVO, ne prvi bajt — "česma" ima dvobajtni prvi
// znak pa bi rezanje po bajtu razbilo naziv
func CapitalizeFirst(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}

// Slug gradi stabilnu šifru iz naziva: "potok Karašica (Baranja)" → "potok-karasica-baranja"
func Slug(name string) string {
	slug := reNonSlug.ReplaceAllString(FoldDiacritics(strings.ToLower(name)), "-")
	return strings.Trim(slug, "-")
}

// ParseKm pretvara zapis stacionaže u kilometre: "271+900" → 271.9,
// "1.424,85" → 1424.85, "19,10" → 19.1
func ParseKm(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)

	// "271+900" — kilometri i metri
	if km, meters, found := strings.Cut(raw, "+"); found {
		whole, err := strconv.ParseFloat(strings.ReplaceAll(km, ".", ""), 64)
		if err != nil {
			return 0, false
		}
		part, err := strconv.ParseFloat(meters, 64)
		if err != nil {
			return 0, false
		}
		return whole + part/1000, true
	}

	// "1.424,85" — točka je razdjelnik tisućica, zarez decimalni
	normalized := strings.ReplaceAll(raw, ".", "")
	normalized = strings.ReplaceAll(normalized, ",", ".")
	value, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// ParseStationingKm čita stacionažu iz teksta s oznakom ("rkm 271+900") u kilometre
func ParseStationingKm(stationing string) (float64, bool) {
	m := reStationingValue.FindStringSubmatch(stationing)
	if m == nil {
		return 0, false
	}
	return ParseKm(m[1])
}

// ParseRange čita raspon stacionaže iz opisa dionice, uređen od manje prema većoj
func ParseRange(desc string) (lo, hi float64, ok bool) {
	m := reStationingRange.FindStringSubmatch(desc)
	if m == nil {
		return 0, 0, false
	}
	a, okA := ParseKm(m[1])
	b, okB := ParseKm(m[2])
	if !okA || !okB {
		return 0, 0, false
	}
	if a > b {
		a, b = b, a
	}
	return a, b, true
}
