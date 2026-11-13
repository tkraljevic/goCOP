package web

import (
	"maps"
	"net/url"
	"strconv"
	"strings"

	"gocop/internal/models"
)

// Popis vodomjernih postaja (goCOP-m3, 17c · Registri, 4a): čipovi Sve i Čeka
// pregled, pretraga, vodotok i država, tablica na računalu i kartice na
// mobitelu. Čipovi sektora, „Bez pragova” i „Bez kote nule” su novo ponašanje.

// izgledPopisaLetava upisuje čipove pregleda i adresu Karte. Čipovi čuvaju
// pretragu, vodotok i državu; broje cijeli registar, kao i prije.
func izgledPopisaLetava(d *StationsPageData, q url.Values) {
	osnova := zadrziParametre(q, "q", "watercourse", "country")
	pregled := maps.Clone(osnova)
	pregled.Set("review", "1")
	d.CipoviPregleda = []CipPopisa{
		{Naziv: "Sve (" + strconv.Itoa(d.TotalStations) + ")", URL: adresaSIzborom("/stations", osnova), Odabran: !d.OnlyNeedsReview},
		{Naziv: "Čeka pregled (" + strconv.Itoa(d.NeedsReview) + ")", URL: adresaSIzborom("/stations", pregled), Odabran: d.OnlyNeedsReview},
	}
	d.KartaURL = adresaKarteStanica(d.Stations)
}

// adresaKarteStanica otvara glavnu Kartu zumiranu na letve s ove stranice
// popisa (Odluke (4. 11. 2026.): svaka karta u programu je glavna Karta).
func adresaKarteStanica(letve []models.Station) string {
	id := make([]string, 0, len(letve))
	for _, s := range letve {
		id = append(id, s.ID.String())
	}
	if len(id) == 0 {
		return "/karta"
	}
	return "/karta#letve=" + strings.Join(id, ",")
}

// pragoviPopisa piše pragove P, R, I i IS u centimetrima, crticom gdje ih nema:
// „120 · 200 · 250 · —”.
func pragoviPopisa(st models.Station) string {
	var out []string
	for _, p := range []models.Threshold{st.Prep, st.Regular, st.Emergency, st.State} {
		if p.Cm == nil {
			out = append(out, "—")
			continue
		}
		out = append(out, strconv.Itoa(*p.Cm))
	}
	return strings.Join(out, " · ")
}
