package main

import (
	"flag"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/postava"
)

// pokreniMain zove main sa zadanim argumentima i vraća što je ispisao.
// Korisnikove mape su u privremenoj mapi, pa se ništa pravo ne dira.
func pokreniMain(t *testing.T, args ...string) string {
	t.Helper()
	stariArgs, stareZastavice, stariIzlaz := os.Args, flag.CommandLine, os.Stdout
	t.Cleanup(func() { os.Args, flag.CommandLine, os.Stdout = stariArgs, stareZastavice, stariIzlaz })
	os.Args = append([]string{"gocop-postava"}, args...)
	flag.CommandLine = flag.NewFlagSet("gocop-postava", flag.ContinueOnError)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	procitano := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r) // čitanje iz cijevi u memoriji ne javlja grešku; prazno pada na usporedbi
		procitano <- string(b)
	}()
	main()
	w.Close()
	os.Stdout = stariIzlaz
	return <-procitano
}

func korisnikUPrivremenoj(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
}

// instalacijaUPrivremenoj vraća put Postave i njezina mjesta, i staje prije
// nego main išta dirne ako mjesta nisu u privremenoj korisnikovoj mapi
func instalacijaUPrivremenoj(t *testing.T) (string, postava.Mjesta) {
	t.Helper()
	korisnikUPrivremenoj(t)
	exe, err := putPostave()
	if err != nil {
		t.Fatal(err)
	}
	m := postava.OdrediMjesta(exe)
	if !strings.HasPrefix(m.Baza, os.Getenv("HOME")) {
		t.Fatalf("instalacija izvan privremene mape: %s", m.Baza)
	}
	return exe, m
}

func TestMainIspisujeIzdanje(t *testing.T) {
	korisnikUPrivremenoj(t)
	if izlaz := pokreniMain(t, "-version"); izlaz != "goCOP Postava "+verzija+"\n" {
		t.Errorf("ispis %q", izlaz)
	}
}

func TestMainZapisujeIkonu(t *testing.T) {
	korisnikUPrivremenoj(t)
	put := filepath.Join(t.TempDir(), "gocop.png")
	if izlaz := pokreniMain(t, "-ikona", put); izlaz != "" {
		t.Errorf("ispis %q", izlaz)
	}
	f, err := os.Open(put)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if s, err := png.Decode(f); err != nil || s.Bounds().Dx() != 256 {
		t.Errorf("ikona: %v", err)
	}
}

// Zaustavljanje bez Postave i čvora koji rade prolazi tiho.
func TestMainZaustaviKadNistaNeRadi(t *testing.T) {
	instalacijaUPrivremenoj(t)
	if izlaz := pokreniMain(t, "-zaustavi"); izlaz != "" {
		t.Errorf("ispis %q", izlaz)
	}
}

// Uklanjanje miče pokretanje pri prijavi i poveznicu ove instalacije.
func TestMainUkloniMiceUnose(t *testing.T) {
	exe, m := instalacijaUPrivremenoj(t)
	if err := postava.DodajUPut(m.Program); err != nil {
		t.Fatal(err)
	}
	if err := postava.UkljuciPriPrijavi(exe); err != nil {
		t.Fatal(err)
	}
	pokreniMain(t, "-ukloni")
	if postava.UPutu(m.Program) {
		t.Error("poveznica u PATH-u je ostala")
	}
	if uklj, _ := postava.PriPrijavi(exe); uklj {
		t.Error("pokretanje pri prijavi je ostalo")
	}
}
