package peers

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/repository"
)

// zapisCvoraSDrugog: b upiše svoj zapis s adresom i vrati tu verziju, kakva
// bi stigla razmjenom
func zapisCvoraSDrugog(t *testing.T, b *Service, adresa string) ledger.Version {
	t.Helper()
	if err := b.SavePeer(ctxRub, Peer{NodeID: b.node.ID, Name: b.node.Name, PublicKey: b.node.PublicKey(), Addresses: []string{adresa}}); err != nil {
		t.Fatal(err)
	}
	povijest, err := b.rec.History(ctxRub, EntityPeers, b.node.ID)
	if err != nil || len(povijest) == 0 {
		t.Fatalf("b nema verziju svog zapisa: %v", err)
	}
	return povijest[0]
}

// Izmjena zapisa čvora (adresa, izloženost, naziv, uparivanje, potvrda)
// čeka upis primljenog i nastavlja od onoga što je razmjena upisala. Prije
// je zapis čitala izvan brave: kad bi primljeni zapis istog čvora stigao
// između čitanja i spremanja, izmjena bi spremila stari zapis kao novu
// verziju i primljenu adresu izgubila na svim čvorovima.
func TestIzmjenaCvoraCekaUpisPrimljenog(t *testing.T) {
	a := rubniCvor(t, "cop-osijek")
	a.OnApplied(func(ctx context.Context, verzije []ledger.Version) error {
		return repository.ApplyVersions(ctx, a.db, a.rec, verzije)
	})
	b := rubniCvor(t, "laptop-vinkovci")
	if _, povrsina, err := a.primijeni(ctxRub, []ledger.Version{zapisCvoraSDrugog(t, b, "10.0.0.1:4710")}); err != nil || povrsina != nil {
		t.Fatal(err, povrsina)
	}

	// razmjena upisuje primljeno
	if err := a.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	gotovo := make(chan error, 1)
	go func() {
		_, err := a.PublicAddress(ctxRub, b.node.ID, "cop.primjer.hr:4710", "")
		gotovo <- err
	}()
	select {
	case err := <-gotovo:
		a.pustiBravu()
		t.Fatalf("izmjena zapisa čvora nije čekala upis primljenog (%v)", err)
	case <-time.After(300 * time.Millisecond):
	}
	novi := zapisCvoraSDrugog(t, b, "10.0.0.2:4710")
	_, povrsina, err := a.primijeni(ctxRub, []ledger.Version{novi})
	a.pustiBravu()
	if err != nil || povrsina != nil {
		t.Fatal(err, povrsina)
	}
	if err := <-gotovo; err != nil {
		t.Fatal(err)
	}

	p, err := a.GetPeer(ctxRub, b.node.ID)
	if err != nil || p == nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.Addresses, "10.0.0.2:4710") || !slices.Contains(p.Addresses, "cop.primjer.hr:4710") {
		t.Errorf("zapis B-a na A nakon izmjene: %v; mora imati primljenu i dodanu adresu", p.Addresses)
	}
	if slices.Contains(p.Addresses, "10.0.0.1:4710") {
		t.Errorf("izmjena je vratila adresu koju je primljeni zapis zamijenio: %v", p.Addresses)
	}
}

// IzmijeniCvor ne sprema kad ne dobije bravu, ne pročita zapis ili izmjena
// odustane, a grešku spremanja vrati
func TestIzmijeniCvorRubovi(t *testing.T) {
	a := rubniCvor(t, "cop-osijek")
	sprema := func(*Peer) (*Peer, error) {
		return &Peer{NodeID: "laptop-vinkovci", Name: "laptop-vinkovci", PublicKey: a.node.PublicKey()}, nil
	}

	otkazan, cancel := context.WithCancel(ctxRub)
	cancel()
	if err := a.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	_, err := a.IzmijeniCvor(otkazan, "laptop-vinkovci", sprema)
	a.pustiBravu()
	if !errors.Is(err, context.Canceled) {
		t.Errorf("bez brave: %v", err)
	}

	odustani := errors.New("odustano")
	if p, err := a.IzmijeniCvor(ctxRub, "laptop-vinkovci", func(*Peer) (*Peer, error) { return nil, odustani }); p != nil || !errors.Is(err, odustani) {
		t.Errorf("izmjena koja odustane: %v %v", p, err)
	}
	if _, err := a.IzmijeniCvor(ctxRub, "laptop-vinkovci", func(*Peer) (*Peer, error) { return &Peer{NodeID: "laptop-vinkovci"}, nil }); err == nil {
		t.Error("zapis bez ključa je spremljen")
	}
	if p, err := a.GetPeer(ctxRub, "laptop-vinkovci"); err != nil || p != nil {
		t.Fatalf("zapis je spremljen iako izmjena nije prošla: %+v %v", p, err)
	}

	pokvariTablicu(t, a, "peers")
	if _, err := a.IzmijeniCvor(ctxRub, "laptop-vinkovci", sprema); err == nil {
		t.Error("bez tablice peers izmjena nije javila grešku")
	}
}
