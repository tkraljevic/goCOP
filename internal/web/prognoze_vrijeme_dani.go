package web

import (
	"context"
	"database/sql"
	"math"
	"strconv"
	"time"

	"gocop/internal/dhmz"
	"gocop/internal/kisomjeri"
	"gocop/internal/models"
)

// Dani prognoze regije: DHMZ za regiju daje samo tekst za danas, pa dani
// idu iz satnih vrijednosti Open-Meteo koje program ionako preuzima za
// izvedene točke međuslivova (oborine.db, tablica satne): najniža i
// najviša temperatura i kiša po danu. Oblačnosti ondje nema, pa ikona
// kaže samo ima li kiše. Točka mora biti do 40 km od mjesta; izvedene
// točke postoje samo u slivovima Drave, Mure i Dunava.

const (
	daniPrognoze     = 5  // danas i četiri dana
	najdaljaTockaDan = 40 // km
	satiZaDan        = 20 // dan s manje satnih vrijednosti se ne pokazuje
)

// zbrojDana su satne vrijednosti jednog dana
type zbrojDana struct {
	min, max     float64
	kisa, snijeg float64
	sati, temp   int
}

// daniPrognoze vraća dane s najbliže aktivne izvedene točke do 40 km i
// odakle su; bez točke ili bez podataka ništa
func (h *VrijemeHandler) daniPrognoze(ctx context.Context, lat, lon float64, sada time.Time) ([]DanVremena, string) {
	svc, sp := h.izvoriKise()
	if svc == nil || sp == nil {
		return nil, ""
	}
	tocke, err := svc.ListKisomjeri(ctx)
	if err != nil {
		return nil, ""
	}
	poUdaljenosti(tocke, lat, lon)
	for _, t := range tocke {
		if !t.Aktivan || t.JeStvarni() {
			continue
		}
		km := dhmz.Udaljenost(lat, lon, t.Latitude, t.Longitude)
		if km > najdaljaTockaDan {
			return nil, ""
		}
		dani := daniTocke(sp, t.Code, sada)
		if len(dani) == 0 {
			return nil, ""
		}
		return dani, "Po danima: Open-Meteo za točku " + t.Naziv + ", " + brojHRf(km, 0) + " km. Ikona pokazuje samo kišu."
	}
	return nil, ""
}

// daniTocke zbraja satne vrijednosti točke po danu, od današnje ponoći
func daniTocke(sp *kisomjeri.Spremiste, code string, sada time.Time) []DanVremena {
	ponoc := ponocDana(sada)
	kraj := ponoc.AddDate(0, 0, daniPrognoze)
	r, err := sp.DB.Query(`SELECT sat, oborina, snijeg, temperatura FROM satne WHERE kisomjer = ? AND sat >= ? AND sat < ?`,
		code, ponoc.Unix()/3600, kraj.Unix()/3600)
	if err != nil {
		return nil
	}
	defer r.Close()
	zbrojevi := make([]zbrojDana, daniPrognoze)
	for r.Next() {
		var s int64
		var obo, snij, temp sql.NullFloat64
		if r.Scan(&s, &obo, &snij, &temp) != nil {
			continue
		}
		i := danOd(ponoc, time.Unix(s*3600, 0))
		if i >= 0 && i < daniPrognoze {
			zbrojevi[i].dodaj(obo, snij, temp)
		}
	}
	var out []DanVremena
	for i, z := range zbrojevi {
		if z.sati >= satiZaDan {
			out = append(out, z.dan(ponoc.AddDate(0, 0, i), i))
		}
	}
	return out
}

// danOd je koji je dan od ponoći t, po našem vremenu (ljetno računanje
// vremena mijenja duljinu dana)
func danOd(ponoc, t time.Time) int {
	l := t.In(models.Zagreb)
	d := time.Date(l.Year(), l.Month(), l.Day(), 12, 0, 0, 0, models.Zagreb)
	return int(math.Floor(d.Sub(ponoc).Hours() / 24))
}

// dodaj upisuje jedan sat; rupa u oborini je sat bez podatka
func (z *zbrojDana) dodaj(obo, snij, temp sql.NullFloat64) {
	if !obo.Valid {
		return
	}
	z.sati++
	z.kisa += obo.Float64
	if snij.Valid {
		z.snijeg += snij.Float64
	}
	if !temp.Valid {
		return
	}
	if z.temp == 0 || temp.Float64 < z.min {
		z.min = temp.Float64
	}
	if z.temp == 0 || temp.Float64 > z.max {
		z.max = temp.Float64
	}
	z.temp++
}

// dan piše dan riječima: „Danas”, „Pet 6. 11.”, „1 / 11 °C”, „kiša 7 mm”
func (z zbrojDana) dan(d time.Time, i int) DanVremena {
	out := DanVremena{Naslov: velikoSlovo(daniUTjednu[d.Weekday()]) + " " + strconv.Itoa(d.Day()) + ". " + strconv.Itoa(int(d.Month())) + ".",
		Kisa: "kiša " + mmSliva(z.kisa)}
	if i == 0 {
		out.Naslov = "Danas"
	}
	if z.temp > 0 {
		out.Temp = stupnjevi(z.min) + " / " + stupnjevi(z.max) + " °C"
	}
	if z.kisa >= 1 {
		out.Ikona = "cloud-rain"
	}
	if z.snijeg >= 0.5 {
		out.Kisa += " · snijeg " + brojHRf(z.snijeg, 0) + " cm"
	}
	return out
}

// stupnjevi su cijeli stupnjevi, bez „-0”
func stupnjevi(v float64) string { return strconv.Itoa(int(math.Round(v))) }
