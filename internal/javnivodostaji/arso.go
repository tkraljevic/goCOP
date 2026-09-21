// Slovenske postaje čita službeni ARSO XML. Dokument sadrži trenutačno
// očitanje svih postaja, a fragment adrese (#2110) govori koju postaju
// goCOP treba izdvojiti. Fragment se ne šalje poslužitelju.
package javnivodostaji

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

const (
	PodrijetloARSO = "arso.gov.si"
	AdresaARSOXML  = "https://www.arso.gov.si/xml/vode/hidro_podatki_zadnji.xml"
)

// ARSO čita službeni XML trenutačnih hidroloških podataka.
type ARSO struct {
	Client *Client
	Base   string // zamjenska adresa u testu
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
	dohvati := strings.SplitN(adresa, "#", 2)[0]
	if a.Base != "" {
		dohvati = a.Base
	}
	c := a.Client
	if c == nil {
		c = &Client{}
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
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}
