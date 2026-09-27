package kisomjeri

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gocop/internal/mletva"
	"gocop/internal/posta"
)

// Letva čita oborinu DHMZ-ovih postaja sa stranice Meteorologija na
// letva.voda.hr. Stranica traži prijavu sustava Windows (NTLM) računom domene
// Hrvatskih voda: isto korisničko ime kao za e-poštu, bez "@voda.hr" — to je
// račun čvora upisan za mletva.voda.hr.
//
// Sat u tablici je lokalni i znači kraj razdoblja; satne postaje daju svaki
// sat, glavne postaje s ručnim mjerenjem samo 12-satne zbrojeve u 06 i 18.
// Prazna ćelija je rupa. Od 7. 7. 2025. sat 23 dolazi prazan na svim
// automatskim postajama.
type Letva struct {
	Adresa string       // prazno znači https://letva.voda.hr
	HTTP   *http.Client // prazno znači klijent s provjerom certifikata bez datuma isteka
	Racun  func() (korisnik, lozinka string, ok bool)
	Dana   int // koliko dana unatrag se traži; zadano 2
	Sada   func() time.Time
}

const putOborine = "/2013Vodostaji/MeteoroloskiPodaci/KolicinaOborina"

func (l *Letva) klijent() *http.Client {
	if l.HTTP != nil {
		return l.HTTP
	}
	// Jedna veza po poslužitelju: NTLM prijava vrijedi po vezi.
	l.HTTP = &http.Client{Timeout: 90 * time.Second,
		Transport: &http.Transport{TLSClientConfig: mletva.TLSBezIsteka(), MaxConnsPerHost: 1}}
	return l.HTTP
}

// Preuzmi dohvaća zadnje dane za svaku postaju zasebnim zahtjevom: stranica
// imenuje stupce po imenu postaje, pa je jedan stupac po zahtjevu jedini
// pouzdan način da se zna čiji je koji.
func (l *Letva) Preuzmi(ctx context.Context, postaje []Postaja) ([]Mjerenje, error) {
	if l.Racun == nil {
		return nil, fmt.Errorf("letva.voda.hr: nema računa")
	}
	korisnik, lozinka, ok := l.Racun()
	if !ok {
		return nil, fmt.Errorf("letva.voda.hr: račun čvora za Hrvatske vode nije upisan")
	}
	sada := time.Now()
	if l.Sada != nil {
		sada = l.Sada()
	}
	dana := l.Dana
	if dana <= 0 {
		dana = 2
	}
	adresa := strings.TrimRight(l.Adresa, "/")
	if adresa == "" {
		adresa = "https://letva.voda.hr"
	}
	var out []Mjerenje
	for _, p := range postaje {
		forma := url.Values{
			"postaje":    {p.IzvorSifra},
			"datumOd":    {sada.In(Zagreb).AddDate(0, 0, -dana).Format("02.01.2006")},
			"datumDo":    {sada.In(Zagreb).Format("02.01.2006")},
			"grupiranje": {"0"},
		}
		res, err := posta.NTLMPost(ctx, l.klijent(), adresa+putOborine,
			"application/x-www-form-urlencoded; charset=UTF-8", []byte(forma.Encode()),
			posta.Racun{Korisnik: korisnik, Lozinka: lozinka})
		if err != nil {
			return out, fmt.Errorf("letva.voda.hr: %w", err)
		}
		tijelo, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if res.StatusCode == http.StatusUnauthorized {
			return out, fmt.Errorf("letva.voda.hr: prijava računom čvora nije prošla")
		}
		if res.StatusCode != http.StatusOK {
			return out, fmt.Errorf("letva.voda.hr: %s za postaju %s", res.Status, p.Naziv)
		}
		sati := 1
		switch p.Korak {
		case "12-satni":
			sati = 12
		case "dnevni":
			sati = 24
		}
		for _, r := range CitajTablicu(string(tijelo)) {
			out = append(out, Mjerenje{Kisomjer: p.Code, Kraj: r.Kraj.UTC(), Sati: sati, Oborina: r.Oborina,
				Izvor: "kisomjer-dhmz"})
		}
	}
	return out, nil
}

// RedakTablice je jedan redak tablice oborine s vrijednošću.
type RedakTablice struct {
	Kraj    time.Time // lokalno
	Oborina float64
}

var (
	reRedak = regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)
	reCelja = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	reSat   = regexp.MustCompile(`^(\d\d)\.(\d\d)\.(\d{4})\. (\d\d)$`)
)

// CitajTablicu čita tablicu s jednom postajom: redak po satu, prva ćelija
// "dd.mm.gggg. HH", druga oborina, treća zbroj od početka razdoblja.
// Prazni redci (rupe) se preskaču.
func CitajTablicu(s string) []RedakTablice {
	var out []RedakTablice
	for _, r := range reRedak.FindAllStringSubmatch(s, -1) {
		c := reCelja.FindAllStringSubmatch(r[1], -1)
		if len(c) < 2 {
			continue
		}
		m := reSat.FindStringSubmatch(strings.TrimSpace(html.UnescapeString(c[0][1])))
		if m == nil {
			continue
		}
		v, ok := broj(html.UnescapeString(c[1][1]))
		if !ok {
			continue
		}
		d, _ := strconv.Atoi(m[1])
		mj, _ := strconv.Atoi(m[2])
		g, _ := strconv.Atoi(m[3])
		h, _ := strconv.Atoi(m[4])
		out = append(out, RedakTablice{Kraj: time.Date(g, time.Month(mj), d, h, 0, 0, 0, Zagreb), Oborina: v})
	}
	return out
}
