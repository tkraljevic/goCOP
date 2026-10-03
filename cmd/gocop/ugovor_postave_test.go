package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gocop/internal/web"
)

// Postava se ne mijenja s goCOP-om (docs/plan-instalacija.md §3.1a), pa
// izdanje goCOP-a ne smije promijeniti ono na što se ona oslanja: redak
// koji ispiše -version, /zdravlje i gašenje pod -upravitelj kad se zatvori
// standardni ulaz. Test prevodi pravi program i razgovara s njim kao Postava.
func TestUgovorSPostavom(t *testing.T) {
	if testing.Short() {
		t.Skip("prevodi i pokreće cijeli program")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "gocop")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	gradnja := exec.Command("go", "build", "-o", program, ".")
	if out, err := gradnja.CombinedOutput(); err != nil {
		t.Fatalf("prevođenje: %v\n%s", err, out)
	}

	// -version: točno jedan redak, bez ičega prije
	out, err := exec.Command(program, "-version").Output()
	if err != nil {
		t.Fatalf("-version: %v", err)
	}
	if got, want := string(out), "goCOP "+verzijaPrograma+"\n"; got != want {
		t.Fatalf("-version ispisuje %q, Postava očekuje %q", got, want)
	}

	port := slobodanPort(t)
	cvor := exec.Command(program, "-upravitelj",
		"-db", filepath.Join(dir, "gocop.db"),
		"-addr", fmt.Sprintf("127.0.0.1:%d", port),
		"-sync-port", "0", "-pair-port", "0", "-discovery-port", "0", "-auto-sync", "0",
		"-pakete", "", "-podaci", "")
	cvor.Dir = dir
	var dnevnik bytes.Buffer
	cvor.Stdout, cvor.Stderr = &dnevnik, &dnevnik
	ulaz, err := cvor.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cvor.Start(); err != nil {
		t.Fatal(err)
	}
	gotov := make(chan error, 1)
	go func() { gotov <- cvor.Wait() }()
	defer func() {
		if cvor.ProcessState == nil {
			_ = cvor.Process.Kill()
		}
	}()

	// /zdravlje s ovog računala: izdanje bez oznake commita
	url := fmt.Sprintf("http://127.0.0.1:%d/zdravlje", port)
	var z web.Zdravlje
	rok := time.Now().Add(60 * time.Second)
	for {
		if odg, err := http.Get(url); err == nil {
			err = json.NewDecoder(odg.Body).Decode(&z)
			odg.Body.Close()
			if err == nil && odg.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case err := <-gotov:
			t.Fatalf("čvor je izašao prije nego što je odgovorio: %v\n%s", err, dnevnik.String())
		default:
		}
		if time.Now().After(rok) {
			t.Fatalf("čvor nije odgovorio na /zdravlje za 60 s\n%s", dnevnik.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	if z.Izdanje != verzijaPrograma || !z.Radi {
		t.Fatalf("/zdravlje: %+v, Postava očekuje izdanje %q", z, verzijaPrograma)
	}

	// zatvoren ulaz: uredno gašenje, izlazni kod 0
	if err := ulaz.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-gotov:
		if err != nil {
			t.Fatalf("čvor se nije uredno ugasio: %v\n%s", err, dnevnik.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("čvor se nije ugasio 20 s nakon zatvaranja ulaza\n%s", dnevnik.String())
	}
	if !strings.Contains(dnevnik.String(), "Standardni ulaz zatvoren") {
		t.Errorf("u dnevniku nema razloga gašenja:\n%s", dnevnik.String())
	}
}

// Kad gašenje već čeka (npr. Ctrl+C), zatvoren ulaz ne blokira
func TestZatvorenUlazNeBlokiraKadGasenjeVecCeka(t *testing.T) {
	stop := make(chan os.Signal, 1)
	stop <- os.Interrupt
	gotovo := make(chan struct{})
	go func() {
		cekajZatvaranjeUlaza(strings.NewReader(""), stop)
		close(gotovo)
	}()
	select {
	case <-gotovo:
	case <-time.After(2 * time.Second):
		t.Fatal("čekanje ulaza je blokiralo na punom kanalu gašenja")
	}
}

func slobodanPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
