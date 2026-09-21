// eHYD objavljuje trenutačne hidrološke podatke kao službeni OGC API.
// Za Lavamünd je javno dostupan trenutačni protok. Uvoznik ga dohvaća jednom
// na sat, pa ne gomila svaku 15-minutnu vrijednost koju državni servis prima.
package javnivodostaji

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const PodrijetloEHYD = "ehyd.gv.at"

type EHYD struct {
	Client *Client
	Base   string // zamjenska adresa u testu
}

func (e EHYD) Naziv() string { return PodrijetloEHYD }

func (e EHYD) Prepoznaje(adresa string) bool {
	u, err := url.Parse(strings.TrimSpace(adresa))
	return err == nil && strings.EqualFold(u.Hostname(), "gis.lfrz.gv.at") &&
		strings.Contains(u.Path, "/collections/i000501:pegel_aktuell/items/")
}

func (e EHYD) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	if !e.Prepoznaje(adresa) {
		return nil, fmt.Errorf("adresa nije službeni eHYD zapis aktualne postaje")
	}
	if e.Base != "" {
		adresa = e.Base
	}
	c := e.Client
	if c == nil {
		c = &Client{}
	}
	b, err := c.dohvati(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return CitajEHYD(b)
}

type ehydDokument struct {
	Features []struct {
		Properties struct {
			Vrijednost float64 `json:"wert"`
			Jedinica   string  `json:"einheit"`
			Vrijeme    string  `json:"zeitpunkt"`
			Parametar  string  `json:"parameter"`
		} `json:"properties"`
	} `json:"features"`
}

func CitajEHYD(b []byte) ([]Redak, error) {
	var dokument ehydDokument
	if err := json.Unmarshal(b, &dokument); err != nil {
		return nil, fmt.Errorf("eHYD odgovor nije valjan JSON: %w", err)
	}
	if len(dokument.Features) == 0 {
		return nil, fmt.Errorf("eHYD nije vratio postaju")
	}
	p := dokument.Features[0].Properties
	kad, err := time.Parse(time.RFC3339, p.Vrijeme)
	if err != nil {
		return nil, fmt.Errorf("eHYD ima nepoznato vrijeme %q", p.Vrijeme)
	}
	r := Redak{Kad: kad.UTC()}
	parametar := strings.ToUpper(strings.TrimSpace(p.Parametar))
	jedinica := strings.ToLower(strings.TrimSpace(p.Jedinica))
	switch {
	case parametar == "Q" && strings.Contains(jedinica, "m³/s"):
		r.FlowM3s = floatPtr(p.Vrijednost)
	case (parametar == "W" || parametar == "H") && jedinica == "cm":
		r.LevelCm = intPtr(int(p.Vrijednost))
	case (parametar == "T" || parametar == "TW") && strings.Contains(jedinica, "°c"):
		r.TempC = floatPtr(p.Vrijednost)
	default:
		return nil, fmt.Errorf("eHYD parametar %q u jedinici %q još nije podržan", p.Parametar, p.Jedinica)
	}
	return []Redak{r}, nil
}
