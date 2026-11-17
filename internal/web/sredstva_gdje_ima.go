package web

import (
	"net/http"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Gdje ima (crtež 19a, 5): ispod skladišta redak zbroja, u retku gumb
// „Prijenos” koji otvara zahvat u tom skladištu, a ispod tablice kartica s
// onim što je te vrste na terenu.

// ZbrojGdjeIma je redak „Ukupno u skladištima”
type ZbrojGdjeIma struct {
	Kolicina, NaPutu float64
	PoOblicima       map[string]float64
}

// zbrojGdjeIma zbraja skladišta tablice
func zbrojGdjeIma(mjesta []service.MjestoZalihe) ZbrojGdjeIma {
	z := ZbrojGdjeIma{PoOblicima: map[string]float64{}}
	for _, m := range mjesta {
		z.Kolicina += m.Kolicina
		z.NaPutu += m.NaPutu
		for o, k := range m.PoOblicima {
			z.PoOblicima[o] += k
		}
	}
	return z
}

// terenVrste je ono što je te vrste na terenu
func terenVrste(stanja []models.Stanje, vrstaID string) []models.Stanje {
	var out []models.Stanje
	for _, st := range stanja {
		if st.VrstaID == vrstaID {
			out = append(out, st)
		}
	}
	return out
}

// dopuniGdjeIma puni zbroj, skladišta iz kojih osoba smije upisati prijenos
// i teren; što se ne da pročitati, stranica prikaže bez kartice terena
func (h *MtsHandler) dopuniGdjeIma(r *http.Request, data *MtsPageData) {
	data.ZbrojGdjeIma = zbrojGdjeIma(data.GdjeIma)
	data.PrijenosIz = map[string]bool{}
	for _, m := range data.GdjeIma {
		sk := m.Skladiste
		data.PrijenosIz[sk.ID] = h.svc().SmijePisati(data.Permissions, &sk)
	}
	teren, _ := h.svc().NaTerenu(r.Context(), "", "") // bez terena ostaje samo tablica skladišta
	data.TerenVrste = terenVrste(teren, data.Vrsta.ID)
}
