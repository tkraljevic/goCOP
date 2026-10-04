package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/config"
	"gocop/internal/imecvora"
)

// Postavke: zastavica > gocop.toml > zadano
func TestPrednostPostavki(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "gocop.db")
	toml := "addr = \":9090\"\npodaci = \"arhiva-izvori\"\npakete = \"izdani\"\n\n[node]\nid = \"pperic-thinkpad\"\nname = \"Ured\"\n"
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	z, _ := procitajZastavice([]string{"-db", db})
	cfg, odakle, ime, err := odrediPostavke(&z)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9090" || cfg.DB != db || cfg.Node.ID != "pperic-thinkpad" || ime != "pperic-thinkpad" || odakle == "" {
		t.Errorf("iz datoteke: %+v %q %q", cfg, odakle, ime)
	}
	if z.podaci != "arhiva-izvori" || z.paketi != "izdani" {
		t.Errorf("putanje iz datoteke: podaci %q, pakete %q", z.podaci, z.paketi)
	}

	z, _ = procitajZastavice([]string{"-db", db, "-addr", ":8181", "-node", "pperic-laptop", "-podaci", "", "-sync-port", "0", "-auto-sync", "0"})
	cfg, _, ime, err = odrediPostavke(&z)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8181" || cfg.Node.ID != "pperic-laptop" || ime != "pperic-thinkpad" || cfg.Sync.ExchangePort != 0 || cfg.AutoSyncDuration() != 0 {
		t.Errorf("zastavice imaju prednost: %+v, ime iz datoteke %q", cfg, ime)
	}
	if z.podaci != "" || z.paketi != "izdani" {
		t.Errorf("upisana prazna -podaci mora ostati prazna: %q, %q", z.podaci, z.paketi)
	}
}

func TestPokvarenaDatotekaPostavki(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte("addr = [nije niz"), 0o644); err != nil {
		t.Fatal(err)
	}
	z, _ := procitajZastavice([]string{"-db", filepath.Join(dir, "gocop.db")})
	if _, _, _, err := odrediPostavke(&z); err == nil {
		t.Error("pokvaren gocop.toml je prihvaćen")
	}
}

// Ime čvora: upisano ostaje; postojeća baza bez imena zadržava staro zadano
// ime; svjež čvor dobiva jedinstveno ime iz imena računala
func TestImeCvora(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DB: filepath.Join(dir, "gocop.db")}
	cfg.Node.ID = "pperic-thinkpad"
	if odrediImeCvora(&cfg) || cfg.Node.ID != "pperic-thinkpad" {
		t.Errorf("upisano ime: %q", cfg.Node.ID)
	}

	cfg.Node.ID = ""
	if !odrediImeCvora(&cfg) || imecvora.Provjeri(cfg.Node.ID) != nil || cfg.Node.ID == imecvora.Stari {
		t.Errorf("svjež čvor: %q", cfg.Node.ID)
	}
	racunalo, _ := os.Hostname()
	if osnova := imecvora.Ocisti(racunalo); osnova != "" && !strings.HasPrefix(cfg.Node.ID, osnova[:min(len(osnova), 10)]) {
		t.Errorf("ime %q ne počinje imenom računala %q", cfg.Node.ID, osnova)
	}

	if err := os.WriteFile(cfg.DB, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Node.ID = ""
	if odrediImeCvora(&cfg) || cfg.Node.ID != imecvora.Stari {
		t.Errorf("postojeća baza bez imena: %q", cfg.Node.ID)
	}

	cfg.Node.ID = "Krivo Ime"
	if odrediImeCvora(&cfg) || cfg.Node.ID != "Krivo Ime" {
		t.Errorf("neispravno upisano ime se ne mijenja samo: %q", cfg.Node.ID)
	}
}
