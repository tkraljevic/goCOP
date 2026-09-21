// PEGELONLINE daje službeni REST API s vodostajima u 15-minutnom koraku.
// goCOP uzima stvarno objavljena očitanja na punom satu, jednako kao pri
// gradnji historijskog niza; četvrtine se ne prosječe.
package javnivodostaji

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	PodrijetloPegelOnline = "pegelonline.wsv.de"
	PegelOnlineBase       = "https://www.pegelonline.wsv.de/webservices/rest-api/v2"
)

var rePegelOnline = regexp.MustCompile(`(?i)/stations/([0-9a-f-]{36})/W/measurements\.json$`)

type PegelOnline struct {
	Client *Client
	Base   string // zamjenska adresa u testu
}

func (p PegelOnline) Naziv() string { return PodrijetloPegelOnline }

func (p PegelOnline) Prepoznaje(adresa string) bool { return PostajaPegelOnlineIzAdrese(adresa) != "" }

func AdresaPegelOnline(uuid string) string {
	return PegelOnlineBase + "/stations/" + strings.TrimSpace(uuid) + "/W/measurements.json?start=P2D"
}

func PostajaPegelOnlineIzAdrese(adresa string) string {
	u, err := url.Parse(strings.TrimSpace(adresa))
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), "pegelonline.wsv.de") {
		return ""
	}
	m := rePegelOnline.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

func (p PegelOnline) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	if PostajaPegelOnlineIzAdrese(adresa) == "" {
		return nil, fmt.Errorf("PEGELONLINE adresa nema UUID postaje")
	}
	if p.Base != "" {
		adresa = p.Base
	}
	c := p.Client
	if c == nil {
		c = &Client{}
	}
	b, err := c.dohvati(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return CitajPegelOnline(b)
}

type pegelOnlineMjerenje struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

func CitajPegelOnline(b []byte) ([]Redak, error) {
	var mjerenja []pegelOnlineMjerenje
	if err := json.Unmarshal(b, &mjerenja); err != nil {
		return nil, fmt.Errorf("PEGELONLINE odgovor nije valjan JSON: %w", err)
	}
	poVremenu := make(map[int64]Redak)
	for _, m := range mjerenja {
		kad, err := time.Parse(time.RFC3339, m.Timestamp)
		if err != nil || kad.Minute() != 0 || kad.Second() != 0 || kad.Nanosecond() != 0 ||
			math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
			continue
		}
		poVremenu[kad.UTC().Unix()] = Redak{Kad: kad.UTC(), LevelCm: intPtr(int(math.Round(m.Value)))}
	}
	if len(poVremenu) == 0 {
		return nil, fmt.Errorf("PEGELONLINE nije vratio nijedno valjano očitanje na punom satu")
	}
	out := make([]Redak, 0, len(poVremenu))
	for _, r := range poVremenu {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}
