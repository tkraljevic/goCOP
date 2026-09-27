package kisomjeri

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ZadanaTablicaPljuska je javna tablica trenutnog vremena mreže pljusak.com
const ZadanaTablicaPljuska = "https://pljusak.com/trenutno-vrijeme-tablica.php"

// Pljusak čita javnu tablicu pljusak.com, mreže amaterskih postaja. Jedna
// stranica nosi sve postaje: vrijeme zadnjeg javljanja, kišu u zadnjem satu i
// zbroj od ponoći. Povijesti nema, pa se niz skuplja čitanjem svaki krug:
// satna vrijednost ide na puni sat najbliži javljanju, a dnevni zbroj se
// prepisuje dok dan traje i ostaje kakav je bio zadnji.
//
// Postaje dolaze i odlaze; ona koja se nije javila dva sata preskače se.
type Pljusak struct {
	Adresa string // prazno znači ZadanaTablicaPljuska
	HTTP   *http.Client
	Sada   func() time.Time
}

func (p *Pljusak) Preuzmi(ctx context.Context, postaje []Postaja) ([]Mjerenje, error) {
	adresa := p.Adresa
	if adresa == "" {
		adresa = ZadanaTablicaPljuska
	}
	c := p.HTTP
	if c == nil {
		c = &http.Client{Timeout: 60 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adresa, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goCOP kisomjeri")
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pljusak.com: %s", res.Status)
	}
	tijelo, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	sada := time.Now()
	if p.Sada != nil {
		sada = p.Sada()
	}
	redci := CitajPljusak(string(tijelo), sada)
	var out []Mjerenje
	for _, pos := range postaje {
		r, ima := redci[pos.IzvorSifra]
		if !ima || sada.Sub(r.Javljeno) > 2*time.Hour {
			continue
		}
		if r.ImaSat {
			sat := r.Javljeno.Add(30 * time.Minute).Truncate(time.Hour) // najbliži puni sat
			out = append(out, Mjerenje{Kisomjer: pos.Code, Kraj: sat.UTC(), Sati: 1, Oborina: r.Sat, Izvor: "kisomjer-pljusak"})
		}
		if r.ImaDanas {
			l := r.Javljeno.In(Zagreb)
			ponoc := time.Date(l.Year(), l.Month(), l.Day()+1, 0, 0, 0, 0, Zagreb)
			out = append(out, Mjerenje{Kisomjer: pos.Code, Kraj: ponoc.UTC(), Sati: 24, Oborina: r.Danas, Izvor: "kisomjer-pljusak"})
		}
	}
	return out, nil
}

// RedakPljuska je jedna postaja iz tablice.
type RedakPljuska struct {
	Sifra, Ime       string
	Javljeno         time.Time
	Sat, Danas       float64
	ImaSat, ImaDanas bool
}

var (
	rePljRedak   = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	rePljCelija  = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	rePljSifra   = regexp.MustCompile(`stanica=([\w\-]+)`)
	rePljOznake  = regexp.MustCompile(`<[^>]+>`)
	rePljVrijeme = regexp.MustCompile(`^(\d{1,2}):(\d\d)$`)
	rePljOBO1    = regexp.MustCompile(`(?s)<td class="OBO_1"[^>]*>(.*?)</td>`)
	rePljOBOD    = regexp.MustCompile(`(?s)<td class="OBO_D"[^>]*>(.*?)</td>`)
)

// CitajPljusak čita tablicu. Zadnja tri stupca su oborina u zadnjem satu,
// danas i ovaj mjesec; peti je vrijeme javljanja (HH:MM, lokalno, danas —
// ili jučer kad bi inače bilo u budućnosti).
func CitajPljusak(s string, sada time.Time) map[string]RedakPljuska {
	out := map[string]RedakPljuska{}
	lok := sada.In(Zagreb)
	for _, r := range rePljRedak.FindAllStringSubmatch(s, -1) {
		sifra := rePljSifra.FindStringSubmatch(r[1])
		if sifra == nil {
			continue
		}
		var c []string
		for _, x := range rePljCelija.FindAllStringSubmatch(r[1], -1) {
			c = append(c, strings.TrimSpace(strings.Join(strings.Fields(html.UnescapeString(rePljOznake.ReplaceAllString(x[1], " "))), " ")))
		}
		if len(c) < 8 {
			continue
		}
		v := rePljVrijeme.FindStringSubmatch(c[4])
		if v == nil {
			continue
		}
		h, _ := strconv.Atoi(v[1])
		m, _ := strconv.Atoi(v[2])
		kad := time.Date(lok.Year(), lok.Month(), lok.Day(), h, m, 0, 0, Zagreb)
		if kad.After(sada.Add(10 * time.Minute)) {
			kad = kad.AddDate(0, 0, -1)
		}
		red := RedakPljuska{Sifra: sifra[1], Ime: c[2], Javljeno: kad}
		// ćelije oborine nose svoju klasu; bez nje vrijede zadnji stupci
		satna, dnevna := c[len(c)-3], c[len(c)-2]
		if x := rePljOBO1.FindStringSubmatch(r[1]); x != nil {
			satna = rePljOznake.ReplaceAllString(x[1], "")
		}
		if x := rePljOBOD.FindStringSubmatch(r[1]); x != nil {
			dnevna = rePljOznake.ReplaceAllString(x[1], "")
		}
		red.Sat, red.ImaSat = broj(satna)
		red.Danas, red.ImaDanas = broj(dnevna)
		if _, vec := out[red.Sifra]; !vec {
			out[red.Sifra] = red
		}
	}
	return out
}
