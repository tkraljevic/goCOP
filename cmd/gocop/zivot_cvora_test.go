package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/web"
)

// Životni ciklus čvora u istom procesu: run s privremenom mapom podigne
// čvor (baza, knjiga, servisi, poslužitelj), čvor odgovara na /zdravlje,
// prijavu i postavljanje, a otkazan kontekst ga uredno ugasi. Ovo je mreža
// za preuređivanje main: svaki korak mora ostaviti ovaj test zelenim.
func TestZivotCvoraURunu(t *testing.T) {
	if testing.Short() {
		t.Skip("podiže cijeli čvor")
	}
	dir := t.TempDir()
	adresa, z, ugasi := cvorURunu(t, "-db", filepath.Join(dir, "gocop.db"), "-node", "pperic-thinkpad")
	if !z.Radi || z.Izdanje != verzijaPrograma {
		t.Errorf("/zdravlje: %+v", z)
	}
	klijent := &http.Client{Timeout: 5 * time.Second}
	for _, putanja := range []string{"/login", "/postavljanje"} {
		odg, err := klijent.Get("http://" + adresa + putanja)
		if err != nil {
			t.Fatalf("%s: %v", putanja, err)
		}
		odg.Body.Close()
		if odg.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", putanja, odg.StatusCode)
		}
	}
	// Stanje obrane mijenja samo ovjeren akt: izravnih ruta obrane na
	// dionici nema (postojeća ruta neprijavljenog bi poslala na prijavu)
	bezPreusmjeravanja := &http.Client{Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, radnja := range []string{"proglasi", "podigni", "prekini"} {
		odg, err := bezPreusmjeravanja.Post("http://"+adresa+"/sections/A.1.1/obrana/"+radnja, "application/x-www-form-urlencoded", nil)
		if err != nil {
			t.Fatalf("obrana/%s: %v", radnja, err)
		}
		odg.Body.Close()
		if odg.StatusCode != http.StatusNotFound {
			t.Errorf("izravna ruta obrane %s postoji: %d", radnja, odg.StatusCode)
		}
	}
	for _, datoteka := range []string{"gocop.db", "gocop.toml", "node-key"} {
		if _, err := os.Stat(filepath.Join(dir, datoteka)); err != nil {
			t.Errorf("čvor nije napravio %s: %v", datoteka, err)
		}
	}

	if kod := ugasi(); kod != 0 {
		t.Errorf("gašenje s kodom %d", kod)
	}
}

// cvorURunu pokrene čvor (run) u pozadini, s web sučeljem na slobodnom
// portu i bez razmjene, i čeka da odgovori na /zdravlje. Vraća adresu,
// odgovor /zdravlje i funkciju koja čvor ugasi i vrati njegov izlazni kod.
func cvorURunu(t *testing.T, args ...string) (string, web.Zdravlje, func() int) {
	t.Helper()
	adresa := fmt.Sprintf("127.0.0.1:%d", slobodanPort(t))
	ctx, otkazi := context.WithCancel(context.Background())
	t.Cleanup(otkazi)
	args = append(args, "-addr", adresa, "-sync-port", "0", "-discovery-port", "0",
		"-pair-port", fmt.Sprint(slobodanPort(t)), "-auto-sync", "0", "-pakete", "", "-podaci", "")
	gotovo := make(chan int, 1)
	go func() { gotovo <- run(ctx, args, strings.NewReader("")) }()

	klijent := &http.Client{Timeout: 5 * time.Second}
	var z web.Zdravlje
	rok := time.Now().Add(60 * time.Second)
	for {
		odg, err := klijent.Get("http://" + adresa + "/zdravlje")
		if err == nil && odg.StatusCode == http.StatusOK {
			err = json.NewDecoder(odg.Body).Decode(&z)
			odg.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		if err == nil {
			odg.Body.Close()
		}
		select {
		case kod := <-gotovo:
			t.Fatalf("čvor je završio prije nego što je proradio (kod %d)", kod)
		default:
		}
		if time.Now().After(rok) {
			t.Fatalf("čvor nije proradio: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	ugasi := func() int {
		otkazi()
		select {
		case kod := <-gotovo:
			return kod
		case <-time.After(15 * time.Second):
			t.Fatal("čvor se nije ugasio nakon otkazivanja")
		}
		return -1
	}
	return adresa, z, ugasi
}
