package peers_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/peers"
)

// prazanCvor je čvor bez registara: za uparivanje i članstvo ne trebaju, pa
// test radi i bez data/sections.json
func prazanCvor(t *testing.T, ctx context.Context, id string) *node {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "gocop.db")
	database, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(database, id)
	n, err := peers.LoadNode(dbPath, id, "test-"+id, "test")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := peers.NewService(database, rec, n, peers.Ports{Exchange: freePort(t), Pair: freePort(t)})
	if err != nil {
		t.Fatal(err)
	}
	go svc.Serve(ctx)
	return &node{id: id, db: database, svc: svc, rec: rec}
}

// uparenUz upari b s a; a potvrđuje s ovlašću primi, b uvijek s punom
func uparenUz(t *testing.T, ctx context.Context, a, b *node, primi bool) (outA, outB peers.PairOutcome) {
	t.Helper()
	if err := a.svc.StartListening(); err != nil {
		t.Fatal(err)
	}
	var err error
	for i := 0; i < 50; i++ {
		if err = b.svc.DialPair(ctx, fmt.Sprintf("127.0.0.1:%d", a.svc.Ports().Pair)); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("nazivanje: %v", err)
	}
	for i := 0; i < 50 && !a.svc.PairStatus().Pending; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	done := make(chan peers.PairOutcome, 1)
	go func() {
		out, err := a.svc.ConfirmPair(ctx, true, primi)
		if err != nil {
			t.Errorf("potvrda na %s: %v", a.id, err)
		}
		done <- out
	}()
	outB, err = b.svc.ConfirmPair(ctx, true, true)
	if err != nil {
		t.Fatalf("potvrda na %s: %v", b.id, err)
	}
	return <-done, outB
}

// Nositelj ključa mreže prima drugi čvor samo kad potvrđuje ovlašten čovjek
// (administrator ili izravan klijent svježeg čvora). Neovlaštena potvrda
// čvorove upari, ali potvrdu članstva ne izda.
func TestNeovlastenaPotvrdaNePrimaUMrezu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := prazanCvor(t, ctx, "cop-osijek")
	b := prazanCvor(t, ctx, "laptop-nepoznat")
	c := prazanCvor(t, ctx, "laptop-vinkovci")
	if err := a.svc.CreateNetwork(ctx, "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}

	outA, outB := uparenUz(t, ctx, a, b, false)
	if !outA.Paired || outA.Member || outB.Member {
		t.Errorf("bez ovlasti: čvorovi upareni, ali drugi nije član; A=%+v B=%+v", outA, outB)
	}
	if b.svc.NetworkInfo() != nil {
		t.Error("čvor primljen u mrežu bez ovlaštene potvrde")
	}

	// kontrola: ovlaštena potvrda prima
	outA, outC := uparenUz(t, ctx, a, c, true)
	if !outA.Member || !outC.Member || c.svc.NetworkInfo() == nil {
		t.Errorf("ovlaštena potvrda mora primiti čvor u mrežu; A=%+v C=%+v", outA, outC)
	}
}
