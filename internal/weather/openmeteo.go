// Package weather dohvaća vremenske prilike za list dnevnika s Open-Meteo
// (open-meteo.com), javne usluge bez ključa. Radi samo dok ima interneta;
// bez njega list se popunjava ručno, kao i dosad.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Day su prilike jednog dana na jednom mjestu, sažete kako ih list traži
type Day struct {
	Temperature   float64 // °C u podne (ili u zadani sat)
	WindFrom      float64 // m/s, najmanja brzina kroz radni dan
	WindTo        float64 // m/s, najveća
	Pressure      float64 // hPa u podne
	Precipitation float64 // mm, zbroj dana
	Description   string  // "sunčano, toplo", "oblačno, kiša"...
	Source        string
}

// Client dohvaća podatke; prazan je spreman za rad
type Client struct {
	HTTP    *http.Client
	BaseURL string // za testove
	Archive string
}

const (
	defaultBase    = "https://api.open-meteo.com/v1/forecast"
	defaultArchive = "https://archive-api.open-meteo.com/v1/archive"
)

type response struct {
	Hourly struct {
		Time        []string  `json:"time"`
		Temperature []float64 `json:"temperature_2m"`
		Wind        []float64 `json:"wind_speed_10m"`
		Gusts       []float64 `json:"wind_gusts_10m"`
		Pressure    []float64 `json:"surface_pressure"`
		Precip      []float64 `json:"precipitation"`
		Code        []int     `json:"weather_code"`
	} `json:"hourly"`
	Reason string `json:"reason"`
}

// Fetch vraća prilike za dan; hour je sat za temperaturu i tlak (podne kad je 0)
func (c *Client) Fetch(ctx context.Context, lat, lon float64, day time.Time, hour int) (*Day, error) {
	if hour <= 0 {
		hour = 12
	}
	base := c.BaseURL
	if base == "" {
		base = defaultBase
	}
	// Prognoza drži zadnjih ~90 dana; starije nosi arhiva
	if time.Since(day) > 85*24*time.Hour {
		base = c.Archive
		if base == "" {
			base = defaultArchive
		}
	}
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%.4f", lat))
	q.Set("longitude", fmt.Sprintf("%.4f", lon))
	q.Set("hourly", "temperature_2m,wind_speed_10m,wind_gusts_10m,surface_pressure,precipitation,weather_code")
	q.Set("start_date", day.Format("2006-01-02"))
	q.Set("end_date", day.Format("2006-01-02"))
	q.Set("timezone", "Europe/Zagreb")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gocop/0.1 (dnevnik)")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 12 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vremenska usluga nije dostupna: %w", err)
	}
	defer resp.Body.Close()
	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("vremenska usluga: neočekivan odgovor: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vremenska usluga: %s", strings.TrimSpace(r.Reason))
	}
	return summarize(r, hour)
}

func summarize(r response, hour int) (*Day, error) {
	h := r.Hourly
	if len(h.Time) == 0 || len(h.Temperature) != len(h.Time) {
		return nil, fmt.Errorf("vremenska usluga nema podataka za taj dan")
	}
	at := func(vals []float64, i int) float64 {
		if i < len(vals) {
			return vals[i]
		}
		return math.NaN()
	}
	d := &Day{Source: "OPEN_METEO", WindFrom: math.Inf(1), WindTo: math.Inf(-1)}
	idx := -1
	codes := map[int]int{}
	for i, ts := range h.Time {
		var hh int
		if _, err := fmt.Sscanf(ts[len(ts)-5:], "%d:", &hh); err != nil {
			continue
		}
		if hh == hour {
			idx = i
		}
		if p := at(h.Precip, i); !math.IsNaN(p) {
			d.Precipitation += p
		}
		// radni dan: vjetar i vrijeme od 6 do 18
		if hh >= 6 && hh <= 18 {
			if w := at(h.Wind, i); !math.IsNaN(w) {
				w /= 3.6
				d.WindFrom = math.Min(d.WindFrom, w)
				d.WindTo = math.Max(d.WindTo, w)
			}
			if g := at(h.Gusts, i); !math.IsNaN(g) {
				d.WindTo = math.Max(d.WindTo, g/3.6)
			}
			if i < len(h.Code) {
				codes[h.Code[i]]++
			}
		}
	}
	if idx < 0 {
		idx = len(h.Time) / 2
	}
	d.Temperature = at(h.Temperature, idx)
	d.Pressure = at(h.Pressure, idx)
	if math.IsInf(d.WindFrom, 1) {
		d.WindFrom, d.WindTo = 0, 0
	}
	d.WindFrom = math.Round(d.WindFrom)
	d.WindTo = math.Round(d.WindTo)
	d.Precipitation = math.Round(d.Precipitation*10) / 10
	d.Description = describe(codes, d.Temperature, d.WindTo)
	return d, nil
}

// describe svodi WMO oznake vremena kroz dan na riječi obrasca
func describe(codes map[int]int, temp, wind float64) string {
	// WMO oznake svedene na riječi obrasca, po redu od vedrog prema lošem
	kinds := []string{"sunčano", "oblačno", "magla", "kiša", "snijeg", "grmljavina"}
	kindOf := func(c int) int {
		switch {
		case c <= 1:
			return 0
		case c <= 3:
			return 1
		case c == 45 || c == 48:
			return 2
		case (c >= 51 && c <= 67) || (c >= 80 && c <= 82):
			return 3
		case (c >= 71 && c <= 77) || c == 85 || c == 86:
			return 4
		case c >= 95:
			return 5
		}
		return 1
	}
	var hours [6]int
	for c, k := range codes {
		hours[kindOf(c)] += k
	}
	var words []string
	best, n := -1, 0
	for i, k := range hours {
		if k > n { // pri izjednačenju ostaje vedrija riječ, loša se doda ispod
			best, n = i, k
		}
	}
	if best >= 0 {
		words = append(words, kinds[best])
	}
	// kiša ili snijeg u bilo kojem satu dana vrijede spomenuti i kad nisu prevladali
	for i := 3; i <= 5; i++ {
		if hours[i] > 0 && i != best {
			words = append(words, kinds[i])
		}
	}
	switch {
	case math.IsNaN(temp):
	case temp >= 25:
		words = append(words, "toplo")
	case temp <= 5:
		words = append(words, "hladno")
	}
	if wind >= 8 {
		words = append(words, "vjetrovito")
	}
	return strings.Join(words, ", ")
}
