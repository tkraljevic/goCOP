package web

import (
	"strings"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// zapisiModela su zapisi modela po letvi; prazno kad baze prognoza nema.
func (h *PrognozeHandler) zapisiModela() map[string]prognoza.ZapisLetve {
	if h.citac == nil {
		return nil
	}
	return h.citac().ZapisiModela()
}

// opisDnevnogZapisa kaže iz čega dnevni model iz zapisa računa letvu:
// „dnevni model: Bračevci i kiša”. Prazno kad letva nema takav zapis.
func opisDnevnogZapisa(z prognoza.ZapisLetve, popis map[string]models.Station) string {
	d, err := z.DnevniBlok()
	if err != nil || z.Metoda != prognoza.MetodaDnevni {
		return ""
	}
	var dijelovi []string
	for _, u := range d.Ulazi {
		dijelovi = append(dijelovi, imeLetve(popis, u))
	}
	if len(d.Slivovi) > 0 {
		dijelovi = append(dijelovi, "kiša")
	}
	if len(dijelovi) == 1 {
		return "dnevni model: " + dijelovi[0]
	}
	return "dnevni model: " + strings.Join(dijelovi[:len(dijelovi)-1], ", ") + " i " + dijelovi[len(dijelovi)-1]
}
