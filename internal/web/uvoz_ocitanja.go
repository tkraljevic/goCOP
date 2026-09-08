package web

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gocop/internal/models"
)

// Uvoz zalijepljenih očitanja s letva.voda.hr.
//
// Ispis s letve izgleda ovako:
//
//	Vodostaj (cm) - satni podaci za razdoblje 07.09.2026. - 07.09.2026.
//	Dunav - Batina (DHMZ)
//	07.09.2026. 00 h    -118
//	07.09.2026. 01 h    -118
//
// Zaglavlja se preskaču, a redak s datumom i satom prepoznaje se po obliku.
// Prima se i oblik bez sata, za dnevne vrijednosti.

var (
	reSatni  = regexp.MustCompile(`^\s*(\d{1,2})\.\s*(\d{1,2})\.\s*(\d{4})\.?\s+(\d{1,2})\s*h?\s+(-?[\d.,]+)\s*$`)
	reDnevni = regexp.MustCompile(`^\s*(\d{1,2})\.\s*(\d{1,2})\.\s*(\d{4})\.?\s+(-?[\d.,]+)\s*$`)
	reISO    = regexp.MustCompile(`^\s*(\d{4})-(\d{2})-(\d{2})[ T]?(\d{2})?:?\d*:?\d*\s*[;\t ]\s*(-?[\d.,]+)\s*$`)

	// Redak koji počinje datumom, ali se nije dao pročitati do kraja. Bez ovoga
	// bi „07.09.2026. 00 h  x118“ prošao kao zaglavlje i tiho nestao — a upravo
	// je tiho preskakanje najlakši način da se izgubi pola dana.
	reMozdaRedak = regexp.MustCompile(`^\s*(?:\d{1,2}\.\s*\d{1,2}\.\s*\d{4}|\d{4}-\d{2}-\d{2})`)
)

// ZalijepljenoOcitanje je jedan pročitani redak.
type ZalijepljenoOcitanje struct {
	Kad     time.Time
	Vrijedi float64
	Redak   int
	Greska  string

	// Usporedba s onim što već imamo
	Postoji  bool
	Staro    float64
	Razlicit bool
}

// citajZalijepljeno čita zalijepljeni ispis. Vraća i redak po redak, da se
// vidi što je pročitano, a što nije — tiho preskakanje redaka najlakše je
// mjesto da se izgubi pola dana.
func citajZalijepljeno(tekst string) ([]ZalijepljenoOcitanje, bool, error) {
	var out []ZalijepljenoOcitanje
	satni := false
	for i, r := range strings.Split(strings.ReplaceAll(tekst, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(r) == "" {
			continue
		}
		var d, m, g, h, v string
		switch {
		case reSatni.MatchString(r):
			p := reSatni.FindStringSubmatch(r)
			d, m, g, h, v = p[1], p[2], p[3], p[4], p[5]
			satni = true
		case reISO.MatchString(r):
			p := reISO.FindStringSubmatch(r)
			g, m, d, h, v = p[1], p[2], p[3], p[4], p[5]
			if h != "" {
				satni = true
			}
		case reDnevni.MatchString(r):
			p := reDnevni.FindStringSubmatch(r)
			d, m, g, v = p[1], p[2], p[3], p[4]
		default:
			if reMozdaRedak.MatchString(r) {
				out = append(out, ZalijepljenoOcitanje{
					Redak:  i + 1,
					Greska: "redak počinje datumom, ali se ne da pročitati: " + strings.TrimSpace(r),
				})
			}
			continue // zaglavlje ili prazan redak
		}
		sat := 0
		if h != "" {
			fmt.Sscanf(h, "%d", &sat)
		}
		var dd, mm, gg int
		fmt.Sscanf(d, "%d", &dd)
		fmt.Sscanf(m, "%d", &mm)
		fmt.Sscanf(g, "%d", &gg)
		z := ZalijepljenoOcitanje{Redak: i + 1}
		if mm < 1 || mm > 12 || dd < 1 || dd > 31 || sat > 23 {
			z.Greska = "datum ili sat izvan raspona"
			out = append(out, z)
			continue
		}
		z.Kad = time.Date(gg, time.Month(mm), dd, sat, 0, 0, 0, time.UTC)
		x, ok := parseBroj(v)
		if !ok {
			z.Greska = "vrijednost nije broj: " + v
			out = append(out, z)
			continue
		}
		z.Vrijedi = x
		out = append(out, z)
	}
	if len(out) == 0 {
		return nil, false, fmt.Errorf("nijedan redak nije prepoznat — očekuje se oblik „07.09.2026. 00 h  -118“")
	}
	return out, satni, nil
}

// usporedi označava koji su redci novi, koji isti, a koji se razlikuju od
// onoga što već imamo.
func usporedi(redci []ZalijepljenoOcitanje, postojece map[int64]models.SpojenaVrijednost) (novih, istih, razlicitih int) {
	for i := range redci {
		if redci[i].Greska != "" {
			continue
		}
		s, ima := postojece[redci[i].Kad.Unix()]
		if !ima {
			novih++
			continue
		}
		redci[i].Postoji, redci[i].Staro = true, s.Vrijednost
		if s.Vrijednost != redci[i].Vrijedi {
			redci[i].Razlicit = true
			razlicitih++
			continue
		}
		istih++
	}
	return
}

// PregledUvoza je ono što se pokaže prije nego što se išta upiše.
type PregledUvoza struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Station     *models.Station
	GaugeName   string
	Satni       bool
	Redci       []ZalijepljenoOcitanje
	Novih       int
	Istih       int
	Razlicitih  int
	Greske      int
	Od, Do      time.Time

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}
