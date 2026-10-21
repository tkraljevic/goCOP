package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"gocop/internal/config"
	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/peers"
)

// Postavke [sync] iz gocop.toml stižu do razmjene: sve i tailscale, u oba
// smjera
func TestPostavkeRazmjeneIzDatoteke(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	cvor, err := peers.LoadNode(dbPath, "pperic-thinkpad", "Probni", "test")
	if err != nil {
		t.Fatal(err)
	}
	s, err := peers.NewService(baza, ledger.New(baza, "pperic-thinkpad"), cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	for _, k := range []struct{ sve, tailscale bool }{{false, false}, {true, true}, {true, false}, {false, true}} {
		cfg.Sync.All, cfg.Sync.Tailscale = k.sve, k.tailscale
		postaviRazmjenu(s, cfg)
		if s.WantsAll() != k.sve || s.TailscaleUkljucen() != k.tailscale {
			t.Errorf("[sync] sve = %v, tailscale = %v: razmjena prati sve %v, Tailscale %v", k.sve, k.tailscale, s.WantsAll(), s.TailscaleUkljucen())
		}
	}
}

// [sync] protokol iz gocop.toml stiže do razmjene prije osnivanja mreže:
// baza pamti mrežu pod tim imenom, pa ista mapa uz zadano ime ne kreće
func TestProtokolRazmjeneIzDatoteke(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	cvor, err := peers.LoadNode(dbPath, "laptop-sim", "Simulacija", "test")
	if err != nil {
		t.Fatal(err)
	}
	s, err := peers.NewService(baza, ledger.New(baza, "laptop-sim"), cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Sync.Protokol = "gocop-sim"
	postaviRazmjenu(s, cfg)
	if err := s.CreateNetwork(context.Background(), "Testna simulacija"); err != nil {
		t.Fatal(err)
	}
	if err := peers.ProvjeriProtokolMreze(dbPath, "gocop-sim"); err != nil {
		t.Errorf("isto ime: %v", err)
	}
	var drugi *peers.ProtokolDrugeMreze
	if err := peers.ProvjeriProtokolMreze(dbPath, "gocop"); !errors.As(err, &drugi) || drugi.Upisan != "gocop-sim" {
		t.Errorf("zadano ime uz mrežu simulacije: %v", err)
	}
}
