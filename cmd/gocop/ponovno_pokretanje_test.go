package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/config"
	"gocop/internal/web"
)

// Tko je pokrenuo čvor: Postava zastavicom, usluga i spremnik varijablom,
// a stariji tragom koji ostave (INVOCATION_ID uz roditelja PID 1, datoteka
// spremnika). Ostalo je ručno.
func TestNacinRada(t *testing.T) {
	slucajevi := []struct {
		ime         string
		podPostavom bool
		env         map[string]string
		datoteka    string
		roditelj    int
		ocekivano   web.NacinRada
	}{
		{"Postava", true, nil, "", 4321, web.PodPostavom},
		{"usluga sustava", false, map[string]string{VarijablaNadzora: "systemd"}, "", 1, web.UslugaSustava},
		{"spremnik", false, map[string]string{VarijablaNadzora: "docker"}, "", 1, web.USpremniku},
		{"stariji spremnik", false, nil, "/.dockerenv", 0, web.USpremniku},
		{"Podman", false, nil, "/run/.containerenv", 0, web.USpremniku},
		{"starija usluga", false, map[string]string{"INVOCATION_ID": "a1b2"}, "", 1, web.UslugaSustava},
		{"terminal pokrenut iz usluge", false, map[string]string{"INVOCATION_ID": "a1b2"}, "", 4321, web.RucnoPokrenut},
		{"ručno", false, nil, "", 4321, web.RucnoPokrenut},
	}
	for _, s := range slucajevi {
		env := func(k string) string { return s.env[k] }
		postoji := func(p string) bool { return p == s.datoteka }
		if got := nacinRada(s.podPostavom, env, postoji, s.roditelj); got != s.ocekivano {
			t.Errorf("%s: %d, a mora biti %d", s.ime, got, s.ocekivano)
		}
	}
	if postojiDatoteka(filepath.Join(t.TempDir(), "nema")) {
		t.Error("nepostojeća datoteka postoji")
	}
}

// Ponovno pokretanje daje izlazni kod koji podigne čvor u svakom načinu
// rada: Postava podiže svaki izlazak koji nije zatražila, systemd uz
// Restart=on-failure i Docker uz --restart on-failure svaki kod različit od
// 0. Gašenje signalom ili zatvorenim ulazom ostaje 0.
func TestIzlazniKodPonovnogPokretanja(t *testing.T) {
	for _, nacin := range []web.NacinRada{web.PodPostavom, web.UslugaSustava, web.USpremniku} {
		ponovno := make(chan struct{}, 1)
		zatrazi := zatraziPonovno(ponovno)
		zatrazi()
		zatrazi() // drugi klik dok prvi čeka ne blokira
		if kod := cekajKraj(context.Background(), nil, ponovno); kod != web.KodPonovnogPokretanja || kod == 0 || !nacin.Podize() {
			t.Errorf("način %d: izlazni kod %d", nacin, kod)
		}
	}
	if web.RucnoPokrenut.Podize() {
		t.Error("ručno pokrenut čvor nitko ne podiže, a gumb bi ga ugasio zauvijek")
	}
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	if kod := cekajKraj(ctx, nil, nil); kod != 0 {
		t.Errorf("gašenje signalom: %d", kod)
	}
	stop := make(chan os.Signal, 1)
	stop <- os.Interrupt
	if kod := cekajKraj(context.Background(), stop, nil); kod != 0 {
		t.Errorf("gašenje na zahtjev Postave: %d", kod)
	}
	// jedinica i spremnik nose varijablu, a jedinica ne sprječava podizanje koda 75
	jedinica, err := os.ReadFile(filepath.Join("..", "..", "build", "linux", "gocop.service"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(jedinica); !strings.Contains(s, "Restart=on-failure") || !strings.Contains(s, "Environment="+VarijablaNadzora+"=systemd") ||
		strings.Contains(s, "RestartPreventExitStatus") || strings.Contains(s, "SuccessExitStatus") {
		t.Errorf("gocop.service:\n%s", s)
	}
	spremnik, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spremnik), "ENV "+VarijablaNadzora+"=docker") {
		t.Error("Dockerfile ne javlja čvoru da radi u spremniku")
	}
}

// Postavke koje zastavica zadaje umjesto datoteke, kao u odrediPostavke
func TestJaceZastavice(t *testing.T) {
	z, err := procitajZastavice([]string{"-addr", ":8080", "-db", "/data/gocop.db", "-podaci", "/arhiva/vodostaji", "-sync-port", "0"})
	if err != nil {
		t.Fatal(err)
	}
	jace := jaceZastavice(z)
	for kljuc, zastavica := range map[string]string{"addr": "-addr", "db": "-db", "podaci": "-podaci", "sync.exchange_port": "-sync-port"} {
		if jace[kljuc] != zastavica {
			t.Errorf("%s: %q", kljuc, jace[kljuc])
		}
	}
	for _, kljuc := range []string{"pakete", "arhiva", "node.id", "sync.auto_sync", "sync.pair_port"} {
		if jace[kljuc] != "" {
			t.Errorf("%s bez zastavice: %q", kljuc, jace[kljuc])
		}
	}
	for kljuc := range jace {
		if _, ok := config.NadjiKljuc(kljuc); !ok {
			t.Errorf("zastavica za nepostojeću postavku %s", kljuc)
		}
	}
}

// Koju datoteku čvor čita i od čega nastaje nova
func TestPostavkeZaPosluzitelj(t *testing.T) {
	cfg := config.Default()
	cfg.DB = filepath.Join("/var/lib/gocop", "gocop.db")
	cfg.Node.ID = "ivic-laptop"
	z, err := procitajZastavice([]string{"-pakete", "/arhiva/pakete"})
	if err != nil {
		t.Fatal(err)
	}
	p := postavkeZaPosluzitelj(cfg, "", z, map[string]string{"arhiva": "/srv/vodostaji.db", "podaci": "vodostaji", "pakete": ""})
	if p.Put != filepath.Join("/var/lib/gocop", config.FileName) || p.Pocetna.Node.ID != "ivic-laptop" || p.Vrijedi.Pakete != "/arhiva/pakete" || p.Pocetna.Pakete != "" {
		t.Errorf("bez datoteke: %+v", p)
	}
	// putanje se pokazuju pune, a isključena (prazna) se ne pokazuje
	radna, _ := os.Getwd()
	if p.Putanje["arhiva"] != "/srv/vodostaji.db" || p.Putanje["podaci"] != filepath.Join(radna, "vodostaji") || len(p.Putanje) != 2 {
		t.Errorf("putanje: %v", p.Putanje)
	}
	z.configPath = "/data/gocop.toml"
	if p := postavkeZaPosluzitelj(cfg, "", z, nil); p.Put != "/data/gocop.toml" {
		t.Errorf("uz -config: %q", p.Put)
	}
	if p := postavkeZaPosluzitelj(cfg, "/drugdje/gocop.toml", z, nil); p.Put != "/drugdje/gocop.toml" {
		t.Errorf("pročitana datoteka: %q", p.Put)
	}
}
