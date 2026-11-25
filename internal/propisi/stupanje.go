package propisi

import (
	"regexp"
	"strconv"
	"time"

	"gocop/internal/models"
)

// danaOdObjave su redni brojevi dana od objave kako ih pišu odredbe o
// stupanju na snagu.
var danaOdObjave = map[string]int{"prvoga": 1, "osmoga": 8, "petnaestoga": 15, "tridesetoga": 30}

// mjeseciGenitiv su mjeseci u genitivu, kako stoje u datumu („1. kolovoza 2021.”).
var mjeseciGenitiv = map[string]time.Month{
	"siječnja": time.January, "veljače": time.February, "ožujka": time.March,
	"travnja": time.April, "svibnja": time.May, "lipnja": time.June, "srpnja": time.July,
	"kolovoza": time.August, "rujna": time.September, "listopada": time.October,
	"studenoga": time.November, "studenog": time.November, "prosinca": time.December,
}

// uzorakStupanja hvata odredbu o stupanju na snagu: „… dana od dana objave”,
// „danom objave” ili stalni datum.
var uzorakStupanja = regexp.MustCompile(`stupa na snagu\s+(?:` +
	`((prvoga|osmoga|petnaestoga|tridesetoga)\s+dana od dana objave)` +
	`|(danom objave)` +
	`|((\d{1,2})\.\s*(siječnja|veljače|ožujka|travnja|svibnja|lipnja|srpnja|kolovoza|rujna|listopada|studenoga?|prosinca)\s+(\d{4})\.))`)

// StupaNaSnagu traži odredbu o stupanju na snagu u dijelovima akta i iz
// datuma objave računa dan: „osmoga dana od dana objave” je osam dana iza
// objave, „danom objave” dan objave, a stalni datum je taj datum. Izraz je
// tekst odredbe iza „stupa na snagu”. ok je false kad odredbe nema ili kad
// dan ovisi o datumu objave, a on nije poznat.
func StupaNaSnagu(d []models.DioPropisa, objava time.Time) (dan time.Time, izraz string, ok bool) {
	for _, dio := range d {
		for _, s := range dio.Stavci {
			if s.Navod {
				continue
			}
			if g := uzorakStupanja.FindStringSubmatch(s.Tekst); g != nil {
				dan, ok = danStupanja(g, objava)
				return dan, g[1] + g[3] + g[4], ok
			}
		}
	}
	return time.Time{}, "", false
}

// danStupanja računa dan iz pogotka uzorakStupanja.
func danStupanja(g []string, objava time.Time) (time.Time, bool) {
	if g[4] != "" {
		d, _ := strconv.Atoi(g[5])   // uzorak jamči znamenke
		god, _ := strconv.Atoi(g[7]) // uzorak jamči znamenke
		mjesto := objava.Location()
		return time.Date(god, mjeseciGenitiv[g[6]], d, 0, 0, 0, 0, mjesto), true
	}
	if objava.IsZero() {
		return time.Time{}, false
	}
	return objava.AddDate(0, 0, danaOdObjave[g[2]]), true
}
