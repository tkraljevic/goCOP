package web

import (
	"html/template"
	"log"
	"time"

	"gocop/internal/prognoza"
	"gocop/internal/snijeg"
)

// Snijeg na tlu na stranici Slivovi (crtež „slivovi”, Odluke 24. 11. 2026.):
// u svakom alpskom međuslivu kartica s krivuljom sezone (SNOWGRID, prosjek
// austrijskog dijela sliva), procjenom za pet dana i crtama pragova za 3, 5
// i 10 zima. Nizinski međuslivovi je nemaju.

// SnijegSliva je kartica snijega na tlu u odjeljku međusliva.
type SnijegSliva struct {
	Podrucje       string
	Stanje         string // „stanje 23. 11.”
	Mm             string // „170 mm vode”
	Klasa, Rijetko string // razina; prazno ispod praga za 3 zime
	Trend          string // „raste u zadnja 3 dana”
	Kasni          string
	Pragovi        bool // crte pragova su na krivulji
	Graf           template.HTML
}

// SetSnijeg daje stranici snijeg na tlu u alpskim slivovima
func (h *SlivoviHandler) SetSnijeg(f func() *snijeg.Izvor) { h.snijeg = f }

// dodajSnijeg stavlja karticu snijega u odjeljke alpskih međuslivova; bez
// izvora ili podataka kartice nema
func (h *SlivoviHandler) dodajSnijeg(skupine map[string]*SkupinaKisomjera, sad time.Time) {
	if h.snijeg == nil || h.snijeg() == nil {
		return
	}
	slivovi, err := h.snijeg().Slivovi()
	if err != nil {
		log.Printf("snijeg na tlu: %v", err)
		return
	}
	for _, s := range slivovi {
		if sk, ima := skupine[s.Oznaka]; ima && s.Ima {
			sk.Snijeg = snijegSliva(s, sk.Sliv.Naziv, sad)
		}
	}
}

// snijegSliva slaže karticu iz stanja međusliva
func snijegSliva(s snijeg.StanjeSliva, naziv string, sad time.Time) *SnijegSliva {
	k := &SnijegSliva{Podrucje: s.Podrucje, Stanje: "stanje " + snijeg.Dan(s.Dan).Format("2. 1."), Mm: mmVode(s.Mm),
		Kasni: kasniSnijeg([]snijeg.StanjeSliva{s}, sad), Pragovi: s.ImaPragove}
	if s.Trend != "" {
		k.Trend = s.Trend + " u zadnja 3 dana"
	}
	if s.Razina > 0 {
		k.Klasa, k.Rijetko = klaseSnijega[s.Razina], rijetkoSnijega(s.Razina-1)
	}
	g := grafSnijega{Sezona: s.Sezona, Procjena: s.Procjena, Od: snijegOd(s), Danas: sad.UTC().Unix() / 86400,
		Sirina: 960, Visina: 260, Opis: "Snijeg na tlu, " + naziv + ": " + k.Mm}
	if s.ImaPragove {
		p := s.Pragovi
		g.Pragovi = &p
	}
	k.Graf = crtajSnijegNaTlu(g)
	return k
}

// snijegOd je prvi dan osi: 1. kolovoza sezone
func snijegOd(s snijeg.StanjeSliva) int64 {
	if len(s.Sezona) == 0 {
		return s.Dan
	}
	return prognoza.PocetakSezone(s.Sezona[0].Dan)
}
