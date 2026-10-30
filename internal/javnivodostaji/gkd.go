// Bavarske postaje koje nisu u PEGELONLINE-u čitaju se sa službene stranice
// Gewässerkundlicher Dienst Bayern. Stranica daje 15-minutna očitanja;
// u operativni niz uzimaju se stvarna očitanja na punom satu.
package javnivodostaji

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

const PodrijetloGKD = "gkd.bayern.de"

var (
	reGKDPostaja = regexp.MustCompile(`(?i)/de/fluesse/wasserstand/(?:[^/]+/)+[^/]+-(\d{8})/messwerte(?:/tabelle)?$`)
	reGKDRedak   = regexp.MustCompile(`<td[^>]*>\s*(\d{2})\.(\d{2})\.(\d{4})\s+(\d{2}):(\d{2})\s+Uhr\s*</td>\s*<td[^>]*class="center"[^>]*>\s*(-?\d+)\s*</td>`)
)

type GKD struct {
	Client *Client
	Base   string // zamjenska adresa u testu
}

func (g GKD) Naziv() string { return PodrijetloGKD }

func (g GKD) Prepoznaje(adresa string) bool { return PostajaGKDIzAdrese(adresa) != "" }

func PostajaGKDIzAdrese(adresa string) string {
	u, ok := adresaIzvora(adresa, "gkd.bayern.de")
	if !ok {
		return ""
	}
	m := reGKDPostaja.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return m[1]
}

func (g GKD) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	if PostajaGKDIzAdrese(adresa) == "" {
		return nil, fmt.Errorf("GKD adresa nema broj postaje")
	}
	adresa = zaDohvat(adresa, "gkd.bayern.de")
	if g.Base != "" {
		adresa = g.Base
	}
	c := g.Client
	if c == nil {
		c = &Client{}
	}
	b, err := c.dohvati(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return CitajGKD(string(b))
}

func CitajGKD(html string) ([]Redak, error) {
	var out []Redak
	vidjeno := make(map[int64]bool)
	for _, m := range reGKDRedak.FindAllStringSubmatch(html, -1) {
		min, _ := strconv.Atoi(m[5])
		if min != 0 {
			continue
		}
		d, _ := strconv.Atoi(m[1])
		mj, _ := strconv.Atoi(m[2])
		god, _ := strconv.Atoi(m[3])
		sat, _ := strconv.Atoi(m[4])
		cm, _ := strconv.Atoi(m[6])
		kad := time.Date(god, time.Month(mj), d, sat, min, 0, 0, models.Zagreb).UTC()
		if vidjeno[kad.Unix()] {
			continue
		}
		vidjeno[kad.Unix()] = true
		out = append(out, Redak{Kad: kad, LevelCm: intPtr(cm)})
	}
	if len(out) == 0 {
		if strings.Contains(html, "Wasserstand [cm]") {
			return nil, fmt.Errorf("GKD tablica nema nijedno valjano očitanje na punom satu")
		}
		if naslov := naslovStranice(html); naslov != "" {
			// naslov kaže je li stigla njihova stranica bez tablice ili
			// nešto drugo, recimo obavijest o održavanju ili zabrani pristupa
			return nil, fmt.Errorf("na GKD stranici „%s” nema tablice očitanja — je li se stranica promijenila?", naslov)
		}
		return nil, fmt.Errorf("na GKD stranici nema tablice očitanja — je li se stranica promijenila?")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}
