package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/postava"
)

// cvorUStanju je vlastiti čvor u zadanom stanju procesa
type cvorUStanju struct {
	proc                  postava.StanjeProcesa
	pokrenut, zaustavljen int
}

func (c *cvorUStanju) Instaliran() bool                       { return true }
func (c *cvorUStanju) Pokreni() error                         { c.pokrenut++; return nil }
func (c *cvorUStanju) Zaustavi(time.Duration) error           { c.zaustavljen++; return nil }
func (c *cvorUStanju) Proces() postava.StanjeProcesa          { return c.proc }
func (c *cvorUStanju) Zdravlje(context.Context) (string, int) { return "", 0 }
func (c *cvorUStanju) NaPromjenu(func())                      {}

func TestOpisStanja(t *testing.T) {
	for s, zelim := range map[postava.StanjeCvora]string{
		postava.NijeInstaliran:  "nije preuzet",
		postava.Zaustavljen:     "zaustavljen",
		postava.Pokrece:         "pokreće se",
		postava.Radi:            "radi",
		postava.NeOdgovara:      "ne odgovara",
		postava.Pao:             "pao je; vidi dnevnik čvora",
		postava.StanjeCvora(99): "nije preuzet",
	} {
		if g := opisStanja(postava.Stanje{Cvor: s}); g != zelim {
			t.Errorf("%d: %q, želim %q", s, g, zelim)
		}
	}
}

// Prekidač u izborniku pali zaustavljen ili pao čvor, a onaj koji radi gasi.
func TestPrekidacPoStanju(t *testing.T) {
	slucajevi := []struct {
		ime              string
		proc             postava.StanjeProcesa
		pokrenut, ugasen int
	}{
		{"zaustavljen", postava.StanjeProcesa{}, 1, 0},
		{"pao", postava.StanjeProcesa{Pao: true}, 1, 0},
		// stanje računa time.Since(Pokrenut), pa ovdje mora biti sada; rok
		// zdravlja je 90 s, pa brzina stroja ne mijenja ishod
		{"pokreće se", postava.StanjeProcesa{Radi: true, Pokrenut: time.Now()}, 0, 1},
		{"ne odgovara", postava.StanjeProcesa{Radi: true, Pokrenut: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, 0, 1},
	}
	for _, s := range slucajevi {
		t.Run(s.ime, func(t *testing.T) {
			tr, pr := novaTraka(t, nil)
			c := &cvorUStanju{proc: s.proc}
			tr.p.Cvor = c
			tr.prekidacKlik()
			if c.pokrenut != s.pokrenut || c.zaustavljen != s.ugasen {
				t.Errorf("pokrenut %d, zaustavljen %d; želim %d, %d", c.pokrenut, c.zaustavljen, s.pokrenut, s.ugasen)
			}
			if len(pr.poruke)+len(pr.pitanja) != 0 {
				t.Errorf("vlastiti čvor: poruke %v, pitanja %v", pr.poruke, pr.pitanja)
			}
		})
	}
}

// Pri pokretanju instalirana Postava popravi unos za prijavu koji pokazuje
// na staru putanju i vrati program u PATH.
func TestPocetakPopravljaUnoseInstalacije(t *testing.T) {
	tr, _ := novaTraka(t, nil)
	tr.instaliran = true
	tr.p.Cvor = &cvorUStanju{}
	if err := postava.UkljuciPriPrijavi(filepath.Join(t.TempDir(), "stara", "gocop-postava")); err != nil {
		t.Fatal(err)
	}
	if uklj, ispravno := postava.PriPrijavi(tr.p.Exe); !uklj || ispravno {
		t.Fatalf("priprema: %v %v", uklj, ispravno)
	}
	tr.pocetak()
	if uklj, ispravno := postava.PriPrijavi(tr.p.Exe); !uklj || !ispravno {
		t.Errorf("unos za prijavu nije popravljen: %v %v", uklj, ispravno)
	}
	if !postava.UPutu(tr.p.M.Program) {
		t.Error("program nije vraćen u PATH")
	}
	b, _ := os.ReadFile(tr.p.M.DnevnikPostave()) // nepostojeći dnevnik je prazan, pa usporedba niže pada
	if !strings.Contains(string(b), "Pokretanje pri prijavi popravljeno") {
		t.Errorf("dnevnik:\n%s", b)
	}
}

// Isključeno pokretanje pri prijavi ostaje isključeno, a tuđa datoteka na
// mjestu poveznice se ne dira.
func TestPocetakNeDiraTudje(t *testing.T) {
	tr, _ := novaTraka(t, nil)
	tr.instaliran = true
	tr.p.Cvor = &cvorUStanju{}
	home, _ := os.UserHomeDir() // bez kućne mape put je relativan, a test i dalje provjerava ishod
	tudja := filepath.Join(home, ".local", "bin", "gocop")
	if err := os.MkdirAll(filepath.Dir(tudja), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tudja, []byte("tuđe"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr.pocetak()
	if uklj, _ := postava.PriPrijavi(tr.p.Exe); uklj {
		t.Error("pokretanje pri prijavi uključeno bez pitanja")
	}
	if b, _ := os.ReadFile(tudja); string(b) != "tuđe" { // nepročitana datoteka je prazna, pa usporedba pada
		t.Error("tuđa datoteka je promijenjena")
	}
	b, _ := os.ReadFile(tr.p.M.DnevnikPostave()) // nepostojeći dnevnik je prazan, pa usporedba niže pada
	if !strings.Contains(string(b), "PATH:") {
		t.Errorf("dnevnik:\n%s", b)
	}
}

func TestPriPrijaviKlikPrebacuje(t *testing.T) {
	tr, pr := novaTraka(t, nil)
	tr.izbornik()
	if tr.priPrijavi.IsChecked() {
		t.Fatal("uključeno prije klika")
	}
	tr.priPrijaviKlik()
	if uklj, ispravno := postava.PriPrijavi(tr.p.Exe); !uklj || !ispravno || !tr.priPrijavi.IsChecked() {
		t.Errorf("nakon prvog klika: %v %v, kvačica %v", uklj, ispravno, tr.priPrijavi.IsChecked())
	}
	tr.priPrijaviKlik()
	if uklj, _ := postava.PriPrijavi(tr.p.Exe); uklj || tr.priPrijavi.IsChecked() {
		t.Errorf("nakon drugog klika: %v, kvačica %v", uklj, tr.priPrijavi.IsChecked())
	}
	if len(pr.poruke) != 0 {
		t.Errorf("poruke %v", pr.poruke)
	}
}

// Bez novijeg izdanja nadogradnja ne pita ništa, a ploča se ne otvara dok
// čvor ne radi.
func TestBezPonudeINeRadiNistaSeNeOtvara(t *testing.T) {
	tr, pr := novaTraka(t, nil)
	c := &cvorUStanju{}
	tr.p.Cvor = c
	tr.nadogradiKlik()
	tr.otvoriPlocu()
	if len(pr.pitanja)+len(pr.poruke)+len(pr.otvoreno) != 0 || c.pokrenut+c.zaustavljen != 0 {
		t.Errorf("pitanja %v, poruke %v, otvoreno %v", pr.pitanja, pr.poruke, pr.otvoreno)
	}
}
