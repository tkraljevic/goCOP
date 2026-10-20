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
//
// Namjerno promijenjeno: G4 (plan WEB synca, §3). Čvor javlja i
// granica-odbijenih/1, pa razgovor dvaju novih čvorova nosi i granicu
// odbijenih; do G4 javljao je samo razmjena/1. Namjerno promijenjeno: G4b;
// čvor javlja i rupe/1, pa razgovor dvaju novih čvorova nosi i poruku rupa.
func TestRazmjenaNosiZnanje(t *testing.T) {
	oznake := []string{"granica-odbijenih/1", "razmjena/1", "rupe/1"}
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
	if !slices.Equal(pa.State.Mogucnosti, oznake) || !pa.State.MogucnostiPoznate || pa.State.CekaNadogradnju != 0 {
		t.Fatalf("B o A: %+v", pa.State)
	}
	// A je primio: bilješka nastaje nakon razgovora, na strani posluživanja
	var pb peers.PeerStatus
	for i := 0; i < 100 && !pb.State.MogucnostiPoznate; i++ {
		if _, pb = statusCvora(t, ctx, a, b.id); !pb.State.MogucnostiPoznate {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !slices.Equal(pb.State.Mogucnosti, oznake) || !pb.State.MogucnostiPoznate {
		t.Fatalf("A o B: %+v", pb.State)
	}
	ploca, err := json.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ploca), `"mogucnosti":["granica-odbijenih/1","razmjena/1","rupe/1"]`) {
		t.Errorf("ploča: %s", ploca)
	}
}
