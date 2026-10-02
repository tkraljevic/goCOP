package peers_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/peers"
)

// statusCvora vraća ono što ploča zna o drugom čvoru
func statusCvora(t *testing.T, ctx context.Context, n *node, id string) (*peers.Status, peers.PeerStatus) {
	t.Helper()
	st, err := n.svc.Status(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Peers {
		if p.NodeID == id {
			return st, p
		}
	}
	t.Fatalf("%s ne zna za %s", n.id, id)
	return nil, peers.PeerStatus{}
}

// Izdanje programa putuje u razmjeni u oba smjera: onaj tko nazove i onaj
// tko prima znaju na čemu drugi radi, a ploča upozori na razliku. Čvor koji
// izdanje ne javlja (stariji program) vidi se kao stariji.
func TestRazmjenaNosiInacicuPrograma(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	b := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)

	a.svc.PostaviProgram("0.0.24-alfa", "0.0.24-alfa (abc1234)")
	b.svc.PostaviProgram("0.0.23-alfa", "0.0.23-alfa (def5678)")
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}

	// B je nazvao: zna A-ovo izdanje iz odgovora
	stB, pa := statusCvora(t, ctx, b, a.id)
	if pa.Program != "0.0.24-alfa" || pa.Gradnja != "0.0.24-alfa (abc1234)" || !pa.RazlicitProgram || stB.Program != "0.0.23-alfa" {
		t.Fatalf("B o A: %+v (svoj %q)", pa, stB.Program)
	}
	// A je primio: bilješka nastaje nakon razgovora, na strani posluživanja
	var pb peers.PeerStatus
	var stA *peers.Status
	for i := 0; i < 50; i++ {
		if stA, pb = statusCvora(t, ctx, a, b.id); pb.Program != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pb.Program != "0.0.23-alfa" || pb.Gradnja != "0.0.23-alfa (def5678)" || !pb.RazlicitProgram {
		t.Fatalf("A o B: %+v", pb)
	}
	if !strings.Contains(strings.Join(stA.Alerts, "\n"), "radi na 0.0.23-alfa, ovaj na 0.0.24-alfa") {
		t.Errorf("A ne upozorava na stariji B: %v", stA.Alerts)
	}

	// isto izdanje: bez razlike i bez upozorenja
	b.svc.PostaviProgram("0.0.24-alfa", "0.0.24-alfa (abc1234)")
	if _, _, err := a.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("razmjena A→B: %v", err)
	}
	stA, pb = statusCvora(t, ctx, a, b.id)
	if pb.Program != "0.0.24-alfa" || pb.RazlicitProgram {
		t.Fatalf("A o B nakon ažuriranja: %+v", pb)
	}
	if strings.Contains(strings.Join(stA.Alerts, "\n"), "ažurirajte") {
		t.Errorf("suvišno upozorenje: %v", stA.Alerts)
	}

	// B bez izdanja glumi stariji program: ne javlja ga, a razmjena uspije
	b.svc.PostaviProgram("", "")
	if _, _, err := a.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("razmjena sa starijim: %v", err)
	}
	stA, pb = statusCvora(t, ctx, a, b.id)
	if pb.Program != "" || !pb.RazlicitProgram {
		t.Fatalf("A o starijem B: %+v", pb)
	}
	if !strings.Contains(strings.Join(stA.Alerts, "\n"), "starijoj od 0.0.24-alfa (ne javlja verziju)") {
		t.Errorf("A ne upozorava na B bez izdanja: %v", stA.Alerts)
	}
}
