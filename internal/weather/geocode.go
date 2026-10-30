package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if err := json.NewDecoder(io.LimitReader(res.Body, najveciOdgovor)).Decode(&out); err != nil {
		return nil, fmt.Errorf("odgovor OpenStreetMapa nije čitljiv: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("za „%s” nije nađeno mjesto; pokušajte samo s gradom", upit)
	}
	lat, lon, ok := koordinate(out[0].Lat, out[0].Lon)
	if !ok {
		return nil, fmt.Errorf("OpenStreetMap je za „%s” vratio neispravne koordinate; pokušajte ponovno", upit)
	}
	return &Tocka{Lat: lat, Lon: lon, Naziv: out[0].DisplayName}, nil
}

// koordinate čita širinu i dužinu; ok je netočno kad nisu brojevi ili su
// izvan ±90° i ±180°
func koordinate(sirina, duzina string) (lat, lon float64, ok bool) {
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(sirina), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(duzina), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	// NaN ne prolazi nijednu usporedbu, a beskonačno je izvan raspona
	if !(lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180) {
		return 0, 0, false
	}
	return lat, lon, true
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
