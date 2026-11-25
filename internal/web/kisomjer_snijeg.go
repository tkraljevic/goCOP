package web

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"math"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/snijeg"
)

// Snijeg na stranici kišomjera alpskog sliva (crtež „kišomjer”, Odluke 24.
// 11. 2026.): oznaka „alpski sliv” uz ime, stupci oborine s kišom dolje i
// snijegom (mm vode) gore, stupac „od toga snijeg” u popisu i kartica
// „Snijeg na tlu” s krivuljom sezone iz SNOWGRID-a i procjenom za pet dana.

// SnijegNaTluTocke je kartica „Snijeg na tlu” na stranici kišomjera.
type SnijegNaTluTocke struct {
	Ima      bool
	IzModela bool   // SNOWGRID ovdje nema podataka; krivulja je procjena iz ERA5
	Mm       string // „40 mm vode”
	Opis     string // „oko točke, 23. 11. · raste”
	Graf     template.HTML
}

// jeAlpski kaže je li međusliv alpski (A, B, K, L)
func jeAlpski(sliv string) bool {
	_, ima := snijeg.AlpskiSliv(sliv)
	return ima
}

// izvorSnijega je izvor snijega na tlu; nil kad ga čvor nema
func (h *KisomjerStranicaHandler) izvorSnijega() *snijeg.Izvor {
	if h.snijeg == nil {
		return nil
	}
	return h.snijeg()
}

// snijegUNizu dopisuje snijeg satima i danima izvedene točke alpskog
// sliva: iz arhive, a zadnje dane iz baze oborina, istim pravilom kao
// oborinu (nizKise). Popisi se mijenjaju na mjestu.
func (h *KisomjerStranicaHandler) snijegUNizu(ctx context.Context, p *KisomjerStranica, satni, dnevni []RedKise, od, do time.Time) {
	if !p.Alpski || !p.Procjena {
		return
	}
	poSatu, poDanu := h.snijegIzArhive(ctx, p.Tocka.Code, od, do)
	zivoSat, zivoDan := h.snijegIzBaze(ctx, p.Tocka.Code, od, do)
	for k, v := range zivoSat {
		poSatu[k] = v
	}
	for i := range satni {
		if v, ima := poSatu[satni[i].Kad.Unix()]; ima && satni[i].Sati == 1 {
			satni[i].Snijeg = snijegRetka(v, satni[i].Mm)
		}
	}
	for i := range dnevni {
		d := dnevni[i].Kad.UTC().Format("2006-01-02")
		v, ima := poDanu[d]
		if !ima {
			v, ima = zivoDan[d]
		}
		if ima {
			dnevni[i].Snijeg = snijegRetka(v, dnevni[i].Mm)
		}
	}
}

// snijegRetka: snijeg zaokružen i ne veći od oborine retka
func snijegRetka(v, oborina float64) *float64 {
	x := math.Round(math.Min(v, oborina)*10) / 10
	return &x
}

// snijegIzArhive je snijeg točke u arhivi po satu (unix) i po danu
func (h *KisomjerStranicaHandler) snijegIzArhive(ctx context.Context, code string, od, do time.Time) (map[int64]float64, map[string]float64) {
	poSatu, poDanu := map[int64]float64{}, map[string]float64{}
	a := h.arhiva()
	if a == nil {
		return poSatu, poDanu
	}
	if vs, err := a.SpojRaspon(ctx, code, "snijeg", "satni", od, do, 20000, 0, repository.PoVremenu); err == nil {
		for _, v := range vs {
			poSatu[v.Kad.Unix()] = v.Vrijednost
		}
	}
	odDana := time.Date(od.Year(), od.Month(), od.Day(), 0, 0, 0, 0, time.UTC)
	if vs, err := a.SpojRaspon(ctx, code, "snijeg", "dnevni", odDana, do, 20000, 0, repository.PoVremenu); err == nil {
		for _, v := range vs {
			poDanu[v.Kad.UTC().Format("2006-01-02")] = v.Vrijednost
		}
	}
	return poSatu, poDanu
}

// snijegIzBaze je snijeg točke iz baze oborina (Open-Meteo) po satu i po
// zagrebačkom danu, bez prognoze
func (h *KisomjerStranicaHandler) snijegIzBaze(ctx context.Context, code string, od, do time.Time) (map[int64]float64, map[string]float64) {
	poSatu, poDanu := map[int64]float64{}, map[string]float64{}
	sp := h.mjerenja()
	if sp == nil || sp.DB == nil {
		return poSatu, poDanu
	}
	rows, err := sp.DB.QueryContext(ctx, `SELECT sat, snijeg FROM satne
		WHERE kisomjer = ? AND prognoza = 0 AND snijeg IS NOT NULL AND sat > ? AND sat <= ?`,
		code, od.Unix()/3600, do.Unix()/3600)
	if err != nil {
		return poSatu, poDanu
	}
	defer rows.Close()
	for rows.Next() {
		var s int64
		var v float64
		if rows.Scan(&s, &v) != nil {
			continue
		}
		kad := time.Unix(s*3600, 0).UTC()
		poSatu[kad.Unix()] = v
		poDanu[kad.In(models.Zagreb).Add(-time.Minute).Format("2006-01-02")] += v
	}
	return poSatu, poDanu
}

// snijegZaPrikaz: izvan alpskih slivova stupci su samo oborina
func snijegZaPrikaz(alpski bool, stupci []stupacKise) []stupacKise {
	if alpski {
		return stupci
	}
	for i := range stupci {
		stupci[i].Snijeg = 0
	}
	return stupci
}

// snijegSedam je snijeg zadnjih sedam dana, od dana koji ga imaju
func snijegSedam(dani []DanKise) float64 {
	z := 0.0
	for _, d := range dani {
		if d.Snijeg != nil {
			z += *d.Snijeg
		}
	}
	return math.Round(z*10) / 10
}

// snijegNaTlu puni karticu „Snijeg na tlu” izvedene točke alpskog sliva
func (h *KisomjerStranicaHandler) snijegNaTlu(p *KisomjerStranica, sada time.Time) {
	iz := h.izvorSnijega()
	if !p.Alpski || !p.Procjena || iz == nil {
		return
	}
	st, err := iz.Tocka(p.Tocka.Code)
	if err != nil {
		log.Printf("snijeg na tlu, %s: %v", p.Tocka.Code, err)
		return
	}
	p.SnijegNaTlu = karticaSnijegaTocke(st, sada)
}

// karticaSnijegaTocke slaže karticu iz stanja točke
func karticaSnijegaTocke(st snijeg.StanjeTocke, sada time.Time) *SnijegNaTluTocke {
	k := &SnijegNaTluTocke{Ima: st.Ima, IzModela: st.IzModela}
	if !st.Ima {
		return k
	}
	gdje := "oko točke"
	if st.IzModela {
		gdje = "procjena iz ERA5, SNOWGRID ovdje nema podataka"
	}
	k.Mm = mmVode(st.Mm)
	k.Opis = spoji(" · ", gdje+", "+snijeg.Dan(st.Dan).Format("2. 1."), st.Trend)
	danas := sada.UTC().Unix() / 86400
	k.Graf = crtajSnijegNaTluZaSve(grafSnijega{Sezona: st.Sezona, Procjena: st.Procjena, Od: prognoza.PocetakSezone(danas),
		Danas: danas, Opis: "Snijeg na tlu oko točke od 1. kolovoza: " + k.Mm + " " + k.Opis}, 640, 190)
	return k
}

// crtajStupacKise crta jedan stupac: kiša dolje punom bojom, a snijeg (mm
// vode) iznad nje svijetlo s obrubom; prognoza je svjetlija
func crtajStupacKise(b *strings.Builder, s stupacKise, x0, x1 float64, y func(float64) float64, dno float64) {
	if s.Mm <= 0 {
		return
	}
	w := math.Max(1, (x1-x0)*0.85)
	lijevo := x0 + (x1-x0-w)/2
	sn := math.Min(math.Max(s.Snijeg, 0), s.Mm)
	boja, prozirnost := bojaStupca(s)
	opis := opisStupca(s)
	vrhKise := y(s.Mm - sn)
	fmt.Fprintf(b, `<g style="opacity:%s"><title>%s</title>`, prozirnost, template.HTMLEscapeString(opis))
	if s.Mm-sn > 0 {
		fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" style="fill:%s"/>`, lijevo, vrhKise, w, dno-vrhKise, boja)
	}
	if sn > 0 {
		fmt.Fprintf(b, `<rect class="stupac-snijega" x="%.1f" y="%.1f" width="%.1f" height="%.1f" style="fill:var(--graf-mjerenoploha, #e1e9f5);stroke:%s;stroke-width:1"/>`,
			lijevo, y(s.Mm), w, math.Max(vrhKise-y(s.Mm), 0), boja)
	}
	b.WriteString(`</g>`)
}

// bojaStupca: izmjereno punom bojom, prognoza svjetlije, dan koji traje
// prozirnije
func bojaStupca(s stupacKise) (string, string) {
	switch {
	case s.Prognoza:
		return "var(--primary-muted, #7c97bd)", "0.75"
	case s.Nepotpun:
		return "var(--primary)", "0.6"
	}
	return "var(--primary)", "1"
}

// opisStupca: „21.11.2026.: 13,4 mm, od toga snijeg 12,4 mm vode”
func opisStupca(s stupacKise) string {
	opis := s.Kad.In(models.Zagreb).Format("02.01.2006.")
	if s.Sati < 24 {
		opis = s.Kad.In(models.Zagreb).Format("02.01. 15:04") + "–" + s.Kad.Add(time.Duration(s.Sati)*time.Hour).In(models.Zagreb).Format("15:04")
	}
	switch {
	case s.Prognoza:
		opis += ", prognoza"
	case s.Nepotpun:
		opis += ", dan još traje"
	}
	opis += ": " + brojHRf(s.Mm, 1) + " mm"
	if s.Snijeg > 0 {
		opis += ", od toga snijeg " + brojHRf(math.Min(s.Snijeg, s.Mm), 1) + " mm vode"
	}
	return opis
}
