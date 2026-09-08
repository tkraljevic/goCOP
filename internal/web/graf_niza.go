package web

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
)

// Graf spojenog niza, za bilo koju veličinu.
//
// buildChart je pisan za vodostaj: crta pojaseve faza obrane i računa u
// centimetrima. Protok, temperatura i nanos nemaju pragove i imaju svoje
// jedinice i decimale, pa im treba graf koji to zna — inače bi prikazivao
// brojku koja ne znači ono što uz nju piše.

// crtajNiz gradi graf iz spojenih vrijednosti. Pragovi se crtaju samo za
// vodostaj i samo kad je letva poznata.
func crtajNiz(vals []models.SpojenaVrijednost, velicina string, station *models.Station) *Chart {
	if len(vals) < 2 {
		return nil
	}
	pts := make([]models.SpojenaVrijednost, len(vals))
	copy(pts, vals)
	sort.Slice(pts, func(i, j int) bool { return pts[i].Kad.Before(pts[j].Kad) })

	dec := decimalaVelicine(velicina)
	jed := models.JedinicaVelicine(velicina)

	c := &Chart{Width: 640, Height: 220, From: pts[0].Kad, To: pts[len(pts)-1].Kad}
	najn, najv := pts[0].Vrijednost, pts[0].Vrijednost
	for _, p := range pts {
		najn = math.Min(najn, p.Vrijednost)
		najv = math.Max(najv, p.Vrijednost)
	}
	// pragovi ulaze u raspon samo kad su blizu, da jedan visok prag ne
	// spljošti cijeli graf
	var pragovi []struct {
		cm    int
		label string
		class string
	}
	if velicina == "vodostaj" && station != nil {
		for _, t := range []struct {
			t     models.Threshold
			l, cl string
		}{
			{station.Prep, "pripremno", "prep"},
			{station.Regular, "redovna", "regular"},
			{station.Emergency, "izvanredna", "emerg"},
			{station.State, "izvanredno", "crit"},
		} {
			if t.t.IsUsable() && float64(*t.t.Cm) <= najv+50 && float64(*t.t.Cm) >= najn-50 {
				najn = math.Min(najn, float64(*t.t.Cm))
				najv = math.Max(najv, float64(*t.t.Cm))
				pragovi = append(pragovi, struct {
					cm    int
					label string
					class string
				}{*t.t.Cm, t.l, t.cl})
			}
		}
	}
	if najv == najn {
		najv += math.Max(1, math.Abs(najv)*0.05)
	}
	pad := (najv - najn) / 10
	c.Min, c.Max = int(math.Floor(najn-pad)), int(math.Ceil(najv+pad))

	const left, right, top, bottom = 52.0, 12.0, 12.0, 28.0
	plotW := float64(c.Width) - left - right
	plotH := float64(c.Height) - top - bottom
	span := c.To.Sub(c.From).Seconds()
	if span <= 0 {
		span = 1
	}
	yOf := func(v float64) float64 {
		return top + plotH - (v-float64(c.Min))/float64(c.Max-c.Min)*plotH
	}
	xOf := func(t time.Time) float64 { return left + t.Sub(c.From).Seconds()/span*plotW }

	var sb strings.Builder
	for i, p := range pts {
		t := ChartPoint{X: xOf(p.Kad), Y: yOf(p.Vrijednost), Level: int(math.Round(p.Vrijednost)), At: p.Kad,
			Vrijednost: p.Vrijednost, Oznaka: brojHRf(p.Vrijednost, dec) + " " + jed}
		c.Points = append(c.Points, t)
		if i == 0 {
			fmt.Fprintf(&sb, "M%.1f %.1f", t.X, t.Y)
		} else {
			fmt.Fprintf(&sb, " L%.1f %.1f", t.X, t.Y)
		}
	}
	c.Path = sb.String()
	prvi, zadnji := c.Points[0], c.Points[len(c.Points)-1]
	c.Area = fmt.Sprintf("%s L%.1f %.1f L%.1f %.1f Z", c.Path, zadnji.X, top+plotH, prvi.X, top+plotH)

	korak := niceStep(float64(c.Max-c.Min) / 5)
	for v := math.Ceil(float64(c.Min)/korak) * korak; v <= float64(c.Max); v += korak {
		c.YTicks = append(c.YTicks, ChartTick{Pos: yOf(v), Label: brojHRf(v, decimalaOsi(korak))})
	}
	for _, p := range pragovi {
		c.Thresholds = append(c.Thresholds, ChartLine{Y: yOf(float64(p.cm)), Label: p.label, Class: p.class})
	}
	for _, t := range []time.Time{c.From, c.From.Add(time.Duration(span/2) * time.Second), c.To} {
		c.XTicks = append(c.XTicks, ChartTick{Pos: xOf(t), Label: t.Format("2.1.2006.")})
	}
	return c
}

// decimalaVelicine je koliko decimala veličina traži da bi značila ono što piše.
func decimalaVelicine(v string) int {
	switch v {
	case "temperatura", "koncentracija":
		return 1
	}
	return 0
}

// decimalaOsi bira decimale oznaka na osi prema koraku: korak od 0,5 bez
// decimale bio bi niz istih brojeva.
func decimalaOsi(korak float64) int {
	if korak >= 1 {
		return 0
	}
	return 1
}

// prorijediNiz svodi dugačak niz na oko ciljBroj točaka, uzimajući iz svakog
// razdoblja najnižu i najvišu vrijednost. Vrh vala tako ostaje; uzimanje svake
// n-te izgubilo bi ga.
func prorijediNiz(vals []models.SpojenaVrijednost, ciljBroj int) []models.SpojenaVrijednost {
	if ciljBroj < 4 || len(vals) <= ciljBroj {
		return vals
	}
	red := make([]models.SpojenaVrijednost, len(vals))
	copy(red, vals)
	sort.Slice(red, func(i, j int) bool { return red[i].Kad.Before(red[j].Kad) })

	kosara := len(red) * 2 / ciljBroj
	if kosara < 2 {
		return red
	}
	out := make([]models.SpojenaVrijednost, 0, ciljBroj+2)
	for i := 0; i < len(red); i += kosara {
		kraj := i + kosara
		if kraj > len(red) {
			kraj = len(red)
		}
		naj, nis := i, i
		for j := i; j < kraj; j++ {
			if red[j].Vrijednost > red[naj].Vrijednost {
				naj = j
			}
			if red[j].Vrijednost < red[nis].Vrijednost {
				nis = j
			}
		}
		if naj == nis {
			out = append(out, red[naj])
			continue
		}
		a, b := naj, nis
		if nis < naj {
			a, b = nis, naj
		}
		out = append(out, red[a], red[b])
	}
	return out
}
