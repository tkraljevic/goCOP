package peers_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// Uloge jednog računala stignu drugome pravom razmjenom i stoje na ploči
// (Odluke (30. 10. 2026.), 13); mijenjaju se samo na samom računalu.
func TestRazmjenaNosiUloge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	if err := a.svc.PostaviUloge(ctx, peers.Uloge{Izdaje: true, Preuzima: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	if _, pa := statusCvora(t, ctx, b, a.id); !slices.Equal(pa.State.Uloge, []string{razmjena.UlogaIzdaje, razmjena.UlogaPreuzima}) {
		t.Errorf("B o ulogama A: %q", pa.State.Uloge)
	}
	if u := b.svc.TrenutneUloge(); u.Izdaje || u.Preuzima {
		t.Errorf("uloge A ne smiju promijeniti uloge B: %+v", u)
	}
}
