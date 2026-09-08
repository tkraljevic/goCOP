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
func crtajNiz(vals []models.SpojenaVrijednost, velicina string, station *models.Station,
	krivulje []models.HQKrivulja) *Chart {
	if len(vals) < 2 {
		return nil
	}
	pts := make([]models.SpojenaVrijednost, len(vals))
	copy(pts, vals)
	sort.Slice(pts, func(i, j int) bool { return pts[i].Kad.Before(pts[j].Kad) })

	dec := decimalaVelicine(velicina)
	jed := models.JedinicaVelicine(velicina)

	// Koordinatni sustav je namjerno velik: SVG se razvlači na širinu stupca,
	// pa bi u malom sustavu svaka oznaka narasla tri puta. Ovako je omjer blizu
	// jedan naprama jedan i tekst ostaje sitan.
	c := &Chart{Width: 1600, Height: 420, From: pts[0].Kad, To: pts[len(pts)-1].Kad}
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
	if station != nil && (velicina == "vodostaj" || velicina == "protok") {
		// Za protok se prag prevodi krivuljom koja vrijedi u prikazanom
		// razdoblju: stupanj obrane je isti, samo iskazan u m³/s. Bez krivulje
		// se ne crta ništa — pogađati prag u protoku bilo bi izmišljanje.
		var kr *models.HQKrivulja
		if velicina == "protok" {
			kr = krivuljaZa(krivulje, pts[len(pts)/2].Kad)
			if kr == nil {
				goto bezPragova
			}
		}
		for _, t := range []struct {
			t     models.Threshold
			l, cl string
		}{
			{station.Prep, "pripremno", "prep"},
			{station.Regular, "redovna", "regular"},
			{station.Emergency, "izvanredna", "emerg"},
			{station.State, "izvanredno", "crit"},
		} {
			if !t.t.IsUsable() {
				continue
			}
			v := float64(*t.t.Cm)
			if kr != nil {
				q, ok := kr.Protok(*t.t.Cm)
				if !ok {
					continue
				}
				v = q
			}
			granica := (najv - najn) * 0.15
			if granica < 50 {
				granica = 50
			}
			if v <= najv+granica && v >= najn-granica {
				najn = math.Min(najn, v)
				najv = math.Max(najv, v)
				pragovi = append(pragovi, struct {
					cm    int
					label string
					class string
				}{int(math.Round(v)), t.l, t.cl})
			}
		}
	}
bezPragova:
	if najv == najn {
		najv += math.Max(1, math.Abs(najv)*0.05)
	}
	pad := (najv - najn) / 10
	c.Min, c.Max = int(math.Floor(najn-pad)), int(math.Ceil(najv+pad))

	const left, right, top, bottom = 96.0, 116.0, 22.0, 46.0
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

	// Rupa u nizu ne smije se povući ravnom crtom: to bi tvrdilo da podatak
	// postoji ondje gdje ga nema. Crta se prekida kad razmak nadmaši višekratnik
	// uobičajenog, pa se prazno razdoblje i vidi kao prazno.
	prag := prazninaPrag(pts)

	var crta, ploha strings.Builder
	novaDionica := true
	var pocetakX float64
	zatvori := func(krajX float64) {
		if !novaDionica {
			fmt.Fprintf(&ploha, " L%.1f %.1f L%.1f %.1f Z", krajX, top+plotH, pocetakX, top+plotH)
		}
	}
	for i, p := range pts {
		t := ChartPoint{X: xOf(p.Kad), Y: yOf(p.Vrijednost), Level: int(math.Round(p.Vrijednost)), At: p.Kad,
			Vrijednost: p.Vrijednost, Oznaka: brojHRf(p.Vrijednost, dec) + " " + jed}
		c.Points = append(c.Points, t)

		rupa := i > 0 && prag > 0 && p.Kad.Sub(pts[i-1].Kad) > prag
		if rupa {
			zatvori(c.Points[i-1].X)
			c.Praznina++
			novaDionica = true
		}
		if novaDionica {
			fmt.Fprintf(&crta, " M%.1f %.1f", t.X, t.Y)
			fmt.Fprintf(&ploha, " M%.1f %.1f", t.X, t.Y)
			pocetakX = t.X
			novaDionica = false
			continue
		}
		fmt.Fprintf(&crta, " L%.1f %.1f", t.X, t.Y)
		fmt.Fprintf(&ploha, " L%.1f %.1f", t.X, t.Y)
	}
	zatvori(c.Points[len(c.Points)-1].X)
	c.Path = strings.TrimSpace(crta.String())
	c.Area = strings.TrimSpace(ploha.String())

	// Deset podjela umjesto pet: vodostaj time dobiva korak od 100 cm, koji se
	// i inače čita, umjesto 200.
	korak := niceStep(float64(c.Max-c.Min) / 10)
	for v := math.Ceil(float64(c.Min)/korak) * korak; v <= float64(c.Max); v += korak {
		c.YTicks = append(c.YTicks, ChartTick{Pos: yOf(v), Label: brojHRf(v, decimalaOsi(korak))})
	}
	for _, p := range pragovi {
		c.Thresholds = append(c.Thresholds, ChartLine{Y: yOf(float64(p.cm)), Label: p.label, Class: p.class})
	}
	c.XTicks = vodoravneOznake(c.From, c.To, xOf)

	// Točke za pokazivač uz miša. Sam SVG ih ne crta — sedamsto kružića je
	// teška slika i nečitljiva crta — nego ih čita skripta i pokazuje onu nad
	// kojom je miš.
	var sj strings.Builder
	sj.WriteByte('[')
	for i, p := range c.Points {
		if i > 0 {
			sj.WriteByte(',')
		}
		fmt.Fprintf(&sj, `[%.1f,%.1f,%q,%q]`, p.X, p.Y,
			p.At.Format("2.1.2006."), p.Oznaka)
	}
	sj.WriteByte(']')
	c.Tocke = sj.String()
	return c
}

// vodoravneOznake bira oznake na vremenskoj osi prema rasponu: mjeseci za
// godinu, dani za kraće razdoblje. Tri oznake na cijelu godinu ne govore ništa
// o tome kad se što dogodilo.
func vodoravneOznake(od, do time.Time, xOf func(time.Time) float64) []ChartTick {
	var out []ChartTick
	raspon := do.Sub(od)
	switch {
	case raspon > 80*24*time.Hour:
		// prvi dan svakog mjeseca
		t := time.Date(od.Year(), od.Month(), 1, 0, 0, 0, 0, od.Location())
		if t.Before(od) {
			t = t.AddDate(0, 1, 0)
		}
		korak := 1
		if raspon > 400*24*time.Hour {
			korak = 3
		}
		for i := 0; t.Before(do) || t.Equal(do); t, i = t.AddDate(0, korak, 0), i+1 {
			out = append(out, ChartTick{Pos: xOf(t), Label: mjesecKratko(t)})
		}
	case raspon > 5*24*time.Hour:
		korak := raspon / 6
		for t := od; t.Before(do) || t.Equal(do); t = t.Add(korak) {
			out = append(out, ChartTick{Pos: xOf(t), Label: t.Format("2.1.")})
		}
	default:
		korak := raspon / 5
		for t := od; t.Before(do) || t.Equal(do); t = t.Add(korak) {
			out = append(out, ChartTick{Pos: xOf(t), Label: t.Format("2.1. 15h")})
		}
	}
	return out
}

// mjesecKratko je mjesec u tri slova; siječanj nosi i godinu, jer graf zna
// prijeći granicu godine.
func mjesecKratko(t time.Time) string {
	kratice := []string{"sij", "velj", "ožu", "tra", "svi", "lip", "srp", "kol", "ruj", "lis", "stu", "pro"}
	m := kratice[int(t.Month())-1]
	if t.Month() == time.January {
		return m + " " + t.Format("06")
	}
	return m
}

// krivuljaZa vraća HQ krivulju koja vrijedi u zadanom trenutku. Korito se
// mijenja pa se krivulja povremeno iznova postavlja; prag u protoku ovisi o
// tome koja je tada vrijedila.
func krivuljaZa(krivulje []models.HQKrivulja, kad time.Time) *models.HQKrivulja {
	d := kad.Format("2006-01-02")
	for i := range krivulje {
		k := &krivulje[i]
		if k.VrijediOd <= d && (k.VrijediDo == "" || d <= k.VrijediDo) {
			return k
		}
	}
	return nil
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

// istakni označava na grafu razdoblje koje je upravo prikazano u tablici.
// Graf se ne prerazapinje: ostaje cijelo razdoblje, a istaknuti dio pokazuje
// gdje se u njemu nalazi ono što se čita.
func istakni(c *Chart, od, do time.Time) {
	if c == nil || od.IsZero() || do.IsZero() || !do.After(od) {
		return
	}
	const left, right = 96.0, 116.0
	plotW := float64(c.Width) - left - right
	span := c.To.Sub(c.From).Seconds()
	if span <= 0 {
		return
	}
	x := func(t time.Time) float64 {
		u := t.Sub(c.From).Seconds() / span
		if u < 0 {
			u = 0
		}
		if u > 1 {
			u = 1
		}
		return left + u*plotW
	}
	x0, x1 := x(od), x(do)
	if x1-x0 < 2 {
		x1 = x0 + 2 // razdoblje kraće od dva piksela ipak mora biti vidljivo
	}
	c.IstakniOd, c.IstakniSir = x0, x1-x0
	c.Istaknuto = true
}

// prazninaPrag je razmak nakon kojega se crta prekida. Uzima se iz samog niza:
// srednji razmak pomnožen s pet. Niz koji je gust svaki sat prekida se nakon
// nekoliko sati, a dnevni tek nakon tjedan dana — pa isto pravilo radi i na
// satnom nizu i na godišnjem, bez ijedne unaprijed upisane brojke.
//
// Vraća nulu kad niz nema dovoljno točaka da se razmak uopće procijeni.
func prazninaPrag(pts []models.SpojenaVrijednost) time.Duration {
	if len(pts) < 4 {
		return 0
	}
	razmaci := make([]time.Duration, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		if d := pts[i].Kad.Sub(pts[i-1].Kad); d > 0 {
			razmaci = append(razmaci, d)
		}
	}
	if len(razmaci) < 3 {
		return 0
	}
	sort.Slice(razmaci, func(i, j int) bool { return razmaci[i] < razmaci[j] })
	srednji := razmaci[len(razmaci)/2]
	if srednji <= 0 {
		return 0
	}
	return srednji * 5
}
