package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Geokoder nalazi koordinate po adresi ili nazivu mjesta preko
// OpenStreetMapa (Nominatim). Za pokoje traženje pri upisu registra, ne
// za masovnu upotrebu: jedan upit u sekundi i obvezno ime programa.
type Geokoder struct {
	HTTP    *http.Client
	BaseURL string // za testove
}

const defaultNominatim = "https://nominatim.openstreetmap.org/search"

// Tocka je nađeno mjesto
type Tocka struct {
	Lat, Lon float64
	Naziv    string
}

// Nadji vraća prvo mjesto u Hrvatskoj koje odgovara upitu
func (g *Geokoder) Nadji(ctx context.Context, upit string) (*Tocka, error) {
	upit = strings.TrimSpace(upit)
	if upit == "" {
		return nil, errors.New("upišite adresu ili mjesto")
	}
	base := g.BaseURL
	if base == "" {
		base = defaultNominatim
	}
	q := url.Values{"q": {upit}, "format": {"jsonv2"}, "limit": {"1"}, "countrycodes": {"hr"}, "accept-language": {"hr"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goCOP/0.0 (obrana od poplava; registar podrucja)")
	c := g.HTTP
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("traženje koordinata nije uspjelo (nema interneta?): %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenStreetMap je odgovorio %s", res.Status)
	}
	var out []struct {
		Lat, Lon    string
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("odgovor OpenStreetMapa nije čitljiv: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("za „%s” nije nađeno mjesto; pokušajte samo s gradom", upit)
	}
	lat, _ := strconv.ParseFloat(out[0].Lat, 64)
	lon, _ := strconv.ParseFloat(out[0].Lon, 64)
	return &Tocka{Lat: lat, Lon: lon, Naziv: out[0].DisplayName}, nil
}

// MjestoIzNaziva vadi grad iz "VGI Vuka, Osijek" ili "Podcentar Osijek":
// ono iza zadnjeg zareza, inače zadnja riječ
func MjestoIzNaziva(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ","); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	dijelovi := strings.Fields(s)
	if len(dijelovi) == 0 {
		return ""
	}
	return dijelovi[len(dijelovi)-1]
}
