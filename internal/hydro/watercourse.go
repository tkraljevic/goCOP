package hydro

import (
	"regexp"
	"strings"
)

var (
	// tip vode ispred naziva: "rijeka Sava" → "Sava", "akumulacija Jošava" → "Jošava".
	// Ne dira nazive u kojima je tip usred imena ("Zapadni lateralni kanal Biđ polja").
	reWaterPrefix = regexp.MustCompile(`(?i)^(?:rijeka|rj\.|r\.|potok|p\.|kanal|k\.|prokop|akumulacija|retencija|jezero)\s+`)
	// oznake obale: "l.o.", "d. o.", "l.o. i d.o." — obala nije zaseban vodotok
	reBankSuffix = regexp.MustCompile(`(?i)[\s,;-]*\b[ld]\s*\.?\s*o\.?\b.*$`)
	// pojašnjenje u zagradi: "potok Karašica (Baranja)" → "Baranja"
	reQualifier = regexp.MustCompile(`\(([^)]*)\)`)
	reParens    = regexp.MustCompile(`\s*\([^)]*\)`)
)

// ParseWatercourse izvlači naziv vodotoka iz opisa dionice.
//
// Opis dionice počinje vodotokom, pa slijede oznaka obale i stacionaža:
//
//	"rijeka Sava, l.o.; granica - cestovni most Gunja-Brčko; rkm 212+080..."
//	"Žirovnica, l.o. i d.o.; Dvor - Komora; rkm 0+000 - 27+000"
//	"Krapina; d.o.; „Podsused-Žejinci"; rkm 0+000-19+140"
//
// Lijeva i desna obala nisu zasebni vodotoci — "Drava, l.o." i "Drava, d.o."
// obje su Drava, pa oznaka obale ne smije završiti u nazivu.
func ParseWatercourse(sectionDescription string) string {
	name, _ := ParseWatercourseWithKind(sectionDescription)
	return name
}

// ParseWatercourseWithKind uz naziv vraća i vrstu vode navedenu u opisu.
//
// Vrsta razlikuje vode istog imena: "rijeka Pakra" i "akumulacija Pakra" dva su
// vodna tijela, a opis dionice kaže koje od njih štiti.
func ParseWatercourseWithKind(sectionDescription string) (name, kind string) {
	name = sectionDescription

	// Naziv je ono do prve granice opisa — zareza, točke sa zarezom ili zagrade
	if idx := strings.IndexAny(name, ",;("); idx >= 0 {
		name = name[:idx]
	}
	// Odvojeni crtom slijedi opis prostiranja: "Bednja - od ušća u Dravu do Tuhovca"
	if idx := strings.Index(name, " - "); idx >= 0 {
		name = name[:idx]
	}

	name = reBankSuffix.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)

	if m := reWaterPrefix.FindString(name); m != "" {
		kind = NormalizeWaterKind(strings.TrimSpace(m))
		name = reWaterPrefix.ReplaceAllString(name, "")
	}

	name = strings.Join(strings.Fields(name), " ")
	return strings.Trim(name, " .,-–—"), kind
}

// NormalizeWaterKind svodi kraticu vrste vode na puni oblik ("p." → "potok")
func NormalizeWaterKind(prefix string) string {
	switch strings.ToLower(strings.TrimSpace(strings.TrimSuffix(prefix, "."))) {
	case "rijeka", "rj", "r":
		return "rijeka"
	case "potok", "p":
		return "potok"
	case "kanal", "k":
		return "kanal"
	case "prokop":
		return "prokop"
	case "akumulacija":
		return "akumulacija"
	case "retencija":
		return "retencija"
	case "jezero":
		return "jezero"
	default:
		return ""
	}
}

// WatercourseKey normalizira naziv vode za usporedbu: bez dijakritike, bez
// vrste vode i bez pojašnjenja u zagradi ("rijeka Karašica (miholjačka)" → "karasica").
// Vode istog imena namjerno dobivaju isti ključ — tako se prepozna višeznačnost.
func WatercourseKey(name string) string {
	key := FoldDiacritics(strings.ToLower(strings.TrimSpace(name)))
	key = reParens.ReplaceAllString(key, "")
	key = reWaterPrefix.ReplaceAllString(strings.TrimSpace(key), "")
	key = reNonSlug.ReplaceAllString(key, " ")
	return strings.TrimSpace(key)
}

// WatercourseCode gradi stabilnu šifru vodnog tijela iz službenog naziva
func WatercourseCode(officialName string) string {
	return Slug(officialName)
}

// Qualifier vraća pojašnjenje iz zagrade u službenom nazivu, ili prazno
func Qualifier(officialName string) string {
	if m := reQualifier.FindStringSubmatch(officialName); m != nil {
		return m[1]
	}
	return ""
}

// Candidate je vodno tijelo koje odgovara traženom nazivu
type Candidate struct {
	Code      string
	Kind      string
	Qualifier string // pojašnjenje iz zagrade: "Baranja", "miholjačka"
}

// ResolveWatercourse bira vodno tijelo za naziv iz dokumentacije.
//
// Vode istog imena razlikuju se po vrsti ("rijeka Pakra" nije "akumulacija
// Pakra") i po pojašnjenju u zagradi, koje odgovara branjenom području
// ("potok Karašica (Baranja)" pripada Malom slivu Baranja). Kad ni jedno ni
// drugo ne razriješi izbor, vraća prazno — veza se ne postavlja.
func ResolveWatercourse(index map[string][]Candidate, name, kind, areaText string) string {
	key := WatercourseKey(name)
	if key == "" {
		return ""
	}

	options := index[key]
	switch len(options) {
	case 0:
		return ""
	case 1:
		return options[0].Code
	}

	if kind != "" {
		if only, ok := onlyOne(options, func(c Candidate) bool { return c.Kind == kind }); ok {
			return only
		}
	}

	if areaText != "" {
		folded := FoldDiacritics(strings.ToLower(areaText))
		if only, ok := onlyOne(options, func(c Candidate) bool {
			return c.Qualifier != "" && qualifierMatchesArea(c.Qualifier, folded)
		}); ok {
			return only
		}
	}

	// Voda bez pojašnjenja je osnovna — "Gacka" naspram "Gacka (sjeverni krak)"
	if only, ok := onlyOne(options, func(c Candidate) bool { return c.Qualifier == "" }); ok {
		return only
	}

	return ""
}

func onlyOne(options []Candidate, match func(Candidate) bool) (string, bool) {
	var found string
	count := 0
	for _, c := range options {
		if match(c) {
			found = c.Code
			count++
		}
	}
	return found, count == 1
}

// qualifierMatchesArea uspoređuje pojašnjenje vode s nazivom branjenog područja.
// Uspoređuje se korijen riječi jer se oblici razlikuju: pojašnjenje
// "(miholjačka)" pripada području čija je ispostava u Donjem Miholjcu.
func qualifierMatchesArea(qualifier, foldedAreaText string) bool {
	for _, word := range strings.Fields(FoldDiacritics(strings.ToLower(qualifier))) {
		word = strings.Trim(word, ".,-()")
		if len([]rune(word)) < 4 {
			continue
		}
		stem := word
		if len([]rune(stem)) > 5 {
			stem = string([]rune(stem)[:len([]rune(stem))-2])
		}
		if strings.Contains(foldedAreaText, stem) {
			return true
		}
	}
	return false
}
