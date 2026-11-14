package web

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"gocop/internal/models"
)

// Obilazak su moje letve na vrhu popisa Očitanja: što je danas već upisano,
// koliko je ostalo i gumb do prve neočitane. Prenesen sa stranice Teren
// (Odluke (14. 11. 2026.)); „danas” znači samo očitanje koje je upisao čovjek.
type Obilazak struct {
	Letve      []RedakObilaska
	Ocitano    int
	Postotak   int
	NastaviURL string // prva letva redom obilaska koja danas još nije očitana
	NastaviIme string
}

// RedakObilaska je jedna moja letva u obilasku
type RedakObilaska struct {
	G       models.GaugeSummary
	Danas   string // „07:10 · Ivo Proba” kad je danas očitana
	Vrijed  string // današnji vodostaj
	Obicno  string // „obično oko 07:25”
	UpisURL string // upis s povratkom na isti popis
}

// slozObilazak slaže obilazak iz mojih letava redom obilaska; bez mojih
// letava nema ni obilaska
func slozObilazak(letve []models.GaugeSummary, povratak string, sad time.Time) *Obilazak {
	if len(letve) == 0 {
		return nil
	}
	o := &Obilazak{}
	for _, g := range letve {
		r := RedakObilaska{G: g, UpisURL: g.NewURL + "&back=" + url.QueryEscape(povratak)}
		if t := g.UsualTime(); t != "" {
			r.Obicno = "obično oko " + t
		}
		if d := g.Danas; d != nil {
			o.Ocitano++
			r.Danas = kadOcitano(d.MeasuredAt, sad)
			if ime := strings.TrimSpace(d.Observer); ime != "" {
				r.Danas += " · " + ime
			}
			if d.LevelCm != nil {
				r.Vrijed = fmt.Sprintf("%d cm", *d.LevelCm)
			}
		} else if o.NastaviURL == "" {
			o.NastaviURL, o.NastaviIme = r.UpisURL, g.Name
		}
		o.Letve = append(o.Letve, r)
	}
	o.Postotak = o.Ocitano * 100 / len(o.Letve)
	return o
}
