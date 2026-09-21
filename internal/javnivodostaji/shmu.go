// Slovačke letve čita Slovenský hydrometeorologický ústav na shmu.sk.
// Stranica postaje ima tablicu izmjerenih vrijednosti zadnja 24 sata, svakih
// petnaest minuta:
//
//	Čas merania        Vodný stav [cm]   Teplota vody [°C]
//	21.9.2026 07:15    265               18,8
//
// Uzimaju se samo puni sati, da se letva vodi jednako kao ostale; međuvrijeme
// ionako nema tko čitati, a četverostruko više zapisa ne govori ništa više.
//
// Vrijeme je slovačko lokalno, a Slovačka drži isto vrijeme kao i mi.
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

// PodrijetloSHMU je ono što stoji uz očitanje kao izvor
const PodrijetloSHMU = "shmu.sk"

// AdresaSHMUPostaje je stranica vodomjernih postaja
const AdresaSHMUPostaje = "https://www.shmu.sk/sk/?page=765"

var (
	reSHMUPostaja = regexp.MustCompile(`(?i)station_id=(\d+)`)
	// redak tablice: vrijeme u jednoj ćeliji, vodostaj u sljedećoj
	reSHMURedak = regexp.MustCompile(
		`h_datum_cas"\s*>\s*(\d{1,2})\.(\d{1,2})\.(\d{4})\s+(\d{1,2}):(\d{2})\s*</td>\s*<td[^>]*h_vodny_stav"\s*>\s*(-?\d+)`)
)

// SHMU čita slovačku stranicu
type SHMU struct {
	Client *Client
	Base   string // prazno znači prava stranica; test podmeće svoju
}

// Naziv je shmu.sk
func (s SHMU) Naziv() string { return PodrijetloSHMU }

// Prepoznaje adrese slovačkog zavoda
func (s SHMU) Prepoznaje(adresa string) bool { return PostajaSHMUIzAdrese(adresa) > 0 }

// PostajaSHMUIzAdrese vraća broj postaje iz adrese; 0 kad adresa nije njihova
func PostajaSHMUIzAdrese(adresa string) int {
	if !strings.Contains(strings.ToLower(adresa), "shmu.sk") {
		return 0
	}
	m := reSHMUPostaja.FindStringSubmatch(adresa)
	if m == nil {
		return 0
	}
	id, _ := strconv.Atoi(m[1])
	return id
}

// AdresaSHMU je adresa stranice zadane postaje
func AdresaSHMU(postajaID int) string {
	return AdresaSHMUPostaje + "&station_id=" + strconv.Itoa(postajaID)
}

// Ocitanja čita vrijednosti s adrese, najstarije prvo
func (s SHMU) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	id := PostajaSHMUIzAdrese(adresa)
	if id <= 0 {
		return nil, fmt.Errorf("adresa nema broj postaje (station_id=…)")
	}
	if s.Base != "" {
		adresa = s.Base + "?page=765&station_id=" + strconv.Itoa(id)
	}
	c := s.Client
	if c == nil {
		c = &Client{}
	}
	b, err := c.dohvati(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return CitajSHMU(string(b))
}

// CitajSHMU čita tablicu izmjerenih vrijednosti; zadržava pune sate.
func CitajSHMU(html string) ([]Redak, error) {
	var out []Redak
	nadjeno := 0
	for _, m := range reSHMURedak.FindAllStringSubmatch(html, -1) {
		nadjeno++
		min, _ := strconv.Atoi(m[5])
		if min != 0 {
			continue
		}
		d, _ := strconv.Atoi(m[1])
		mj, _ := strconv.Atoi(m[2])
		g, _ := strconv.Atoi(m[3])
		h, _ := strconv.Atoi(m[4])
		cm, _ := strconv.Atoi(m[6])
		kad := time.Date(g, time.Month(mj), d, h, 0, 0, 0, models.Zagreb)
		out = append(out, Redak{Kad: kad.UTC(), Cm: cm})
	}
	if len(out) == 0 {
		if nadjeno > 0 {
			return nil, fmt.Errorf("tablica ima %d redaka, ali nijedan na punom satu", nadjeno)
		}
		return nil, fmt.Errorf("na stranici nema tablice vodostaja — je li se stranica promijenila?")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}
