package peers_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gocop/internal/peers"
)

// podmetniZapisCvora: A, B i C su članovi iste mreže. C u svoju knjigu
// upiše zapis čvora B sa svojim ključem i svojom adresom ispred B-ove (kako
// bi to napravio izmijenjen program ili ruka u bazi: obična verzija zapisa
// čvora, autor C) i preda ga A razmjenom. A taj zapis primijeni kao svaki
// drugi, pa mu B u popisu računala nosi C-ov ključ.
func podmetniZapisCvora(t *testing.T, ctx context.Context) (a, b, c *node) {
	t.Helper()
	a = startNode(t, ctx, "cop-osijek")
	b = startNode(t, ctx, "laptop-vinkovci")
	c = startNode(t, ctx, "laptop-zupanja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	pair(t, ctx, a, c)

	pravi, err := a.svc.GetPeer(ctx, b.id)
	if err != nil || pravi == nil || pravi.PublicKey != b.svc.Node().PublicKey() || len(pravi.Addresses) == 0 {
		t.Fatalf("A mora znati B s njegovim ključem i adresom: %+v %v", pravi, err)
	}
	podmetnut := peers.Peer{
		NodeID:    b.id,
		Name:      pravi.Name,
		PublicKey: c.svc.Node().PublicKey(),
		Addresses: append([]string{fmt.Sprintf("127.0.0.1:%d", c.svc.Ports().Exchange)}, pravi.Addresses...),
		CreatedAt: pravi.CreatedAt,
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.rec.Record(ctx, tx, peers.EntityPeers, b.id, podmetnut); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// čeka i A-ovu bilješku o C-u: test koji zatim briše A-ove bilješke inače
	// dobije ovu nakon brisanja, kao da je stigla s njegove razmjene
	if err := nazoviICekajBiljesku(t, ctx, c, a); err != nil {
		t.Fatalf("razmjena C → A: %v", err)
	}
	if p, _ := a.svc.GetPeer(ctx, b.id); p == nil || p.PublicKey != podmetnut.PublicKey || p.Addresses[0] != podmetnut.Addresses[0] {
		t.Fatalf("podmetnuti zapis nije stigao na A, pa test ništa ne provjerava: %+v", p)
	}
	return a, b, c
}

// Član mreže ne smije drugom računalu podmetnuti ključ drugog člana. A pri
// razmjeni s B i dalje mora tražiti B-ov ključ iz članstva: C-ova adresa
// odbije se na ključu, a podaci stignu s pravog B (plan WEB synca,
// „Ispravak zapisa čvora”).
func TestPodmetnutiKljucUZapisuCvora(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, b, c := podmetniZapisCvora(t, ctx)

	// i C i B zatim promijene istu postaju; B-ova izmjena je novija, a
	// nijedna od njih nije na A
	editStationNote(t, c, "Primjerovo", "napomena s C")
	editStationNote(t, b, "Primjerovo", "napomena s pravog B")

	if _, _, err := a.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("razmjena A → B mora doći do pravog B na njegovoj adresi: %v", err)
	}
	switch got := stationNote(t, a, "Primjerovo"); got {
	case "napomena s pravog B":
	case "napomena s C":
		t.Errorf("A je razmijenio s C kao da je B: očekivani ključ uzet je iz podmetnutog zapisa čvora, a ne iz članstva B-a")
	default:
		t.Errorf("A nakon razmjene s B ima napomenu %q", got)
	}
}

// Kad C nazove A, A razmjenu bilježi C-u, a ne B-u, iako podmetnuti zapis
// B-a nosi C-ov ključ. Inače bi bilješke o razmjeni i ono što C javi o sebi
// (Tailscale adrese, inačica, mogućnosti) A pamtio pod B.
func TestDolaznaRazmjenaIdeOnomeKoJeDokazaoKljuc(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, b, c := podmetniZapisCvora(t, ctx)
	if _, err := a.db.ExecContext(ctx, `UPDATE peers SET last_sync = NULL, last_sync_note = '' WHERE node_id IN (?, ?)`, b.id, c.id); err != nil {
		t.Fatal(err)
	}

	if _, _, err := c.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena C → A: %v", err)
	}
	// A bilješku upisuje u svojoj gorutini, nakon razgovora
	var naB, naC *peers.Peer
	for i := 0; i < 200; i++ {
		naB, _ = a.svc.GetPeer(ctx, b.id)
		naC, _ = a.svc.GetPeer(ctx, c.id)
		if naB.LastSync != nil || naC.LastSync != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if naB.LastSync != nil {
		t.Errorf("A je C-ovu razmjenu zabilježio B-u (%q): čvor je tražio po ključu iz zapisa čvora", naB.LastSyncNote)
	}
	if naC.LastSync == nil {
		t.Error("A nije zabilježio razmjenu s C")
	}
}

// Računalo koje je dobilo novi ključ (opoziv, zaborav i ponovno
// uparivanje pod istim imenom) trećem čvoru stiže knjigom: novi zapis
// čvora i novo članstvo. Treći čvor zatim razmjenjuje s novim ključem, a
// zapis čvora nosi novi ključ.
func TestNoviKljucStizeTrecemCvoru(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a := startNode(t, ctx, "cop-osijek")
	b := startNode(t, ctx, "laptop-vinkovci")
	c := startNode(t, ctx, "laptop-zupanja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	pair(t, ctx, a, c)
	if _, _, err := c.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("C → B prije novog ključa: %v", err)
	}

	if _, err := a.svc.RevokeMembership(ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if err := a.svc.ForgetPeer(ctx, b.id); err != nil {
		t.Fatal(err)
	}
	novi := startNode(t, ctx, "laptop-vinkovci")
	pair(t, ctx, a, novi)
	if _, _, err := c.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if p, err := c.svc.GetPeer(ctx, b.id); err != nil || p == nil || p.PublicKey != novi.svc.Node().PublicKey() {
		t.Fatalf("C nakon razmjene s A: zapis B-a %+v %v", p, err)
	}

	editStationNote(t, novi, "Primjerovo", "s novim ključem")
	if _, _, err := c.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatalf("C → B s novim ključem: %v", err)
	}
	if got := stationNote(t, c, "Primjerovo"); got != "s novim ključem" {
		t.Errorf("C nema podatke B-a s novim ključem: %q", got)
	}
}
