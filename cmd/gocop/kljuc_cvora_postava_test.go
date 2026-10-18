package main

// Čvor bez ključa pod Postavom (Odluke (8. 10. 2026.), plan izdanja 0.0.35,
// stavka 6). Postava se ne mijenja s goCOP-om (ugovor u docs/postava.md):
// pri nadogradnji vraća prethodno izdanje kad novo ne odgovori na
// /zdravlje ili tri puta padne, a izdanja prije 0.0.35 uz postojeću bazu bez
// ključa tiho naprave novi ključ, dakle novi identitet. Zato čvor koji
// odbije pokretanje pod Postavom ne izlazi, nego čeka da ga ona zaustavi.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/postava"
)

// zapisnikPostave skuplja dnevnik Postave; piše se i iz njezinih gorutina
// nakon kraja testa, pa ne smije ići kroz t.Logf
type zapisnikPostave struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (z *zapisnikPostave) pisi(f string, a ...any) {
	z.mu.Lock()
	defer z.mu.Unlock()
	fmt.Fprintf(&z.b, f+"\n", a...)
}

func (z *zapisnikPostave) String() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.b.String()
}

// Nadogradnja Postavom na čvoru čija baza ima zapise, a ključa nema: novo
// izdanje odbije pokretanje, ali ostane upaljeno i odgovori na /zdravlje,
// pa Postava ne vrati prethodno izdanje, koje bi napravilo novi ključ.
// Prethodno izdanje glumi skripta koja radi upravo to.
func TestNadogradnjaPostavomNaCvoruBezKljuca(t *testing.T) {
	if testing.Short() {
		t.Skip("prevodi program i nadograđuje ga Postavom")
	}
	if runtime.GOOS == "windows" {
		t.Skip("prethodno izdanje glumi skripta ljuske")
	}
	m := postava.MjestaU(t.TempDir())
	for _, mapa := range []string{m.Podaci, filepath.Dir(m.Novi())} {
		if err := os.MkdirAll(mapa, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(bazaZaOporavak(t), m.BazaCvora()); err != nil {
		t.Fatal(err)
	}
	port := slobodanPort(t)
	postavke := fmt.Sprintf("addr = '127.0.0.1:%d'\n\n[node]\nid = 'pperic-thinkpad'\n\n[sync]\nexchange_port = 0\npair_port = 0\ndiscovery_port = 0\nauto_sync = '0'\n", port)
	if err := os.WriteFile(m.PostavkeCvora(), []byte(postavke), 0o644); err != nil {
		t.Fatal(err)
	}
	prethodno := "#!/bin/sh\n# kao izdanja prije 0.0.35: uz bazu bez ključa napravi novi\n[ -f node-key ] || echo novi > node-key\nexec cat > /dev/null\n"
	if err := os.WriteFile(m.Gocop(), []byte(prethodno), 0o755); err != nil {
		t.Fatal(err)
	}
	gradnja := exec.Command("go", "build", "-o", m.Novi(), ".")
	if out, err := gradnja.CombinedOutput(); err != nil {
		t.Fatalf("prevođenje: %v\n%s", err, out)
	}

	var zapisnik zapisnikPostave
	cvor := postava.NoviCvor(m, zapisnik.pisi)
	t.Cleanup(func() { _ = cvor.Zaustavi(10 * time.Second) })
	u := postava.Ugradnja{M: m, Cvor: cvor, Pisi: zapisnik.pisi}
	ctx, otkazi := context.WithTimeout(context.Background(), 2*time.Minute)
	defer otkazi()
	if err := u.Ugradi(ctx, m.Novi(), "0.0.34-alfa", verzijaPrograma); err != nil {
		t.Errorf("Postava je nadogradnju proglasila neuspjelom: %v\n%s", err, zapisnik.String())
	}

	// ploča na ovom računalu kaže zašto čvor ne radi
	if odg, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		t.Errorf("ploča: %v", err)
	} else {
		tekst, _ := io.ReadAll(odg.Body)
		odg.Body.Close()
		if odg.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(tekst), filepath.Join(m.Podaci, "node-key")) {
			t.Errorf("ploča: %d %q", odg.StatusCode, tekst)
		}
	}

	// Postava ga zaustavlja zatvaranjem ulaza, kao i čvor koji radi
	if err := cvor.Zaustavi(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Podaci, "node-key")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("uz bazu je napravljen ključ čvora, dakle novi identitet (%v)", err)
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
		c.Close()
		t.Error("zaustavljen čvor i dalje drži port")
	}
	dnevnik, _ := os.ReadFile(m.DnevnikCvora())
	for _, mora := range []string{"Čvor se ne pokreće", "ostaje upaljen", "Standardni ulaz zatvoren"} {
		if !bytes.Contains(dnevnik, []byte(mora)) {
			t.Errorf("u dnevniku čvora nema %q:\n%s", mora, dnevnik)
		}
	}
}
