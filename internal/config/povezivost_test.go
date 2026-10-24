package config

// Odjeljak [povezivost] (WEB sync, F1a): zadano je sve isključeno, stara
// datoteka bez odjeljka daje zadano, prazan popis STUN-a isključuje
// provjeru, a krivi upisi se odbiju.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPovezivost(t *testing.T) {
	z := Default().Povezivost
	if z.WebSyncPrijava || z.WebSync || z.UDPPort != 4713 || len(z.Stun) != 2 || z.LanAdresa != "" ||
		z.Sastajaliste != "" || z.SastajalisteCvor != "" || z.PosluziSastajaliste {
		t.Errorf("zadano %+v", z)
	}
	dir := t.TempDir()
	for _, c := range []struct {
		ime, sadrzaj string
		provjera     func(Config) bool
	}{
		{"stara datoteka", "[sync]\nsve = true\n", func(c Config) bool { return !c.Povezivost.WebSyncPrijava && len(c.Povezivost.Stun) == 2 }},
		{"prijava", "[povezivost]\nweb_sync_prijava = true\n", func(c Config) bool { return c.Povezivost.WebSyncPrijava && c.Povezivost.UDPPort == 4713 }},
		{"bez STUN-a", "[povezivost]\nstun = []\n", func(c Config) bool { return len(c.Povezivost.Stun) == 0 }},
		{"svoj STUN", "[povezivost]\nstun = ['stun.primjer.hr:3478']\n", func(c Config) bool { return len(c.Povezivost.Stun) == 1 }},
	} {
		put := filepath.Join(dir, strings.ReplaceAll(c.ime, " ", "-")+".toml")
		if err := os.WriteFile(put, []byte(c.sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, _, err := Load([]string{put})
		if err != nil || !c.provjera(cfg) {
			t.Errorf("%s: %+v %v", c.ime, cfg.Povezivost, err)
		}
	}
	if _, err := ZapisiPrimjer(dir); err != nil {
		t.Fatal(err)
	}
	primjer, err := os.ReadFile(filepath.Join(dir, PrimjerFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, trazi := range []string{"[povezivost]", "web_sync_prijava = false", "udp_port = 4713", "Prazan popis isključuje provjeru"} {
		if !strings.Contains(string(primjer), trazi) {
			t.Errorf("primjer postavki ne sadrži %q", trazi)
		}
	}
}

func TestProvjeriPovezivost(t *testing.T) {
	for _, c := range []struct {
		ime     string
		izmjena func(*Config)
		kljuc   string
	}{
		{"port", func(c *Config) { c.Povezivost.UDPPort = 70000 }, "povezivost.udp_port"},
		{"STUN bez porta", func(c *Config) { c.Povezivost.Stun = []string{"stun.primjer.hr"} }, "povezivost.stun"},
		{"STUN s razmakom", func(c *Config) { c.Povezivost.Stun = []string{"stun primjer:3478"} }, "povezivost.stun"},
		{"javna adresa umjesto lokalne", func(c *Config) { c.Povezivost.LanAdresa = "8.8.8.8" }, "povezivost.lan_adresa"},
		{"nije adresa", func(c *Config) { c.Povezivost.LanAdresa = "ured" }, "povezivost.lan_adresa"},
		{"IPv6 iz privatne mreže", func(c *Config) { c.Povezivost.LanAdresa = "fd00::5" }, "povezivost.lan_adresa"},
		{"sastajalište bez sheme", func(c *Config) { c.Povezivost.Sastajaliste = "cop.primjer.hr" }, "povezivost.sastajaliste"},
		{"čvor sastajališta s razmakom", func(c *Config) { c.Povezivost.SastajalisteCvor = "cop osijek" }, "povezivost.sastajaliste_cvor"},
	} {
		cfg := Default()
		c.izmjena(&cfg)
		if g := Provjeri(cfg); g[c.kljuc] == "" {
			t.Errorf("%s: bez greške (%v)", c.ime, g)
		}
	}
	cfg := Default()
	cfg.Povezivost.LanAdresa = "192.168.1.20"
	cfg.Povezivost.Sastajaliste, cfg.Povezivost.SastajalisteCvor = "https://cop.primjer.hr", "cop-osijek"
	cfg.Povezivost.Stun = nil
	if g := Provjeri(cfg); len(g) != 0 {
		t.Errorf("ispravno: %v", g)
	}
}
