package web

import (
	"context"
	"fmt"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Roba na putu na Naslovnoj (Odluke 16. 11. 2026., Na putu, 4): odredište u
// kartici Za riješiti vidi koliko mu pošiljaka stiže, a sektor i dežurni
// COP-a pošiljke koje nitko ne zaprima dulje od dana.

// NaPutuDuljeOd je koliko pošiljka smije biti na putu prije nego je COP vidi
// na Naslovnoj; mjeri se od upisa slanja
const NaPutuDuljeOd = 24 * time.Hour

// posiljke čita robu na putu po filtru; nil bez izvora ili uz grešku, jer
// bez knjige puta redak samo izostaje
func (h *DashboardHandler) posiljke(ctx context.Context, f repository.FiltarPosiljki) []models.Posiljka {
	if h.izvori.Posiljke == nil {
		return nil
	}
	p, _ := h.izvori.Posiljke(ctx, f) // bez knjige puta redak izostaje
	return p
}

// brojPosiljaka: „3 pošiljke”
func brojPosiljaka(n int) string {
	return fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "pošiljka", "pošiljke", "pošiljaka"))
}

// stizeSkladistima: skladište kojem nešto stiže dobije redak „Stiže”
func (h *DashboardHandler) stizeSkladistima(ctx context.Context, skladista []models.Skladiste) []RedakKartice {
	var out []RedakKartice
	for _, s := range skladista {
		if n := len(h.posiljke(ctx, repository.FiltarPosiljki{PremaSkladisteID: s.ID})); n > 0 {
			out = append(out, RedakKartice{Ikona: "package", Glavno: "Stiže: " + brojPosiljaka(n), Sporedno: s.Naziv + " · zaprimite kad roba stigne",
				URL: "/sredstva/skladista/" + s.ID})
		}
	}
	return out
}

// stizeNaTeren: redak „Stiže na teren” za branjeno područje
func (h *DashboardHandler) stizeNaTeren(ctx context.Context, sektor string, area int) []RedakKartice {
	n := len(h.posiljke(ctx, repository.FiltarPosiljki{PremaAreaID: area}))
	if n == 0 {
		return nil
	}
	return []RedakKartice{{Ikona: "package", Glavno: "Stiže na teren: " + brojPosiljaka(n), Sporedno: "zaprimite kad roba stigne",
		URL: "/sredstva/na-terenu?sektor=" + sektor}}
}

// dugoNaPutu: pošiljke sektora koje su na putu dulje od dana, svaka u svom
// retku, s poveznicom na zaprimanje koje dežurni smije upisati umjesto
// odredišta
func (h *DashboardHandler) dugoNaPutu(ctx context.Context, sektor string, sad time.Time) []RedakKartice {
	if sektor == "" {
		return nil
	}
	vrste := h.vrsteSredstava(ctx)
	var out []RedakKartice
	for _, p := range h.posiljke(ctx, repository.FiltarPosiljki{Sektor: sektor}) {
		if sad.Sub(p.Slanje.CreatedAt) < NaPutuDuljeOd {
			continue
		}
		pp := PosiljkaNaPutu{Posiljka: p}
		out = append(out, RedakKartice{Ikona: "package", Glavno: "Na putu dulje od 24 h: " + kolicinaSredstva(p.NaPutu(), vrste[p.Slanje.VrstaID], p.Slanje.VrstaID),
			Sporedno: pp.Odakle() + " → " + pp.Kamo() + " · poslano " + danNaslovne(p.Slanje.Datum, sad), URL: "/sredstva/na-putu/" + p.Slanje.VezaID})
	}
	return out
}
