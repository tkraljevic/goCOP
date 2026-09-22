// Protok s javne stranice Hrvatskih voda. Vodostaj i protok ondje stoje na
// dvije različite stranice: vodostaj na onoj koju čita Ocitanja, a protok na
// mobilnoj, pod „Pregled protoka postaje". Nema ga svaka letva — objavljuje
// se ondje gdje postoji krivulja koju služba održava.
//
// Zato se protok dohvaća zasebno i pridružuje vodostaju po trenutku: letva
// koja ga nema i dalje radi kao i dosad, a letva koja ga ima dobiva ga uz
// vodostaj, bez ijednog dodatnog upisa u kartici.
package javnivodostaji

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// AdresaProtokaPostaje je stranica protoka jedne postaje
const AdresaProtokaPostaje = "https://mvodostaji.voda.hr/Home/PregledProtokaPostaje"

// reProtokRedak prepoznaje redak tablice protoka: datum s dvoznamenkastom
// godinom, sat i vrijednost sa zarezom. Trend se ne čita, izvodi se iz
// vrijednosti.
var reProtokRedak = regexp.MustCompile(
	`(\d{2})\.(\d{2})\.(\d{2})\.?\s+(\d{1,2}):(\d{2})\s+(-?\d+(?:,\d+)?)`)

// AdresaProtoka je adresa stranice protoka za zadanu postaju
func AdresaProtoka(p Postaja) string {
	sektor := p.Sektor
	if sektor <= 0 {
		sektor = 1
	}
	return AdresaProtokaPostaje + "?sektorID=" + strconv.Itoa(sektor) +
		"&bpID=0&postajaID=" + strconv.Itoa(p.ID)
}

// Protoci čita protok postaje s javne stranice, najstarije prvo. Letva koja
// protok ne objavljuje vraća prazno bez greške — to nije kvar nego stanje.
func (c *Client) Protoci(ctx context.Context, p Postaja) ([]Redak, error) {
	adresa := AdresaProtoka(p)
	if c.Base != "" {
		adresa = c.Base + "/Home/PregledProtokaPostaje?sektorID=" +
			strconv.Itoa(max(p.Sektor, 1)) + "&bpID=0&postajaID=" + strconv.Itoa(p.ID)
	}
	b, err := c.dohvati(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return CitajProtokHV(string(b))
}

// CitajProtokHV razlaže tablicu protoka. Vrijeme je lokalno, kao i na letvi.
func CitajProtokHV(html string) ([]Redak, error) {
	if strings.Contains(html, "an error occurred") {
		return nil, fmt.Errorf("stranica je javila grešku umjesto podataka")
	}
	// Oznake se prvo maknu: vrijednosti su razasute po ćelijama, a razmak
	// među njima nije uvijek isti.
	tekst := reOznakeHV.ReplaceAllString(html, " ")
	tekst = strings.Join(strings.Fields(tekst), " ")

	var out []Redak
	for _, m := range reProtokRedak.FindAllStringSubmatch(tekst, -1) {
		d, _ := strconv.Atoi(m[1])
		mj, _ := strconv.Atoi(m[2])
		g, _ := strconv.Atoi(m[3])
		h, _ := strconv.Atoi(m[4])
		min, _ := strconv.Atoi(m[5])
		q, err := strconv.ParseFloat(strings.ReplaceAll(m[6], ",", "."), 64)
		if err != nil {
			continue
		}
		kad := time.Date(2000+g, time.Month(mj), d, h, min, 0, 0, models.Zagreb)
		out = append(out, Redak{Kad: kad.UTC(), FlowM3s: floatPtr(q)})
	}
	return out, nil
}

var reOznakeHV = regexp.MustCompile(`<[^>]+>`)

// reSektorAdrese čita sektor obrane iz adrese postaje
var reSektorAdrese = regexp.MustCompile(`(?i)sektorID=(\d+)`)

// SektorIzAdrese vraća sektor iz adrese javne stranice; 0 kad ga nema
func SektorIzAdrese(adresa string) int {
	m := reSektorAdrese.FindStringSubmatch(adresa)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// dopuniProtokom pridružuje protok vodostajima po trenutku. Trenutak koji
// vodostaj nema ne dodaje se: protok bez vodostaja nije očitanje letve nego
// pola podatka.
func dopuniProtokom(vodostaji, protoci []Redak) []Redak {
	if len(protoci) == 0 {
		return vodostaji
	}
	po := make(map[int64]float64, len(protoci))
	for _, p := range protoci {
		if p.FlowM3s != nil {
			po[p.Kad.Unix()] = *p.FlowM3s
		}
	}
	for i := range vodostaji {
		if q, ima := po[vodostaji[i].Kad.Unix()]; ima {
			vodostaji[i].FlowM3s = floatPtr(q)
		}
	}
	return vodostaji
}
