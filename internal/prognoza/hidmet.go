package prognoza

// Srpska hidrološka služba (RHMZ Srbije) objavljuje prognozu vodostaja na
// hidmet.gov.rs, svaki dan u 12 sati: za svaku postaju vodostaj tog dana i
// četiri dana unaprijed. Među postajama su i one nasuprot našima na Dunavu —
// Bezdan uz Batinu, Apatin, Bogojevo i Bačka Palanka uz Ilok — pa se na
// pregledu prognoza stavljaju uz naše, drugom bojom, jer nisu naše.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// PodrijetloHidmet je ono što stoji uz srpsku prognozu kao izvor.
const PodrijetloHidmet = "hidmet.gov.rs"

// AdresaHidmet je stranica prognoze vodostaja.
const AdresaHidmet = "https://www.hidmet.gov.rs/latin/prognoza/prognoza_voda.php"

// SrpskeLetve preslikava njihove nazive u naše šifre postaja.
var SrpskeLetve = map[string]string{
	"BEZDAN":        "bezdan",
	"APATIN":        "apatin",
	"BOGOJEVO":      "bogojevo",
	"BAČKA PALANKA": "backa-palanka",
	// Ostale postaje iz njihove prognoze, upisane kao strane letve radi povijesti
	"NOVI SAD":          "novi-sad",
	"SLANKAMEN":         "slankamen",
	"ZEMUN":             "zemun",
	"PANČEVO":           "pancevo",
	"SMEDEREVO":         "smederevo",
	"NOVI KNEŽEVAC":     "novi-knezevac",
	"SENTA":             "senta",
	"TITEL":             "titel",
	"SREMSKA MITROVICA": "sremska-mitrovica",
	"ŠABAC":             "sabac",
	"BEOGRAD":           "beograd",
	"VARVARIN":          "varvarin",
	"ĆUPRIJA":           "cuprija",
	"BAGRDAN":           "bagrdan",
	"LJUBIČEVSKI MOST":  "ljubicevski-most",
	"ALEKSINAC":         "aleksinac",
	"JASIKA":            "jasika",
}

// SifraSrpske vraća našu šifru za njihov naziv; prazno kad je ne vodimo.
func SifraSrpske(naziv string) string {
	return SrpskeLetve[strings.ToUpper(strings.TrimSpace(naziv))]
}

var (
	reHidmetDatum = regexp.MustCompile(`>\s*(\d{2})\.(\d{2})\.\s*<`)
	reHidmetRed   = regexp.MustCompile(`(?s)<tr[^>]*>\s*<td[^>]*>(?:&nbsp;|\s)*([^<&]+?)(?:&nbsp;|\s)*</td>\s*<td[^>]*><a[^>]*>([^<]+)</a></td>(.*?)</tr>`)
	reHidmetBroj  = regexp.MustCompile(`<td[^>]*>\s*(-?\d+)\s*</td>`)
)

// CitajHidmet razlaže tablicu prognoze. Vrijednosti su jutarnje, za 07 h,
// kao i u mađarskoj prognozi; prvi stupac je vodostaj na dan izdanja, a
// izdanje je u 12 h tog dana. Godine u tablici nema, pa se uzima iz sada.
func CitajHidmet(stranica string, sada time.Time) ([]Letva, error) {
	var datumi []time.Time
	lok := sada.In(models.Zagreb)
	for _, m := range reHidmetDatum.FindAllStringSubmatch(stranica, -1) {
		d, _ := strconv.Atoi(m[1])
		mj, _ := strconv.Atoi(m[2])
		g := lok.Year()
		if mj == 1 && lok.Month() == 12 {
			g++
		} else if mj == 12 && lok.Month() == 1 {
			g--
		}
		datumi = append(datumi, time.Date(g, time.Month(mj), d, 7, 0, 0, 0, models.Zagreb))
		if len(datumi) == 5 {
			break
		}
	}
	if len(datumi) < 2 {
		return nil, fmt.Errorf("u tablici nema datuma — je li se stranica promijenila?")
	}
	izdano := time.Date(datumi[0].Year(), datumi[0].Month(), datumi[0].Day(), 12, 0, 0, 0, models.Zagreb)
	var out []Letva
	for _, m := range reHidmetRed.FindAllStringSubmatch(stranica, -1) {
		brojevi := reHidmetBroj.FindAllStringSubmatch(m[3], -1)
		if len(brojevi) < 2 {
			continue
		}
		l := Letva{Rijeka: strings.TrimSpace(m[1]), Naziv: strings.TrimSpace(m[2]), Izdano: izdano}
		for i, b := range brojevi {
			if i >= len(datumi) {
				break // iza prognoze stoje granice obrane
			}
			cm, _ := strconv.Atoi(b[1])
			if i == 0 {
				l.Danas, l.ImaDanas = cm, true
				continue
			}
			l.Dani = append(l.Dani, Dan{Kad: datumi[i].UTC(), Cm: cm})
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("u tablici nema nijedne postaje")
	}
	return out, nil
}

// DohvatiHidmet čita srpsku prognozu.
func DohvatiHidmet(ctx context.Context, klijent *http.Client) ([]Letva, error) {
	if klijent == nil {
		klijent = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AdresaHidmet, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goCOP (usporedba prognoza vodostaja)")
	resp, err := klijent.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", AdresaHidmet, resp.Status)
	}
	return CitajHidmet(string(b), time.Now())
}
