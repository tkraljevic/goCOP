package web

import (
	"strings"

	"gocop/internal/models"
)

// registriObjekataUIzborniku su registri objekata sa svojom stavkom u
// izborniku Registri (nasipi, crpne stanice, ustave, brane)
func registriObjekataUIzborniku() []models.StructureRegister {
	var out []models.StructureRegister
	for _, r := range models.StructureRegisters {
		if r.InMenu {
			out = append(out, r)
		}
	}
	return out
}

// navObjekta je oznaka aktivne stavke izbornika za registar vrste
func navObjekta(kind string) string { return "objekti-" + kind }

// jeNavObjekta javlja je li stranica u nekom od registara objekata
func jeNavObjekta(nav string) bool { return nav == "structures" || strings.HasPrefix(nav, "objekti-") }

// rutaRegistaraObjekata daje svakoj vrsti objekta svoju adresu popisa
func (s *Server) rutaRegistaraObjekata(h *StructuresHandler) {
	for _, reg := range models.StructureRegisters {
		s.mux.Handle("GET "+reg.Path, s.authMiddleware(h.ShowRegister(reg)))
	}
}
