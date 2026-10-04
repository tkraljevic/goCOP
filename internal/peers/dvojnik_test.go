package peers_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/repository"
)

// startCvor je startNode bez registara iz data/: uparivanje ih ne treba
func startCvor(t *testing.T, ctx context.Context, id string) *node {
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
	svc.OnApplied(func(ctx context.Context, versions []ledger.Version) error {
		return repository.ApplyVersions(ctx, database, rec, versions)
	})
	go svc.Serve(ctx)
	return &node{id: id, db: database, svc: svc, rec: rec}
}

// pokusajUparivanja upari dva čvora i vrati ishode obje strane, bez
// zahtjeva da uspije
func pokusajUparivanja(t *testing.T, ctx context.Context, a, b *node) (peers.PairOutcome, peers.PairOutcome) {
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
	gotov := make(chan peers.PairOutcome, 1)
	go func() { out, _ := a.svc.ConfirmPair(ctx, true, true); gotov <- out }()
	outB, _ := b.svc.ConfirmPair(ctx, true, true)
	return <-gotov, outB
}

// Dva računala istog imena ne smiju se sresti: drugo bi tiho prepisalo ključ
// prvoga na svim čvorovima i miješalo zapise u knjizi
func TestDvojnikImenaSeOdbija(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startCvor(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	b := startCvor(t, ctx, "pperic-thinkpad")
	pair(t, ctx, a, b)
	kljucB, err := a.svc.GetPeer(ctx, "pperic-thinkpad")
	if err != nil || kljucB == nil {
		t.Fatalf("B nije poznat: %v", err)
	}

	// drugo računalo s istim imenom kao B
	c := startCvor(t, ctx, "pperic-thinkpad")
	outA, _ := pokusajUparivanja(t, ctx, a, c)
	if outA.Member || !strings.Contains(outA.Message, "već ima drugo računalo") {
		t.Fatalf("dvojnik B-a: %+v", outA)
	}
	if p, _ := a.svc.GetPeer(ctx, "pperic-thinkpad"); p == nil || p.PublicKey != kljucB.PublicKey {
		t.Fatal("dvojnik je prepisao ključ B-a")
	}
	if c.svc.NetworkInfo() != nil {
		t.Error("dvojnik je primljen u mrežu")
	}

	// računalo s imenom samog A
	d := startCvor(t, ctx, "cop-osijek")
	outA, _ = pokusajUparivanja(t, ctx, a, d)
	if outA.Member || !strings.Contains(outA.Message, "isto ime kao ovo") {
		t.Fatalf("dvojnik A-a: %+v", outA)
	}

	// zaboravljen i opozvan B: novo računalo tog imena smije ući
	if _, err := a.svc.RevokeMembership(ctx, "pperic-thinkpad"); err != nil {
		t.Fatal(err)
	}
	if err := a.svc.ForgetPeer(ctx, "pperic-thinkpad"); err != nil {
		t.Fatal(err)
	}
	e := startCvor(t, ctx, "pperic-thinkpad")
	if outA, _ = pokusajUparivanja(t, ctx, a, e); !outA.Member {
		t.Fatalf("nakon zaboravljanja novo računalo istog imena: %+v", outA)
	}

	// SavePeer ni izravno ne prepisuje ključ poznatog čvora
	p, _ := a.svc.GetPeer(ctx, "pperic-thinkpad")
	p.PublicKey = kljucB.PublicKey
	if err := a.svc.SavePeer(ctx, *p); err == nil {
		t.Error("SavePeer je prepisao ključ poznatog čvora")
	}
}

// Nazivanje radi uparivanja: dok jedno čeka odluku, drugo se odbija; adresa
// bez porta dobiva port uparivanja; čvor koji ne sluša javlja grešku
func TestNazivanjeRadiUparivanja(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startCvor(t, ctx, "cop-osijek")
	b := startCvor(t, ctx, "pperic-thinkpad")

	if err := b.svc.DialPair(ctx, "127.0.0.1"); err == nil {
		t.Error("nazivanje adrese na kojoj nitko ne sluša (port uparivanja) je prošlo")
	}
	if err := b.svc.DialPair(ctx, fmt.Sprintf("127.0.0.1:%d", freePort(t))); err == nil {
		t.Error("nazivanje zatvorenog porta je prošlo")
	}
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
		t.Fatal(err)
	}
	if err := b.svc.DialPair(ctx, fmt.Sprintf("127.0.0.1:%d", a.svc.Ports().Pair)); err == nil || !strings.Contains(err.Error(), "već čeka") {
		t.Errorf("drugo nazivanje dok prvo čeka odluku: %v", err)
	}
}
