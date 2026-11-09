package service

import (
	"context"
	"testing"

	"gocop/internal/models"
)

// Sektor je u preventivnoj obrani kad nema otvorenog dnevnika COP-a. Bez
// pravila aktivne obrane, bez sektora i bez servisa nije, kao u trebaAktivnu,
// pa se obrazac akta i dalje nudi (Odluke (6. 11. 2026.)).
func TestUPreventivnoj(t *testing.T) {
	ctx := context.Background()
	otvoren := map[string]bool{"A": true}
	s := &AktService{}
	if s.UPreventivnoj(ctx, "B") {
		t.Error("bez pravila aktivne obrane sektor nije u preventivnoj")
	}
	s.aktivna = func(_ context.Context, sektor string) *models.Journal {
		if otvoren[sektor] {
			return &models.Journal{CentarSektor: sektor}
		}
		return nil
	}
	for _, x := range []struct {
		sektor string
		zeli   bool
	}{{"A", false}, {"B", true}, {"", false}} {
		if got := s.UPreventivnoj(ctx, x.sektor); got != x.zeli {
			t.Errorf("sektor %q: %v, želi %v", x.sektor, got, x.zeli)
		}
	}
	var nema *AktService
	if nema.UPreventivnoj(ctx, "B") {
		t.Error("bez servisa sektor nije u preventivnoj")
	}
}
