package propisi

import (
	"regexp"
	"strconv"
	"strings"

	"gocop/internal/models"
)

// Upit je upit pretrage propisa: točka ili članak kad je upit broj, inače
// korijeni riječi. Tocka i Clanak su oznake dijela („xxiii”, „clanak-119”).
type Upit struct {
	Tocka, Clanak string
	Korijeni      []string
}

var (
	upitTocka       = regexp.MustCompile(`(?i)^(?:točk[aeiu]|toč\.|t\.)\s*([ivxlc]+|\d+)\.?$`)
	upitRimski      = regexp.MustCompile(`^([IVXLC]+)\.?$`)
	upitClanak      = regexp.MustCompile(`(?i)^(?:član(?:ak|ka|ku)|čl\.?)\s*(\d+\.?[a-z]?)\.?$`)
	upitSamoBroj    = regexp.MustCompile(`^(\d+\.?[a-z]?)\.?$`)
	upitRazmaciBroj = regexp.MustCompile(`\s+`)
)

// RastaviUpit čita upit pretrage. „XXIII.”, „XXIII”, „točka XXIII” i „t. 23”
// traže točku, a „čl. 119”, „članak 119.” i sam broj „119” članak. Malo „i”
// nije rimski broj: rimski broj bez riječi „točka” mora biti velikim
// slovima. Ostalo se traži po korijenima riječi.
func RastaviUpit(q string) Upit {
	q = strings.TrimSpace(upitRazmaciBroj.ReplaceAllString(q, " "))
	if g := upitTocka.FindStringSubmatch(q); g != nil {
		if t := tockaUpita(g[1]); t != "" {
			return Upit{Tocka: t}
		}
	}
	if g := upitRimski.FindStringSubmatch(q); g != nil {
		if t := tockaUpita(g[1]); t != "" {
			return Upit{Tocka: t}
		}
	}
	if g := upitClanak.FindStringSubmatch(q); g != nil {
		return Upit{Clanak: KljucDijela(models.DioClanak, g[1], "", 0)}
	}
	if g := upitSamoBroj.FindStringSubmatch(q); g != nil {
		return Upit{Clanak: KljucDijela(models.DioClanak, g[1], "", 0)}
	}
	return Upit{Korijeni: Korijeni(q)}
}

// tockaUpita je oznaka točke Plana iz rimskog ili arapskog broja („23” je
// „xxiii”); broj koji nije točka daje prazno.
func tockaUpita(b string) string {
	if n, err := strconv.Atoi(b); err == nil {
		b = Rimski(n)
	}
	if _, ok := IzRimskog(b); !ok {
		return ""
	}
	return strings.ToLower(b)
}
