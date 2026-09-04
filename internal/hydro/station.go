package hydro

import (
	"regexp"
	"strconv"
	"strings"

	"gocop/internal/models"
)

var (
	// stacionaža u nazivu vodomjera: "rkm 271+900", "km 60+486", "pkm 12+160", "27+760 km"
	reStationing = regexp.MustCompile(`(?i)\b(r|p|k)?km\s*([0-9]+(?:[+.,][0-9]+)*)|\b([0-9]+\+[0-9]+)\s*km\b`)
	// kota nule vodomjera na kraju naziva: "(76,28)"
	reZeroDatum = regexp.MustCompile(`\(\s*([0-9]{1,4}[.,][0-9]{1,2})\s*\)`)
	// prag izražen isključivo brojem centimetara: "+ 600", "+1080", "-20", "580"
	reThresholdCm = regexp.MustCompile(`^[+-]?[0-9]{1,4}$`)
	// prefiks vodotoka ispred naziva postaje (nad ASCII-ziranim tekstom):
	// "Sava - Jasenovac", "Česma - Čazma"
	reRiverPrefix = regexp.MustCompile(`(?i)^(sava|drava|dunav|kupa|una|korana|mura|neretva|bosut|vuka|orljava|cesma|krapina|sutla|lonja|ilova|pakra|karasica|vucica|zrmanja|krka|cetina|glina|odra|save)\s*[-–—]?\s+`)
	// rijeka navedena u samom nazivu vodomjera: "Drava - Osijek", "Korana - Karlovac"
	reNamedRiver = regexp.MustCompile(`(?i)^\s*(sava|drava|dunav|kupa|una|korana|mura|neretva|bosut|vuka|orljava|česma|krapina|sutla|lonja|ilova|pakra|karašica|vučica|zrmanja|krka|cetina|glina|odra|bednja|mrežnica|dobra|spačva|biđ)\s*[-–—]\s*`)
)

// ParseThresholdCm vraća prag u centimetrima samo kad je zapisan čistim brojem.
// Kote ("206,30 m n. m.") i upute ("Prema Pravilniku akumulacije Borovik")
// vraćaju nil — čuvaju se kao izvorni tekst i ne ulaze u izračun faze obrane.
func ParseThresholdCm(raw string) *int {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	if cleaned == "" || !reThresholdCm.MatchString(cleaned) {
		return nil
	}
	value, err := strconv.Atoi(strings.TrimPrefix(cleaned, "+"))
	if err != nil {
		return nil
	}
	return &value
}

// ParseZeroDatum čita kotu nule vodomjera iz naziva, npr. "Županja, rkm 271+900 (76,28)".
// Kota 0,00 znači da u dokumentaciji nije upisana, pa se vraća nil.
func ParseZeroDatum(stationName string) *float64 {
	m := reZeroDatum.FindStringSubmatch(stationName)
	if m == nil {
		return nil
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
	if err != nil || value == 0 {
		return nil
	}
	return &value
}

// ParseStationName razlaže izvorni zapis vodomjera na naziv i stacionažu
func ParseStationName(raw string) (name, stationing string) {
	work := strings.TrimSpace(reZeroDatum.ReplaceAllString(raw, ""))

	if loc := reStationing.FindStringIndex(work); loc != nil {
		stationing = strings.Join(strings.Fields(work[loc[0]:loc[1]]), " ")
		name = work[:loc[0]]
	} else {
		name = work
	}

	name = strings.Trim(strings.TrimSpace(name), ",;-–— ")
	name = strings.Join(strings.Fields(name), " ")
	return name, stationing
}

// StationKey normalizira naziv radi prepoznavanja istog vodomjera zapisanog
// na više načina ("Ustava Trebež", "Sava Ustava Trebež", "Sava - ustava Trebež")
func StationKey(name string) string {
	key := FoldDiacritics(strings.ToLower(strings.TrimSpace(name)))
	key = strings.Join(strings.Fields(key), " ")
	key = reRiverPrefix.ReplaceAllString(key, "")
	return strings.Trim(key, " ,.-")
}

// ResolveStationWatercourse utvrđuje vodu na kojoj vodomjer STOJI i vraća
// naziv te oznaku podrijetla (models.WatercourseFrom*).
//
// To nije voda dionice za koju je postaja mjerodavna: vodomjer Batina stoji na
// Dunavu, a mjerodavan je i za dionice potoka Karašice na ušću u Dunav. Zato se
// vodotok NE preuzima iz dionice — uzima se samo kad ga dokumentacija tvrdi:
//
//  1. rijeka navedena u nazivu vodomjera ("Korana - Karlovac"),
//  2. stacionaža vodomjera koja upada u raspon dionica jedne jedine vode
//     (Batina rkm 1424,85 upada samo u dunavske raspone).
//
// Kad stacionaža upada u raspone više različitih voda — Osijek rkm 19,10 je i
// Drava i Poganovačko-kravički kanal — vodotok ostaje neodređen. Prazno polje
// operater popuni, kriva voda ga zavara.
func ResolveStationWatercourse(sourceName, stationing string, sectionDescs []string) (name, source string) {
	if m := reNamedRiver.FindStringSubmatch(sourceName); m != nil {
		return CapitalizeFirst(strings.TrimSpace(m[1])), models.WatercourseFromName
	}

	km, ok := ParseStationingKm(stationing)
	if !ok {
		return "", models.WatercourseUndetermined
	}

	candidates := make(map[string]bool)
	for _, desc := range sectionDescs {
		lo, hi, ok := ParseRange(desc)
		if !ok || km < lo-StationingTolerance || km > hi+StationingTolerance {
			continue
		}
		if river := ParseWatercourse(desc); river != "" {
			candidates[river] = true
		}
	}

	if len(candidates) != 1 {
		return "", models.WatercourseUndetermined
	}
	for river := range candidates {
		return river, models.WatercourseFromStationing
	}

	return "", models.WatercourseUndetermined
}
