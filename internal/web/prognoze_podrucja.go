package web

import (
	"context"
	"slices"

	"gocop/internal/models"
)

// PodrucjeModela je redak kartice „Model po branjenim područjima” na
// postavkama prognoze: branjeno područje na kojem je barem jedna letva iz
// izdanja. Uređivanje modela po područjima je u izradi, pa redak samo kaže
// koje područje prognoza pokriva.
type PodrucjeModela struct {
	Broj  int    // broj branjenog područja (BP n)
	Naziv string // ime iz registra područja
}

// zadnjeIzdanje vraća kad je izdana zadnja prognoza (za zaglavlje postavki)
// i njezine letve; prazno kad baze prognoza ili izdanja nema.
func (h *PrognozeHandler) zadnjeIzdanje() (string, []PregledLetve) {
	if h.citac == nil {
		return "", nil
	}
	izdano, letve, err := h.citac().Pregled()
	if err != nil || izdano.IsZero() {
		return "", nil
	}
	return izdano.In(models.Zagreb).Format("2.1.2006. u 15:04"), letve
}

// podrucjaPrognoze čita registar područja i letvi i slaže popis branjenih
// područja s letvama iz izdanja; nil kad registra nema.
func (h *PrognozeHandler) podrucjaPrognoze(ctx context.Context, letve []PregledLetve) []PodrucjeModela {
	if h.users == nil || len(letve) == 0 {
		return nil
	}
	podrucja, err := h.users.ListAreas("")
	if err != nil {
		return nil
	}
	var dionice map[string][]int
	if h.stations != nil {
		_, dionice, _ = h.stations.StationScopes(ctx) // bez dionica ostaju područja upisana na letvi
	}
	return podrucjaModela(letve, h.postaje(ctx), dionice, podrucja)
}

// podrucjaModela su branjena područja kojima letve iz izdanja pripadaju:
// preko svojih dionica (dionice: ID postaje → područja) i preko područja
// upisanih u obrazac letve. Poredana po broju područja.
func podrucjaModela(letve []PregledLetve, postaje map[string]models.Station, dionice map[string][]int, podrucja []models.Area) []PodrucjeModela {
	ima := map[int]bool{}
	for _, l := range letve {
		st, postoji := postaje[l.Letva]
		if !postoji {
			continue
		}
		for _, id := range append(slices.Clone(dionice[st.ID.String()]), st.PratePodrucja...) {
			ima[id] = true
		}
	}
	var out []PodrucjeModela
	for _, a := range podrucja {
		if ima[a.ID] {
			out = append(out, PodrucjeModela{Broj: a.ID, Naziv: a.Name})
		}
	}
	slices.SortFunc(out, func(a, b PodrucjeModela) int { return a.Broj - b.Broj })
	return out
}
