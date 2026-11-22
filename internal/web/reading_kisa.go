package web

import (
	"context"

	"gocop/internal/models"
	"gocop/internal/service"
)

// SetKisomjeri daje rukovatelju registar kišomjera: na letvi uz koju stoji
// kišomjer obrazac očitanja nudi i kišu.
func (h *ReadingsHandler) SetKisomjeri(f func() *service.KisomjerService) { h.kisomjeri = f }

// kisomjerLetve je stvarni kišomjer uz letvu; nil uz objekt, bez registra
// kišomjera ili kad uz letvu kišomjera nema.
func (h *ReadingsHandler) kisomjerLetve(ctx context.Context, st *models.Station) *models.Kisomjer {
	if h.kisomjeri == nil || st == nil {
		return nil
	}
	return h.kisomjeri().KisomjerUzLetvu(ctx, st.Code)
}
