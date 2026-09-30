// DanubeHIS (ICPDR, danubehis.org) objavljuje operativne satne vodostaje
// postaja cijelog sliva, među njima i mađarskih, koje OVF šalje izravno.
// Stranica postaje bez prijave pokazuje zadnjih 25 satnih vrijednosti:
//
//	<td class="views-field views-field-time">2026-09-30 08:00</td>
//	<td class="views-field views-field-value …">-37</td>
//	<td class="views-field views-field-unit">cm</td>
//	<td class="views-field views-field-description">Current water level</td>
//
// Vrijeme je u zoni Europe/Vienna, istoj kao naša.
//
// Mađarskim letvama služi kao rezerva: vizugy.hu zna satima stati za sve
// postaje odjednom (29. i 30. 9. 2026. od 18 h, pa od 2 h), a DanubeHIS iste
// brojke ima sat-dva iza mjerenja. Podaci su sirovi (licenca CC BY-NC-SA 4.0,
// u NOTICE), isti kao na vizugy.hu.
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

// PodrijetloDanubeHIS je ono što stoji uz očitanje kao izvor
const PodrijetloDanubeHIS = "danubehis.org"

// AdresaDanubeHISPostaje je stranica rezultata postaje; iza nje ide oznaka
const AdresaDanubeHISPostaje = "https://www.danubehis.org/results/"

var (
	reDanubeHISPostaja = regexp.MustCompile(`(?i)danubehis\.org/results/([A-Z]{2}[0-9A-Z]+_HYDRO)`)
	reDanubeHISRedak   = regexp.MustCompile(
		`(?s)views-field-time"\s*>\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2})\s*</td>\s*<td[^>]*views-field-value[^>]*>\s*(-?\d+(?:\.\d+)?)\s*</td>\s*<td[^>]*views-field-unit"\s*>\s*cm\s*</td>\s*<td[^>]*views-field-description"\s*>\s*Current water level`)
)

// RezervaDanubeHIS su mađarske letve i njihove oznake na DanubeHIS-u. Kad
// vizugy.hu kasni, sati koji letvi nedostaju uzimaju se odande.
var RezervaDanubeHIS = map[string]string{
	"nagybajcs":     "HU442502_HYDRO",
	"komarom":       "HU442522_HYDRO",
	"esztergom":     "HU442025_HYDRO",
	"budapest":      "HU442027_HYDRO",
	"dunafoldvar":   "HU442029_HYDRO",
	"paks":          "HU442030_HYDRO",
	"baja":          "HU442031_HYDRO",
	"mohacs":        "HU442032_HYDRO",
	"letenye":       "HU446501_HYDRO",
	"dravaszabolcs": "HU446503_HYDRO",
}

// RezervaNakon je koliko zadnje očitanje s vizugy.hu smije biti staro prije
// nego se posegne za DanubeHIS-om; vizugy obično kasni sat-dva.
const RezervaNakon = 2 * time.Hour

// DanubeHIS čita stranicu postaje na danubehis.org
type DanubeHIS struct {
	Client *Client
	Base   string // prazno znači prava stranica; test podmeće svoju
}

// Naziv je danubehis.org
func (d DanubeHIS) Naziv() string { return PodrijetloDanubeHIS }

// Prepoznaje adrese stranica postaja na DanubeHIS-u
func (d DanubeHIS) Prepoznaje(adresa string) bool { return PostajaDanubeHISIzAdrese(adresa) != "" }

// PostajaDanubeHISIzAdrese vraća oznaku postaje (npr. HU442522_HYDRO)
func PostajaDanubeHISIzAdrese(adresa string) string {
	m := reDanubeHISPostaja.FindStringSubmatch(adresa)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1])
}

// AdresaDanubeHIS je stranica satnih vodostaja zadane postaje
func AdresaDanubeHIS(oznaka string) string {
	return AdresaDanubeHISPostaje + strings.ToUpper(oznaka) + "/h"
}

// Ocitanja čita zadnje satne vodostaje s adrese, najstarije prvo
func (d DanubeHIS) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	oznaka := PostajaDanubeHISIzAdrese(adresa)
	if oznaka == "" {
		return nil, fmt.Errorf("adresa nema oznaku postaje (…/results/XX…_HYDRO)")
	}
	if d.Base != "" {
		adresa = d.Base + "/results/" + oznaka + "/h"
	}
	c := d.Client
	if c == nil {
		c = &Client{}
	}
	b, err := c.dohvati(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return CitajDanubeHIS(string(b))
}

// CitajDanubeHIS čita tablicu rezultata; zadržava samo trenutne vodostaje.
func CitajDanubeHIS(html string) ([]Redak, error) {
	var out []Redak
	for _, m := range reDanubeHISRedak.FindAllStringSubmatch(html, -1) {
		kad, err := time.ParseInLocation("2006-01-02 15:04", m[1], models.Zagreb)
		if err != nil || kad.Minute() != 0 {
			continue
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		out = append(out, Redak{Kad: kad.UTC(), LevelCm: intPtr(int(v))})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("na stranici nema tablice vodostaja — je li se stranica promijenila?")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}
