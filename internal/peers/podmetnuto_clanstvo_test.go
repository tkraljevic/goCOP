package peers_test

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// upisiVerziju upisuje n-u u knjigu verziju kakvu bi napisao izmijenjen
// program ili ruka u bazi
func upisiVerziju(t *testing.T, ctx context.Context, n *node, entitet, id string, sadrzaj any) {
	t.Helper()
	tx, err := n.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := n.rec.Record(ctx, tx, entitet, id, sadrzaj); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// mrezaSPrimateljem: A osnuje mrežu, primi U-a (ured-server) i da mu
// ovlast za primanje, pa primi L (laptop-vinkovci) i X (laptop-zupanja).
// Svi znaju za sve, a X je već razmijenio s L.
func mrezaSPrimateljem(t *testing.T, ctx context.Context) (a, u, l, x *node) {
	t.Helper()
	a = startNode(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	u = startNode(t, ctx, "ured-server")
	pair(t, ctx, a, u)
	if _, err := a.svc.IzdajOvlast(ctx, u.id); err != nil {
		t.Fatal(err)
	}
	l = startNode(t, ctx, "laptop-vinkovci")
	pair(t, ctx, a, l)
	x = startNode(t, ctx, "laptop-zupanja")
	pair(t, ctx, a, x)
	for _, n := range []*node{u, l, x} {
		if _, _, err := n.svc.SyncWith(ctx, a.id); err != nil {
			t.Fatalf("%s → A: %v", n.id, err)
		}
	}
	if _, _, err := x.svc.SyncWith(ctx, l.id); err != nil {
		t.Fatalf("X → L: %v", err)
	}
	return a, u, l, x
}

// cekajBiljesku čeka da n zabilježi razmjenu s jednim od zadanih čvorova
// (bilješka se upisuje u svojoj gorutini) i vrati vrijeme zadnje razmjene
// svakog od njih
func cekajBiljesku(t *testing.T, ctx context.Context, n *node, ids ...string) map[string]*time.Time {
	t.Helper()
	zadnja := map[string]*time.Time{}
	for i := 0; i < 200; i++ {
		ima := false
		for _, id := range ids {
			p, err := n.svc.GetPeer(ctx, id)
			if err != nil || p == nil {
				t.Fatalf("%s ne zna %s: %v", n.id, id, err)
			}
			zadnja[id] = p.LastSync
			ima = ima || p.LastSync != nil
		}
		if ima {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return zadnja
}

// Ovlašteni primatelj s izmijenjenim programom potpiše članstvo za ime L
// svojim ključem i pošalje ga X-u, uz zapis L-a sa svojom adresom ispred
// L-ove. X i dalje razmjenjuje s pravim L, L i dalje dolazi do X-a, a kad
// nazove U, X razmjenu bilježi U-u, a ne L-u.
func TestPrimateljNePreuzimaTudjeIme(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	_, u, l, x := mrezaSPrimateljem(t, ctx)

	kljucU, err := razmjena.LoadKey(filepath.Join(u.svc.Node().Dir, peers.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	var ovlast *razmjena.Ovlast
	if svi, _ := u.svc.ListOvlasti(ctx); len(svi) == 1 && svi[0].Valid {
		ovlast = &svi[0].Ovlast
	}
	if ovlast == nil {
		t.Fatal("U nema ovlast za primanje")
	}
	m, err := razmjena.AdmitAs(kljucU, *ovlast, l.id, kljucU.Public().(ed25519.PublicKey), peers.MembershipValidity)
	if err != nil {
		t.Fatal(err)
	}
	upisiVerziju(t, ctx, u, peers.EntityMemberships, l.id, m)
	pravi, _ := x.svc.GetPeer(ctx, l.id)
	upisiVerziju(t, ctx, u, peers.EntityPeers, l.id, peers.Peer{
		NodeID: l.id, Name: pravi.Name, PublicKey: u.svc.Node().PublicKey(),
		Addresses: append([]string{fmt.Sprintf("127.0.0.1:%d", u.svc.Ports().Exchange)}, pravi.Addresses...),
		CreatedAt: pravi.CreatedAt,
	})
	if _, _, err := u.svc.SyncWith(ctx, x.id); err != nil {
		t.Fatalf("U → X: %v", err)
	}

	editStationNote(t, u, "Primjerovo", "s U")
	editStationNote(t, l, "Primjerovo", "s pravog L")
	if _, _, err := x.svc.SyncWith(ctx, l.id); err != nil {
		t.Errorf("X → L: %v", err)
	}
	if got := stationNote(t, x, "Primjerovo"); got != "s pravog L" {
		t.Errorf("X nakon razmjene s L ima napomenu %q", got)
	}
	if _, _, err := l.svc.SyncWith(ctx, x.id); err != nil {
		t.Errorf("L → X: %v", err)
	}

	if _, err := x.db.ExecContext(ctx, `UPDATE peers SET last_sync = NULL WHERE node_id IN (?, ?)`, l.id, u.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := u.svc.SyncWith(ctx, x.id); err != nil {
		t.Fatalf("U → X: %v", err)
	}
	if zadnja := cekajBiljesku(t, ctx, x, l.id, u.id); zadnja[l.id] != nil || zadnja[u.id] == nil {
		t.Errorf("X je razmjenu s U zabilježio L-u (%v) umjesto U-u (%v)", zadnja[l.id], zadnja[u.id])
	}
}

// Primatelj koji za L još ne zna upari novo računalo N pod imenom L. Kad
// N-ovo članstvo stigne X-u, X ne razmjenjuje s N kao s L, a pravi L i
// dalje dolazi do X-a.
func TestDvojnikKojegJePrimioPrimateljNeZauzimaIme(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	a := startNode(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	u := startNode(t, ctx, "ured-server")
	pair(t, ctx, a, u)
	if _, err := a.svc.IzdajOvlast(ctx, u.id); err != nil {
		t.Fatal(err)
	}
	x := startNode(t, ctx, "laptop-zupanja")
	pair(t, ctx, a, x)
	if _, _, err := u.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	l := startNode(t, ctx, "laptop-vinkovci")
	pair(t, ctx, a, l)
	for _, c := range []*node{x, l} {
		if _, _, err := c.svc.SyncWith(ctx, a.id); err != nil {
			t.Fatal(err)
		}
	}
	n := startNode(t, ctx, "laptop-vinkovci")
	pair(t, ctx, u, n)
	if _, _, err := n.svc.SyncWith(ctx, u.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := u.svc.SyncWith(ctx, x.id); err != nil {
		t.Fatalf("U → X: %v", err)
	}

	editStationNote(t, n, "Primjerovo", "s N")
	editStationNote(t, l, "Primjerovo", "s pravog L")
	// N-ov zapis prepisao je L-ove adrese, pa X do L-a možda ne dođe, ali
	// ne smije razmijeniti s N
	_, _, _ = x.svc.SyncWith(ctx, l.id)
	if got := stationNote(t, x, "Primjerovo"); got == "s N" {
		t.Fatal("X je razmijenio s N kao s L")
	}
	if _, _, err := l.svc.SyncWith(ctx, x.id); err != nil {
		t.Errorf("L → X: %v", err)
	}
	if got := stationNote(t, x, "Primjerovo"); got != "s pravog L" {
		t.Errorf("X nakon razmjene s L ima napomenu %q", got)
	}
	if _, _, err := n.svc.SyncWith(ctx, x.id); err == nil || strings.Contains(err.Error(), "nije poznat") {
		t.Errorf("N → X pod imenom L: %v", err)
	}
}

// Obični član C pošalje X-u krivotvorenu verziju L-ova članstva sa svojim
// ključem i lošim potpisom, pod drugom oznakom zapisa. X i L i dalje
// razmjenjuju u oba smjera.
func TestKrivotvorenoClanstvoNeIskljucujeClana(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	a, _, l, x := mrezaSPrimateljem(t, ctx)
	c := startNode(t, ctx, "laptop-baranja")
	pair(t, ctx, a, c)
	for _, n := range []*node{c, x} {
		if _, _, err := n.svc.SyncWith(ctx, a.id); err != nil {
			t.Fatal(err)
		}
	}
	upisiVerziju(t, ctx, c, peers.EntityMemberships, "smece-1", razmjena.Membership{
		Network: a.svc.NetworkInfo().PublicKey, DeviceID: l.id, DeviceKey: c.svc.Node().PublicKey(), IssuedBy: a.id,
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), Signature: "AAAA",
	})
	if _, _, err := c.svc.SyncWith(ctx, x.id); err != nil {
		t.Fatalf("C → X: %v", err)
	}
	for i, smjer := range []func() error{
		func() error { _, _, err := x.svc.SyncWith(ctx, l.id); return err },
		func() error { _, _, err := l.svc.SyncWith(ctx, x.id); return err },
		func() error { _, _, err := x.svc.SyncWith(ctx, l.id); return err },
	} {
		if err := smjer(); err != nil {
			t.Errorf("%d. razmjena X i L nakon krivotvorenog članstva: %v", i+1, err)
		}
	}
}

// Krivotvoreno članstvo i zapis čvora za L s ključem novog računala N ne
// oslobađaju L-ovo ime: osnivač N pod tim imenom ne uparuje
func TestKrivotvorenoClanstvoNeOtvaraImeZaDvojnika(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	a, _, l, _ := mrezaSPrimateljem(t, ctx)
	c := startNode(t, ctx, "laptop-baranja")
	pair(t, ctx, a, c)
	if _, _, err := c.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	n := startNode(t, ctx, "laptop-vinkovci")
	upisiVerziju(t, ctx, c, peers.EntityMemberships, "smece-2", razmjena.Membership{
		Network: a.svc.NetworkInfo().PublicKey, DeviceID: l.id, DeviceKey: n.svc.Node().PublicKey(), IssuedBy: a.id,
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), Signature: "AAAA",
	})
	pravi, _ := c.svc.GetPeer(ctx, l.id)
	upisiVerziju(t, ctx, c, peers.EntityPeers, "smece-3", peers.Peer{
		NodeID: l.id, Name: pravi.Name, PublicKey: n.svc.Node().PublicKey(), Addresses: pravi.Addresses, CreatedAt: pravi.CreatedAt,
	})
	if _, _, err := c.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("C → A: %v", err)
	}
	if p, _ := a.svc.GetPeer(ctx, l.id); p == nil || p.PublicKey != n.svc.Node().PublicKey() {
		t.Fatalf("krivotvoreni zapis L-a nije stigao na A, pa test ništa ne provjerava: %+v", p)
	}
	outA, _ := pokusajUparivanja(t, ctx, a, n)
	if outA.Member || !strings.Contains(outA.Message, "već ima drugo računalo") {
		t.Errorf("A je N primio pod imenom L: %+v", outA)
	}
}
