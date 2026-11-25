package web

import (
	"fmt"
	"html/template"
	"math"
	"strings"

	"gocop/internal/models"
	"gocop/internal/snijeg"
)

// Krivulja snijega na tlu kroz sezonu (crteži „kišomjer” i „slivovi”, 24.
// 11. 2026.): puna crta do zadnjeg dana SNOWGRID-a, crtkana procjena iz
// prognoze za pet dana, okomita crta danas, a na Slivovima i vodoravne crte
// pragova za 3, 5 i 10 zima.

// grafSnijega je ono što krivulja treba
type grafSnijega struct {
	Sezona, Procjena []snijeg.TockaKrivulje
	Pragovi          *snijeg.Pragovi // nil: bez crta pragova
	Od, Danas        int64           // prvi dan osi (1. 8.) i danas, dani od 1970.
	Sirina, Visina   float64
	Opis             string // krivulja riječima, za čitač zaslona
}

// rubovi crteža: lijevo brojevi osi, desno natpisi pragova, dolje dani
const (
	grafSnijegaLijevo = 36.0
	grafSnijegaVrh    = 10.0
	grafSnijegaDno    = 24.0
)

// crtajSnijegNaTluZaSve crta krivulju dvaput: široku za računalo i usku
// za mobitel, da brojevi i natpisi na mobitelu ostanu čitljivi
func crtajSnijegNaTluZaSve(g grafSnijega, sirina, visina float64) template.HTML {
	usko := g
	usko.Sirina, usko.Visina = 360, 220
	g.Sirina, g.Visina = sirina, visina
	siroko := crtajSnijegNaTlu(g)
	if siroko == "" {
		return ""
	}
	return `<div class="graf-snijega-siroko">` + siroko + `</div><div class="graf-snijega-usko">` + crtajSnijegNaTlu(usko) + `</div>`
}

// crtajSnijegNaTlu crta krivulju kao SVG; bez ijedne točke ništa
func crtajSnijegNaTlu(g grafSnijega) template.HTML {
	if len(g.Sezona) == 0 && len(g.Procjena) == 0 {
		return ""
	}
	desno := 12.0
	if g.Pragovi != nil {
		desno = math.Min(96, g.Sirina/5)
	}
	kraj := g.Danas + snijeg.ProcjenaDana
	gornja, korak := osSnijega(g)
	x := func(d int64) float64 {
		return grafSnijegaLijevo + float64(d-g.Od)/float64(max(kraj-g.Od, 1))*(g.Sirina-grafSnijegaLijevo-desno)
	}
	y := func(v float64) float64 {
		return grafSnijegaVrh + (g.Visina-grafSnijegaVrh-grafSnijegaDno)*(1-math.Min(v, gornja)/gornja)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="graf-snijega" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, g.Sirina, g.Visina,
		template.HTMLEscapeString(g.Opis))
	for v := 0.0; v <= gornja+1e-9; v += korak {
		fmt.Fprintf(&b, `<line class="graf-snijega-mreza" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, grafSnijegaLijevo, g.Sirina-desno, y(v), y(v))
		fmt.Fprintf(&b, `<text class="graf-snijega-os" x="%.1f" y="%.1f" text-anchor="end">%s</text>`, grafSnijegaLijevo-6, y(v)+4, brojHRf(v, 0))
	}
	daniOsi(&b, g, x)
	pragoviSnijega(&b, g, y, g.Sirina-desno)
	fmt.Fprintf(&b, `<polyline class="graf-snijega-snowgrid" points="%s"/>`, tockeKrivulje(g.Sezona, x, y))
	fmt.Fprintf(&b, `<polyline class="graf-snijega-procjena" points="%s"/>`, tockeKrivulje(g.Procjena, x, y))
	if n := len(g.Sezona); n > 0 {
		z := g.Sezona[n-1]
		fmt.Fprintf(&b, `<circle class="graf-snijega-zadnji" cx="%.1f" cy="%.1f" r="3.5"/>`, x(z.Dan), y(z.Mm))
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

// osSnijega: vrh osi i korak mreže; os seže iznad najvećeg snijega i
// najvišeg praga
func osSnijega(g grafSnijega) (gornja, korak float64) {
	najvise := 10.0
	for _, t := range append(append([]snijeg.TockaKrivulje(nil), g.Sezona...), g.Procjena...) {
		najvise = math.Max(najvise, t.Mm)
	}
	if g.Pragovi != nil {
		najvise = math.Max(najvise, g.Pragovi[2]*1.1)
	}
	korak = 10
	for _, k := range []float64{10, 20, 50, 100, 200, 500} {
		korak = k
		if najvise/k <= 5 {
			break
		}
	}
	return korak * math.Ceil(najvise/korak), korak
}

// daniOsi: prvi dan svakog mjeseca („1. 9.”) i „danas” s okomitom crtom
func daniOsi(b *strings.Builder, g grafSnijega, x func(int64) float64) {
	dno := g.Visina - grafSnijegaDno
	razmak := int64(12 * 640 / max(g.Sirina, 1)) // „danas” ne smije prekriti mjesec
	for d := g.Od; d <= g.Danas; d++ {
		t := snijeg.Dan(d)
		if t.Day() != 1 || g.Danas-d < razmak {
			continue
		}
		fmt.Fprintf(b, `<text class="graf-snijega-os" x="%.1f" y="%.1f" text-anchor="middle">%s</text>`, x(d), g.Visina-6, t.Format("2. 1."))
	}
	fmt.Fprintf(b, `<line class="graf-snijega-danas" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, x(g.Danas), x(g.Danas), grafSnijegaVrh-4, dno)
	fmt.Fprintf(b, `<text class="graf-snijega-os" x="%.1f" y="%.1f" text-anchor="middle">danas</text>`, x(g.Danas), g.Visina-6)
}

// pragoviSnijega: vodoravna crta svakog praga s natpisom „3 zime 130”
func pragoviSnijega(b *strings.Builder, g grafSnijega, y func(float64) float64, desno float64) {
	if g.Pragovi == nil {
		return
	}
	for i, p := range g.Pragovi {
		n := snijeg.Zime[i]
		natpis := fmt.Sprintf("%d %s %s", n, models.UzBrojHR(n, "zima", "zime", "zima"), brojHRf(p, 0))
		fmt.Fprintf(b, `<line class="graf-snijega-prag %s" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`,
			klaseSnijega[i+1], grafSnijegaLijevo, desno, y(p), y(p))
		fmt.Fprintf(b, `<text class="graf-snijega-natpis %s" x="%.1f" y="%.1f">%s</text>`, klaseSnijega[i+1], desno+6, y(p)+4, natpis)
	}
}

// tockeKrivulje su točke za polyline
func tockeKrivulje(t []snijeg.TockaKrivulje, x func(int64) float64, y func(float64) float64) string {
	dijelovi := make([]string, 0, len(t))
	for _, p := range t {
		dijelovi = append(dijelovi, fmt.Sprintf("%.1f,%.1f", x(p.Dan), y(p.Mm)))
	}
	return strings.Join(dijelovi, " ")
}
