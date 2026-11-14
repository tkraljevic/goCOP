package web

import (
	"context"
	"time"

	"gocop/internal/models"
)

// Vrh stranice kišomjera (goCOP-m3, 17e · Registri, 6e): oborine zadnjih sedam
// dana u stupcima i broj drugih točaka u istom međuslivu.

// DanKise je dnevni zbroj jednog dana; Mm je nil kad za taj dan nema podatka,
// da se ne pomiješa s danom bez kiše.
type DanKise struct {
	Dan time.Time
	Mm  *float64
}

// sedamDana slaže zadnjih sedam dana, do danas, iz dnevnih zbrojeva koje
// stranica već računa; kod pogleda od 30 ili 90 dana uzima se samo zadnjih sedam.
func sedamDana(dnevni []RedKise, sada time.Time) []DanKise {
	poDanu := map[string]float64{}
	for _, d := range dnevni {
		poDanu[d.Kad.UTC().Format("2006-01-02")] += d.Mm
	}
	danas := sada.In(models.Zagreb)
	out := make([]DanKise, 0, 7)
	for i := 6; i >= 0; i-- {
		dan := time.Date(danas.Year(), danas.Month(), danas.Day()-i, 0, 0, 0, 0, time.UTC)
		d := DanKise{Dan: dan}
		if mm, ima := poDanu[dan.Format("2006-01-02")]; ima {
			d.Mm = &mm
		}
		out = append(out, d)
	}
	return out
}

// zbrojSedam je zbroj dana koji imaju podatak
func zbrojSedam(dani []DanKise) float64 {
	z := 0.0
	for _, d := range dani {
		if d.Mm != nil {
			z += *d.Mm
		}
	}
	return z
}

// drugiUMedjuslivu broji ostale točke istog međusliva
func (h *KisomjerStranicaHandler) drugiUMedjuslivu(ctx context.Context, t models.Kisomjer) int {
	svc := h.svc()
	if svc == nil || t.Sliv == "" {
		return 0
	}
	sve, err := svc.ListKisomjeri(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, k := range sve {
		if k.Sliv == t.Sliv && k.Code != t.Code {
			n++
		}
	}
	return n
}
