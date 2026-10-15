package peers_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/peers"
)

// Na početku razgovora oba čvora kažu što znaju, a druga strana to zapamti
// uz stanje razmjene i pokaže na ploči: i onaj tko je nazvao i onaj tko je
// primio. Da ploča ne šalje vrste zapisa, provjerava TestZapamcenoZnanje.
func TestRazmjenaNosiZnanje(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}

	_, pa := statusCvora(t, ctx, b, a.id)
	if !slices.Equal(pa.State.Mogucnosti, []string{"razmjena/1"}) || !pa.State.MogucnostiPoznate || pa.State.CekaNadogradnju != 0 {
		t.Fatalf("B o A: %+v", pa.State)
	}
	// A je primio: bilješka nastaje nakon razgovora, na strani posluživanja
	var pb peers.PeerStatus
	for i := 0; i < 100 && !pb.State.MogucnostiPoznate; i++ {
		if _, pb = statusCvora(t, ctx, a, b.id); !pb.State.MogucnostiPoznate {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !slices.Equal(pb.State.Mogucnosti, []string{"razmjena/1"}) || !pb.State.MogucnostiPoznate {
		t.Fatalf("A o B: %+v", pb.State)
	}
	ploca, err := json.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ploca), `"mogucnosti":["razmjena/1"]`) {
		t.Errorf("ploča: %s", ploca)
	}
}
