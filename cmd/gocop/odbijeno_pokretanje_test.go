package main

// Odbijeno pokretanje pod Postavom (Odluke (8. 10. 2026.), plan izdanja
// 0.0.35, stavka 6): čvor bez ključa uz postojeću bazu ne kreće, a pod
// Postavom ni ne izlazi dok ga ona ne zaustavi (kljuc_cvora_postava_test.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gocop/internal/web"
)

// Pod -upravitelj čvor bez ključa na ovom računalu odgovara na /zdravlje i
// kaže zašto ne radi, ne zapisuje ništa i izlazi s kodom 1 tek kad mu
// Postava zatvori ulaz
func TestCvorBezKljucaPodPostavomCekaZatvaranjeUlaza(t *testing.T) {
	put := bazaZaOporavak(t)
	dir := filepath.Dir(put)
	dnevnik := dnevnikTesta(t)
	adresa := fmt.Sprintf("127.0.0.1:%d", slobodanPort(t))
	citac, pisac := io.Pipe()
	t.Cleanup(func() { pisac.Close() })
	gotovo := make(chan int, 1)
	go func() {
		gotovo <- run(context.Background(), []string{
			"-upravitelj", "-db", put, "-addr", adresa, "-sync-port", "0", "-discovery-port", "0",
			"-auto-sync", "0", "-pakete", "", "-podaci", "",
		}, citac)
	}()

	var z web.Zdravlje
	rok := time.Now().Add(20 * time.Second)
	for {
		odg, err := http.Get("http://" + adresa + "/zdravlje")
		if err == nil {
			err = json.NewDecoder(odg.Body).Decode(&z)
			odg.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case kod := <-gotovo:
			t.Fatalf("čvor je izašao (kod %d) umjesto da čeka Postavu", kod)
		default:
		}
		if time.Now().After(rok) {
			t.Fatal("odbijeni čvor nije odgovorio na /zdravlje za 20 s")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if z.Izdanje != verzijaPrograma || !z.Radi || !strings.Contains(z.Odbijeno, "nema ključa čvora") {
		t.Errorf("/zdravlje: %+v", z)
	}
	odg, err := http.Get("http://" + adresa + "/")
	if err != nil {
		t.Fatal(err)
	}
	tekst, _ := io.ReadAll(odg.Body)
	odg.Body.Close()
	if odg.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(tekst), filepath.Join(dir, "node-key")) {
		t.Errorf("ploča: %d %q", odg.StatusCode, tekst)
	}

	pisac.Close()
	select {
	case kod := <-gotovo:
		if kod != 1 {
			t.Errorf("izlazni kod %d, očekivano 1", kod)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("odbijeni čvor nije izašao 10 s nakon zatvaranja ulaza")
	}
	for _, datoteka := range []string{"node-key", "gocop.toml", "sadrzaj.db"} {
		if _, err := os.Stat(filepath.Join(dir, datoteka)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("odbijeno pokretanje zapisalo je %s: %v", datoteka, err)
		}
	}
	for _, mora := range []string{"Čvor se ne pokreće", "ostaje upaljen dok ga ona ne zaustavi, da ga pri nadogradnji ne zamijeni prethodnim izdanjem, koje ovu provjeru možda nema.", "Standardni ulaz zatvoren"} {
		if !strings.Contains(dnevnik.String(), mora) {
			t.Errorf("u dnevniku nema %q:\n%s", mora, dnevnik)
		}
	}
	if c, err := net.DialTimeout("tcp", adresa, time.Second); err == nil {
		c.Close()
		t.Error("nakon izlaska port je i dalje otvoren")
	}
}

// Gašenje programa (ctx, npr. Ctrl+C) završava čekanje i bez Postave
func TestOdbijeniCvorIzlaziNaGasenje(t *testing.T) {
	dnevnikTesta(t)
	citac, pisac := io.Pipe()
	t.Cleanup(func() { pisac.Close() })
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	adresa := fmt.Sprintf("127.0.0.1:%d", slobodanPort(t))
	if kod := odbijPokretanje(ctx, true, adresa, errors.New("odbijeno"), citac); kod != 1 {
		t.Errorf("izlazni kod %d", kod)
	}
	if c, err := net.DialTimeout("tcp", adresa, time.Second); err == nil {
		c.Close()
		t.Error("nakon izlaska port je i dalje otvoren")
	}
}

// Kad port web sučelja nije slobodan ili adresa ne valja, odbijeni čvor
// izlazi odmah, uz zapis zašto ga Postava vidi kao pao
func TestOdbijeniCvorBezPortaIzlaziOdmah(t *testing.T) {
	zauzet, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer zauzet.Close()
	for _, adresa := range []string{zauzet.Addr().String(), "bez-porta"} {
		dnevnik := dnevnikTesta(t)
		if kod := odbijPokretanje(context.Background(), true, adresa, errors.New("odbijeno"), strings.NewReader("")); kod != 1 {
			t.Errorf("%s: izlazni kod %d", adresa, kod)
		}
		if !strings.Contains(dnevnik.String(), "Postava će čvor vidjeti kao pao") {
			t.Errorf("%s: dnevnik: %s", adresa, dnevnik)
		}
	}
}

// Postava uz zadani port 80 pita i 8080, kamo i čvor sam prelazi
func TestPortoviOdbijenog(t *testing.T) {
	for adresa, zelim := range map[string][]string{
		":80":            {"80", "8080"},
		"127.0.0.1:4000": {"4000"},
	} {
		if p, err := portoviOdbijenog(adresa); err != nil || !reflect.DeepEqual(p, zelim) {
			t.Errorf("%s: %v %v", adresa, p, err)
		}
	}
	if _, err := portoviOdbijenog("bez-porta"); err == nil {
		t.Error("adresa bez porta prihvaćena")
	}
}

// Zauzet prvi port: odbijeni čvor sluša na sljedećem, i samo na ovom računalu
func TestSlusajNaOvomRacunaluPrelaziNaSljedeciPort(t *testing.T) {
	zauzet, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer zauzet.Close()
	_, prvi, _ := net.SplitHostPort(zauzet.Addr().String())
	drugi := fmt.Sprint(slobodanPort(t))
	ln, err := slusajNaOvomRacunalu([]string{prvi, drugi})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if ln.Addr().String() != "127.0.0.1:"+drugi {
		t.Errorf("sluša na %s, očekivano 127.0.0.1:%s", ln.Addr(), drugi)
	}
	if _, err := slusajNaOvomRacunalu([]string{prvi}); err == nil {
		t.Error("zauzet port prihvaćen")
	}
}
