package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrimjerSeZapiseIProcita(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	written, err := WriteExample(path, Default())
	if err != nil || !written {
		t.Fatalf("primjer nije zapisan: written=%v err=%v", written, err)
	}
	// drugi put se ne prepisuje
	if written, _ := WriteExample(path, Default()); written {
		t.Error("postojeća datoteka ne smije se pregaziti")
	}

	raw, _ := os.ReadFile(path)
	for _, want := range []string{"addr", "[node]", "[sync]", "bootstrap", "cop-osijek.com", "# goCOP"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("primjer ne sadrži %q", want)
		}
	}

	cfg, from, err := Load([]string{path})
	if err != nil || from != path {
		t.Fatalf("čitanje: from=%q err=%v", from, err)
	}
	if cfg.Addr != ":80" || cfg.Sync.ExchangePort != 4710 || cfg.AutoSyncDuration().Minutes() != 5 {
		t.Errorf("pročitane vrijednosti ne odgovaraju zadanima: %+v", cfg)
	}
}

func TestKorisnikovaIzmjenaVrijedi(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(path, []byte(`
addr = ":9090"
[node]
id = "laptop-vinkovci"
[sync]
auto_sync = "0"
bootstrap = ["cop-osijek.com", "cop.voda.hr"]
`), 0o644)

	cfg, _, err := Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9090" || cfg.Node.ID != "laptop-vinkovci" {
		t.Errorf("izmjene nisu pročitane: %+v", cfg)
	}
	if cfg.AutoSyncDuration() != 0 {
		t.Error("auto_sync = \"0\" mora isključiti automatsku sinkronizaciju")
	}
	if len(cfg.Sync.Bootstrap) != 2 || cfg.Sync.Bootstrap[1] != "cop.voda.hr" {
		t.Errorf("popis domena: %v", cfg.Sync.Bootstrap)
	}
	// ono što nije navedeno ostaje zadano
	if cfg.Sync.ExchangePort != 4710 || cfg.DB != "data/gocop.db" {
		t.Errorf("nenavedene vrijednosti moraju ostati zadane: %+v", cfg)
	}
}

func TestNepostojecaDatotekaNijeGreska(t *testing.T) {
	cfg, from, err := Load([]string{filepath.Join(t.TempDir(), "nema.toml")})
	if err != nil || from != "" {
		t.Fatalf("bez datoteke: from=%q err=%v", from, err)
	}
	if cfg.Addr != Default().Addr {
		t.Error("bez datoteke vrijede zadane vrijednosti")
	}
}

func TestNeispravnaDatotekaJeGreska(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(path, []byte("addr = :80 ovo nije toml"), 0o644)
	if _, _, err := Load([]string{path}); err == nil {
		t.Error("neispravna datoteka mora vratiti grešku, ne tiho zadane vrijednosti")
	}
}
