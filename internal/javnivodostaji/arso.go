// Slovenske postaje čita ARSO. Adresa u registru je službeni XML s
// fragmentom šifre postaje (#2110), ali XML nosi samo zadnje očitanje, a
// ARSO ga osvježava svakih deset minuta: pita li se jednom na sat, satima
// ostanu rupe. Zato se prvo čita tablica postaje za zadnji dan (vrijednost
// svakih 10 minuta), a XML ostaje rezerva kad tablice nema. Fragment se ne
// šalje poslužitelju.
package javnivodostaji

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

const (
	PodrijetloARSO = "arso.gov.si"
	AdresaARSOXML  = "https://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml"
	// AdresaARSOTablice je tablica jedne postaje za zadnji dan; %s je šifra.
	AdresaARSOTablice = "https://www.arso.gov.si/vode/podatki/amp/H%s_t_1.html"
)

// ARSO čita službeni XML trenutačnih hidroloških podataka.
type ARSO struct {
	Client      *Client
	Base        string // zamjenska adresa XML-a u testu
	BaseTablice string // zamjenska adresa tablice u testu; %s je šifra
}

func (a ARSO) Naziv() string { return PodrijetloARSO }

func (a ARSO) Prepoznaje(adresa string) bool { return PostajaARSOIzAdrese(adresa) != "" }

// AdresaARSO vraća adresu javnog izvora za jednu šifru postaje.
func AdresaARSO(sifra string) string { return AdresaARSOXML + "#" + strings.TrimSpace(sifra) }

// PostajaARSOIzAdrese čita šifru iz fragmenta ili parametra postaja.
func PostajaARSOIzAdrese(adresa string) string {
	u, err := url.Parse(strings.TrimSpace(adresa))
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), "arso.gov.si") ||
		!strings.HasSuffix(strings.ToLower(u.Path), "/hidro_podatki_zadnji.xml") {
		return ""
	}
	sifra := strings.TrimSpace(u.Fragment)
	if sifra == "" {
		sifra = strings.TrimSpace(u.Query().Get("postaja"))
	}
	if _, err := strconv.Atoi(sifra); err != nil {
		return ""
	}
	return sifra
}

func (a ARSO) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	sifra := PostajaARSOIzAdrese(adresa)
	if sifra == "" {
		return nil, fmt.Errorf("ARSO adresa nema šifru postaje (#…)")
	}
	c := a.Client
	if c == nil {
		c = &Client{}
	}
	tablica := AdresaARSOTablice
	if a.BaseTablice != "" {
		tablica = a.BaseTablice
	}
	if b, err := c.dohvati(ctx, fmt.Sprintf(tablica, sifra)); err == nil {
		if redci, err := CitajARSOTablicu(b); err == nil && len(redci) > 0 {
			return redci, nil
		}
	}
	dohvati := strings.SplitN(adresa, "#", 2)[0]
	if a.Base != "" {
		dohvati = a.Base
	}
	b, err := c.dohvati(ctx, dohvati)
	if err != nil {
		return nil, err
	}
	return CitajARSO(b, sifra)
}

type arsoDokument struct {
	Postaje []arsoPostaja `xml:"postaja"`
}

type arsoPostaja struct {
	Sifra    string `xml:"sifra,attr"`
	Datum    string `xml:"datum"`
	Vodostaj string `xml:"vodostaj"`
	Protok   string `xml:"pretok"`
	TempVode string `xml:"temp_vode"`
}

// CitajARSO izdvaja trenutačno očitanje jedne postaje. Prazna mjerena
// veličina nije nula: ostaje prazna, a ako su sve prazne vraća se prazan niz.
func CitajARSO(b []byte, sifra string) ([]Redak, error) {
	var dokument arsoDokument
	if err := xml.Unmarshal(b, &dokument); err != nil {
		return nil, fmt.Errorf("ARSO odgovor nije valjan XML: %w", err)
	}
	for _, p := range dokument.Postaje {
		if p.Sifra != sifra {
			continue
		}
		kad, err := vrijemeARSO(p.Datum)
		if err != nil {
			return nil, fmt.Errorf("ARSO postaja %s ima nepoznato vrijeme %q", sifra, p.Datum)
		}
		redak := Redak{Kad: kad.UTC()}
		if v, ok := arsoBroj(p.Vodostaj); ok {
			cm := int(v)
			redak.LevelCm = &cm
		}
		if v, ok := arsoBroj(p.TempVode); ok {
			redak.TempC = floatPtr(v)
		}
		if v, ok := arsoBroj(p.Protok); ok {
			redak.FlowM3s = floatPtr(v)
		}
		if redak.LevelCm == nil && redak.TempC == nil && redak.FlowM3s == nil {
			return nil, nil
		}
		return []Redak{redak}, nil
	}
	return nil, fmt.Errorf("ARSO XML nema postaju %s", sifra)
}

func vrijemeARSO(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, oblik := range []string{"2006-01-02 15:04", "02.01.2006 15:04"} {
		if kad, err := time.ParseInLocation(oblik, s, models.Zagreb); err == nil {
			return kad, nil
		}
	}
	return time.Time{}, fmt.Errorf("nepoznato vrijeme %q", s)
}

func arsoBroj(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	if s == "" || s == "-" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

var (
	reARSOZaglavlje = regexp.MustCompile(`(?s)<thead>(.*?)</thead>`)
	reARSOTh        = regexp.MustCompile(`(?s)<th[^>]*>(.*?)</th>`)
	reARSOTijelo    = regexp.MustCompile(`(?s)<tbody>(.*?)</tbody>`)
	reARSORedak     = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	reARSOTd        = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
)

// CitajARSOTablicu čita tablicu postaje: prvi stupac je lokalno vrijeme, a
// ostali se prepoznaju po zaglavlju (Vodostaj, Pretok, Temperatura vode),
// jer ih nema svaka postaja. „-” je rupa, ne nula; redak bez ijedne
// vrijednosti se preskače. Redci su od najnovijeg prema starijem.
func CitajARSOTablicu(b []byte) ([]Redak, error) {
	s := string(b)
	z := reARSOZaglavlje.FindStringSubmatch(s)
	t := reARSOTijelo.FindStringSubmatch(s)
	if z == nil || t == nil {
		return nil, fmt.Errorf("ARSO tablica nema zaglavlje ili tijelo")
	}
	stupac := map[string]int{}
	for i, th := range reARSOTh.FindAllStringSubmatch(z[1], -1) {
		naziv := strings.ToLower(html.UnescapeString(strings.TrimSpace(th[1])))
		switch {
		case strings.HasPrefix(naziv, "vodostaj"):
			stupac["h"] = i
		case strings.HasPrefix(naziv, "pretok"):
			stupac["q"] = i
		case strings.HasPrefix(naziv, "temperatura vode"):
			stupac["t"] = i
		}
	}
	if len(stupac) == 0 {
		return nil, fmt.Errorf("ARSO tablica nema poznatih stupaca")
	}
	var out []Redak
	for _, tr := range reARSORedak.FindAllStringSubmatch(t[1], -1) {
		td := reARSOTd.FindAllStringSubmatch(tr[1], -1)
		if len(td) == 0 {
			continue
		}
		celija := func(k string) (float64, bool) {
			i, ima := stupac[k]
			if !ima || i >= len(td) {
				return 0, false
			}
			return arsoBroj(html.UnescapeString(td[i][1]))
		}
		kad, err := vrijemeARSO(html.UnescapeString(td[0][1]))
		if err != nil {
			continue
		}
		r := Redak{Kad: kad.UTC()}
		if v, ok := celija("h"); ok {
			cm := int(math.Round(v))
			r.LevelCm = &cm
		}
		if v, ok := celija("q"); ok {
			r.FlowM3s = floatPtr(v)
		}
		if v, ok := celija("t"); ok {
			r.TempC = floatPtr(v)
		}
		if r.LevelCm == nil && r.FlowM3s == nil && r.TempC == nil {
			continue
		}
		out = append(out, r)
	}
	// Uvoz očekuje redoslijed od najstarijeg prema najnovijem.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
