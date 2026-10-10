package web

import (
	"fmt"

	"gocop/internal/models"
)

// Sektori i branjena područja koji letvu prate (Odluke (8. 10. 2026.),
// pitanje 8): obrazac letve ih nudi kvačicama, sektor po sektor s njegovim
// područjima, a kartica letve ih pokazuje nazivima.

// SektorPracenja je jedan sektor u obrascu letve, s njegovim područjima
type SektorPracenja struct {
	Sektor   models.Sector
	Podrucja []models.Area
}

// SetUstroj daje rukovatelju sektore i branjena područja iz registra
func (h *StationsHandler) SetUstroj(f func() ([]models.Sector, []models.Area)) { h.ustroj = f }

// sektoriIPodrucja su sektori i područja iz registra; bez dohvatnika ništa
func (h *StationsHandler) sektoriIPodrucja() ([]models.Sector, []models.Area) {
	if h.ustroj == nil {
		return nil, nil
	}
	return h.ustroj()
}

// ustroj su sektori i branjena područja iz registra; greška čitanja daje
// prazan izbor, a obrazac letve i dalje radi
func (s *Server) ustroj() ([]models.Sector, []models.Area) {
	if s.userService == nil {
		return nil, nil
	}
	sektori, _ := s.userService.ListSectors()
	podrucja, _ := s.userService.ListAreas("")
	return sektori, podrucja
}

// izborPracenja slaže sektore s njihovim područjima, redom iz registra
func izborPracenja(sektori []models.Sector, podrucja []models.Area) []SektorPracenja {
	out := make([]SektorPracenja, 0, len(sektori))
	for _, s := range sektori {
		sp := SektorPracenja{Sektor: s}
		for _, a := range podrucja {
			if a.SectorID == s.ID {
				sp.Podrucja = append(sp.Podrucja, a)
			}
		}
		out = append(out, sp)
	}
	return out
}

// pracenjeLetve su nazivi sektora i područja koji letvu prate
func (h *StationsHandler) pracenjeLetve(st models.Station) []string {
	sektori, podrucja := h.sektoriIPodrucja()
	return opisPracenja(st, sektori, podrucja)
}

// opisPracenja su nazivi sektora i područja koji letvu prate, za karticu;
// šifra koje nema u registru ostaje kakva je upisana
func opisPracenja(st models.Station, sektori []models.Sector, podrucja []models.Area) []string {
	var out []string
	for _, id := range st.PrateSektori {
		out = append(out, nazivSektora(id, sektori))
	}
	for _, id := range st.PratePodrucja {
		out = append(out, nazivPodrucja(id, podrucja))
	}
	return out
}

func nazivSektora(id string, sektori []models.Sector) string {
	for _, s := range sektori {
		if s.ID == id && s.Name != "" {
			return s.Name
		}
	}
	return "Sektor " + id
}

func nazivPodrucja(id int, podrucja []models.Area) string {
	for _, a := range podrucja {
		if a.ID == id && a.Name != "" {
			return fmt.Sprintf("BP %d %s", id, a.Name)
		}
	}
	return fmt.Sprintf("BP %d", id)
}
