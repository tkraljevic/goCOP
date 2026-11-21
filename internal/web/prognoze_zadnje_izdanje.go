package web

import "gocop/internal/models"

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
