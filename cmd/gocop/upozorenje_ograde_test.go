package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
)

// Upozorenje ograde (Odluke (17. 10. 2026.), 1A): samo uz uključenu ogradu i
// samo za računala bez oznake rupe/1 ili s još nepoznatim mogućnostima,
// imenom kad ga ima
func TestUpozorenjeOgrade(t *testing.T) {
	novo := peers.PeerStatus{Peer: peers.Peer{NodeID: "cop-osijek", Name: "Unraid"}, State: peers.SyncState{Mogucnosti: razmjena.NaseMogucnosti(), MogucnostiPoznate: true}}
	staro := peers.PeerStatus{Peer: peers.Peer{NodeID: "windows-ured", Name: "Kućni Windows"}, State: peers.SyncState{Mogucnosti: []string{"razmjena/1"}, MogucnostiPoznate: true}}
	nepoznato := peers.PeerStatus{Peer: peers.Peer{NodeID: "laptop-ana"}}
	if got := upozorenjeOgrade(0, []peers.PeerStatus{staro}); got != nil {
		t.Errorf("bez ograde: %q", got)
	}
	if got := upozorenjeOgrade(12, []peers.PeerStatus{novo}); got != nil {
		t.Errorf("svi novi: %q", got)
	}
	jedno := upozorenjeOgrade(1, []peers.PeerStatus{novo, staro})
	if len(jedno) != 1 || jedno[0] != "Ovo računalo ne čuva tuđa očitanja starija od 1 mjeseca, a računalo Kućni Windows ima stariji program koji to ne zna, "+
		"pa bi stara očitanja moglo izgubiti. Dok se Kućni Windows ne nadogradi, isključite ograničenje povijesti (povijest_mjeseci) "+
		"u postavkama ovog računala." {
		t.Errorf("jedno računalo: %q", jedno)
	}
	tri := upozorenjeOgrade(12, []peers.PeerStatus{staro, novo, nepoznato, {Peer: peers.Peer{NodeID: "laptop-ivo"}}})
	if len(tri) != 1 || !strings.HasPrefix(tri[0], "Ovo računalo ne čuva tuđa očitanja starija od 12 mjeseci, a računala Kućni Windows, laptop-ana i laptop-ivo imaju stariji program koji to ne zna, pa bi stara očitanja mogla izgubiti.") ||
		!strings.HasSuffix(tri[0], "Dok se ne nadograde, isključite ograničenje povijesti (povijest_mjeseci) u postavkama ovog računala.") {
		t.Errorf("više računala: %q", tri)
	}
}

// Pločica nosi upozorenje ograde i ono što se predugo traži
func TestStanjeRazmjeneRupe(t *testing.T) {
	ctx := context.Background()
	baza, rec, mreza, _ := cvorPlocice(t)
	if err := mreza.CreateNetwork(ctx, "Proba"); err != nil {
		t.Fatal(err)
	}
	repository.SetReadingHistoryPolicy(repository.ReadingHistoryPolicy{Months: 12})
	t.Cleanup(func() { repository.SetReadingHistoryPolicy(repository.ReadingHistoryPolicy{}) })
	sad := time.Now().UTC()
	if _, err := baza.Exec(`INSERT INTO peers (node_id, name, public_key, created_at) VALUES ('laptop-ana', '', 'k1', ?)`, sad); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO rupe_knjige (node_id, channel, trazi_od, trazi_do, trazi_nastalo) VALUES ('laptop-ana', 'ocitanja/16/2024', '', 'v-1', ?)`,
		sad.Add(-48*time.Hour).Truncate(time.Second)); err != nil {
		t.Fatal(err)
	}
	st := stanjeRazmjene(ctx, mreza, rec, nil, nil, nil, nil)
	var ograda, nedostaje bool
	for _, u := range st.Upozorenja {
		ograda = ograda || strings.Contains(u, "a računalo laptop-ana ima stariji program")
		nedostaje = nedostaje || strings.HasPrefix(u, "Od računala laptop-ana možda nedostaju neka starija očitanja, jer su stigla preko računala koje ne čuva sve.")
	}
	if !ograda || !nedostaje {
		t.Errorf("upozorenja: %q", st.Upozorenja)
	}
}
