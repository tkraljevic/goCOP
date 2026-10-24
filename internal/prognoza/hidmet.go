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
	reHidmetDatum  = regexp.MustCompile(`>\s*(\d{2})\.(\d{2})\.\s*<`)
	reHidmetRed    = regexp.MustCompile(`(?s)<tr[^>]*>\s*<td[^>]*>(?:&nbsp;|\s)*([^<&]+?)(?:&nbsp;|\s)*</td>\s*<td[^>]*><a[^>]*>([^<]+)</a></td>(.*?)</tr>`)
	reHidmetCelija = regexp.MustCompile(`<td[^>]*>([^<]*)</td>`)
)

// CitajHidmet razlaže tablicu prognoze. Vrijednosti su jutarnje, za 07 h,
// kao i u mađarskoj prognozi; prvi stupac je vodostaj na dan izdanja, a
// izdanje je u 12 h tog dana. Godine u tablici nema, pa se uzima iz sada.
// Kad datumi ne idu dan za danom, letve stižu skraćene na uredni početak,
// uz grešku *NeuredniDatumi; zaglavlje s greškom pomaka sata čita se redom
// po danima, uz napomenu iste vrste.
func CitajHidmet(stranica string, sada time.Time) ([]Letva, error) {
	datumi := datumiHidmeta(stranica, sada)
	if len(datumi) < 2 {
		return nil, fmt.Errorf("u tablici nema datuma — je li se stranica promijenila?")
	}
	izdano := time.Date(datumi[0].Year(), datumi[0].Month(), datumi[0].Day(), 12, 0, 0, 0, models.Zagreb)
	stupaca := len(datumi)
	datumi, neuredno := zaglavljeHidmeta(datumi)
	var out []Letva
	for _, m := range reHidmetRed.FindAllStringSubmatch(stranica, -1) {
		l := Letva{Rijeka: strings.TrimSpace(m[1]), Naziv: strings.TrimSpace(m[2]), Izdano: izdano}
		if stupciHidmeta(&l, m[3], stupaca, datumi) >= 2 {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("u tablici nema nijedne postaje")
	}
	return out, neuredno.uz(izdano)
}

// stupciHidmeta upiše letvi vrijednosti iz ćelija reda po stupcima: stupac
// i pripada datumi[i], a ćelija bez broja („-”) je dan bez vrijednosti. Iza
// stupaca zaglavlja stoje granice obrane; stupci iza datumi (skraćeno
// neuredno zaglavlje) ne upisuju se. Vraća koliko je brojeva u stupcima
// zaglavlja: postaja s manje od dva se preskače.
func stupciHidmeta(l *Letva, celije string, stupaca int, datumi []time.Time) int {
	brojeva := 0
	for i, c := range reHidmetCelija.FindAllStringSubmatch(celije, stupaca) {
		cm, err := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(c[1], "&nbsp;", "")))
		switch {
		case err != nil: // „-” ili prazno: za taj dan nema vrijednosti
			continue
		case i == 0:
			l.Danas, l.ImaDanas = cm, true
		case i < len(datumi):
			l.Dani = append(l.Dani, Dan{Kad: datumi[i].UTC(), Cm: cm})
		}
		brojeva++
	}
	return brojeva
}

// datumiHidmeta su prvih pet datuma tablice (dan izdanja i četiri dana
// prognoze), u 07 h. Godine u tablici nema, pa se uzima iz sada, uz
// prijelaz godine.
func datumiHidmeta(stranica string, sada time.Time) []time.Time {
	var datumi []time.Time
	lok := sada.In(models.Zagreb)
	for _, m := range reHidmetDatum.FindAllStringSubmatch(stranica, -1) {
		d, _ := strconv.Atoi(m[1])  // regex jamči dvije znamenke
		mj, _ := strconv.Atoi(m[2]) // regex jamči dvije znamenke
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
	return datumi
}

// DohvatiHidmet čita srpsku prognozu; kao CitajHidmet, uz *NeuredniDatumi
// vraća i letve.
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
