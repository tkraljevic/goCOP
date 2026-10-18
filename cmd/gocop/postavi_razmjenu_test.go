package main

import (
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
