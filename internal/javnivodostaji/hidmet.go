// Srpske letve s lijeve obale Dunava čita Republički hidrometeorološki zavod
// Srbije na hidmet.gov.rs. Stranica daje satne vrijednosti za zadnjih sedam
// dana, u tablici za čitanje:
//
//	20.09.2026 21:00     -115
//
// Vrijeme je UTC+1 cijele godine, što stranica i piše: ljetnog pomaka nema.
// Zato se ne čita u zoni Europe/Zagreb kao hrvatske stranice, nego u stalnom
// pomaku od jednog sata — ljeti bi inače cijeli niz bio sat u krivo.
//
// Zavod uz podatke stoji da su privremeni i nekontrolirani. Tako ih i
// vodimo: za usporedbu s našom obalom i za ranu sliku vala, ne kao ovjereno
// mjerenje.
package javnivodostaji

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PodrijetloHidmet je ono što stoji uz očitanje kao izvor
const PodrijetloHidmet = "hidmet.gov.rs"

// AdresaHidmetPostaje je stranica satnih vrijednosti jedne postaje.
// period=7 su zadnja sedam dana, koliko stranica uopće nudi.
const AdresaHidmetPostaje = "https://www.hidmet.gov.rs/latin/osmotreni/nrt_tabela_grafik.php"

// zonaHidmet je stalni UTC+1, bez ljetnog pomaka, kako stranica i kaže
var zonaHidmet = time.FixedZone("UTC+1", 3600)

var reHidmetPostaja = regexp.MustCompile(`(?i)hm_id=(\d+)`)

// reHidmetRedak prepoznaje redak tablice: datum i sat u jednoj ćeliji,
// vrijednost u sljedećoj, s razmacima i prijelomima retka između
var reHidmetRedak = regexp.MustCompile(
	`(\d{2})\.(\d{2})\.(\d{4})\s+(\d{1,2}):(\d{2})\s*</td>\s*<td[^>]*>(?:&nbsp;|\s)*(-?\d+)\s*</td>`)

// Hidmet čita stranicu srpskog zavoda
type Hidmet struct {
	Client *Client // zbog HTTP klijenta i zamjene adrese u testu
	Base   string  // prazno znači prava stranica
}

// Naziv je hidmet.gov.rs
func (h Hidmet) Naziv() string { return PodrijetloHidmet }

// Prepoznaje adrese srpskog zavoda
func (h Hidmet) Prepoznaje(adresa string) bool { return PostajaHidmetIzAdrese(adresa) > 0 }

// PostajaHidmetIzAdrese vraća broj postaje iz adrese; 0 kad adresa nije
// njihova ili nema broja
func PostajaHidmetIzAdrese(adresa string) int {
	if !strings.Contains(strings.ToLower(adresa), "hidmet.gov.rs") {
		return 0
	}
	m := reHidmetPostaja.FindStringSubmatch(adresa)
	if m == nil {
		return 0
	}
	id, _ := strconv.Atoi(m[1])
	return id
}

// AdresaHidmet je adresa stranice zadane postaje
func AdresaHidmet(hmID int) string {
	return AdresaHidmetPostaje + "?hm_id=" + strconv.Itoa(hmID) + "&period=7"
}

// Ocitanja čita satne vrijednosti s adrese, najstarije prvo
func (h Hidmet) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	if PostajaHidmetIzAdrese(adresa) <= 0 {
		return nil, fmt.Errorf("adresa nema broj postaje (hm_id=…)")
	}
	if h.Base != "" {
		adresa = h.Base + "?" + strings.SplitN(adresa, "?", 2)[1]
	}
	c := h.Client
	if c == nil {
		c = &Client{}
	}
	b, err := c.dohvati(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return CitajHidmet(string(b))
}

// CitajHidmet čita tablicu satnih vrijednosti. Vrijeme na stranici je
// UTC+1 bez ljetnog pomaka i tako se i pretvara.
func CitajHidmet(html string) ([]Redak, error) {
	var out []Redak
	for _, m := range reHidmetRedak.FindAllStringSubmatch(html, -1) {
		d, _ := strconv.Atoi(m[1])
		mj, _ := strconv.Atoi(m[2])
		g, _ := strconv.Atoi(m[3])
		h, _ := strconv.Atoi(m[4])
		min, _ := strconv.Atoi(m[5])
		cm, _ := strconv.Atoi(m[6])
		kad := time.Date(g, time.Month(mj), d, h, min, 0, 0, zonaHidmet)
		out = append(out, Redak{Kad: kad.UTC(), Cm: cm})
	}
	if len(out) == 0 {
		if strings.Contains(html, "Vodostaj") {
			return nil, fmt.Errorf("tablica je prazna — postaja možda ne javlja vodostaj")
		}
		return nil, fmt.Errorf("na stranici nema tablice očitanja — je li se stranica promijenila?")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}
