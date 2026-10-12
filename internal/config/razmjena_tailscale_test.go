package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// [sync] tailscale je zadano uključen, i u datoteci koja ga ne spominje
// (postavke s prijašnjih inačica); false ga isključuje. Opis u primjeru
// kaže što znači i da na prijavu ne utječe.
func TestPrekidacTailscaleaURazmjeni(t *testing.T) {
	if !Default().Sync.Tailscale {
		t.Error("zadano: Tailscale u razmjeni mora biti uključen")
	}
	dir := t.TempDir()
	stara := filepath.Join(dir, "stara.toml")
	if err := os.WriteFile(stara, []byte("[sync]\nsve = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load([]string{stara})
	if err != nil || !cfg.Sync.Tailscale {
		t.Errorf("postavke bez retka tailscale: %v %v", cfg.Sync.Tailscale, err)
	}
	iskljucen := filepath.Join(dir, "iskljucen.toml")
	if err := os.WriteFile(iskljucen, []byte("[sync]\ntailscale = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Load([]string{iskljucen})
	if err != nil || cfg.Sync.Tailscale {
		t.Errorf("tailscale = false: %v %v", cfg.Sync.Tailscale, err)
	}
	if _, err := ZapisiPrimjer(dir); err != nil {
		t.Fatal(err)
	}
	primjer, err := os.ReadFile(filepath.Join(dir, PrimjerFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, trazi := range []string{"tailscale = true", "Tailscale adrese", "kao da Tailscalea nema", "Na prijavu ne utječe."} {
		if !strings.Contains(string(primjer), trazi) {
			t.Errorf("primjer postavki ne sadrži %q", trazi)
		}
	}
}
