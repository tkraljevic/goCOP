package web

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"strings"

	geom "gocop/internal/geometrija"
	"gocop/internal/service"
)

// Granice sektora i branjenih područja podloga su Hrvatskih voda (shapefile
// u HTRS96/TM, pretvoren u WGS84 i pojednostavljen na oko 25 m). Stoje u
// data/geometrija, izvan repozitorija, kao i granice naselja; ovdje im se
// dodaje ono što vodi aplikacija — naziv područja, ispostava, podcentar i
// broj dionica — da oznaka na karti vodi na dionice.

// geometrijaVodaDir je mapa s pretvorenim granicama, u odnosu na radnu mapu
// poslužitelja
const geometrijaVodaDir = "data/geometrija"

// SetVodnaPodrucja daje rukovatelju sektore, područja i dionice za oznake karte
func (h *TerritoriesHandler) SetVodnaPodrucja(users *service.UserService, sections *service.SectionService) {
	h.userService = users
	h.sectionService = sections
}

// HandleGetSektoriGeoJSON vraća granice sektora
func (h *TerritoriesHandler) HandleGetSektoriGeoJSON(w http.ResponseWriter, r *http.Request) {
	h.vodnaGeoJSON(w, r, "sektori", h.dodaciSektora())
}

// HandleGetBranjenaPodrucjaGeoJSON vraća granice branjenih područja
func (h *TerritoriesHandler) HandleGetBranjenaPodrucjaGeoJSON(w http.ResponseWriter, r *http.Request) {
	h.vodnaGeoJSON(w, r, "branjena-podrucja", h.dodaciPodrucja())
}

func (h *TerritoriesHandler) vodnaGeoJSON(w http.ResponseWriter, r *http.Request, sifra string, dodaci map[string]map[string]any) {
	raw, err := geom.Ucitaj(geometrijaVodaDir, sifra)
	if err != nil || len(raw) == 0 {
		http.Error(w, "Granice nisu učitane: nema datoteke "+geometrijaVodaDir+"/"+sifra+".geojson", http.StatusNotFound)
		return
	}
	raw = dopuniGeoJSON(raw, dodaci)
	w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(raw); err == nil && gw.Close() == nil {
			w.Header().Set("Content-Encoding", "gzip")
			w.Write(buf.Bytes())
			return
		}
	}
	w.Write(raw)
}

// brojDionica broji dionice po području i po sektoru
func (h *TerritoriesHandler) brojDionica() (poPodrucju map[int]int, poSektoru map[string]int) {
	poPodrucju, poSektoru = map[int]int{}, map[string]int{}
	if h.sectionService == nil {
		return
	}
	if secs, err := h.sectionService.ListSections("", 0, ""); err == nil {
		for _, s := range secs {
			poPodrucju[s.AreaID]++
			poSektoru[s.SectorID]++
		}
	}
	return
}

func (h *TerritoriesHandler) dodaciPodrucja() map[string]map[string]any {
	out := map[string]map[string]any{}
	if h.userService == nil {
		return out
	}
	areas, err := h.userService.ListAreas("")
	if err != nil {
		return out
	}
	poPodrucju, _ := h.brojDionica()
	for _, a := range areas {
		out[fmt.Sprint(a.ID)] = map[string]any{
			"ime":       a.Name,
			"vgi":       a.VgiName,
			"podcentar": a.Subcenter,
			"dionica":   poPodrucju[a.ID],
			"url":       fmt.Sprintf("/sections?area=%d", a.ID),
		}
	}
	return out
}

func (h *TerritoriesHandler) dodaciSektora() map[string]map[string]any {
	out := map[string]map[string]any{}
	if h.userService == nil {
		return out
	}
	sektori, err := h.userService.ListSectors()
	if err != nil {
		return out
	}
	_, poSektoru := h.brojDionica()
	for _, s := range sektori {
		out[s.ID] = map[string]any{
			"ime":     s.Name,
			"cop":     s.CenterCop,
			"dionica": poSektoru[s.ID],
			"url":     "/sections?sector=" + s.ID,
		}
	}
	return out
}
