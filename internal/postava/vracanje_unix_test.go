//go:build darwin || linux

package postava

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/izdanje"
)

// procitajKljuc čeka da lažni čvor zapiše ključ koji je dobio u okolini
func procitajKljuc(t *testing.T, put string) string {
	t.Helper()
	for i := 0; i < 500; i++ {
		if b, err := os.ReadFile(put); err == nil && len(b) > 0 {
			_ = os.Remove(put) // idući krug čeka novi zapis
			return string(b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("lažni čvor nije zapisao ključ")
	return ""
}

// Postava vlastitom čvoru pri svakom pokretanju da novi ključ za potpis
// dopuštenja, kroz okolinu, i pamti ga za provjeru /zdravlje
func TestCvorDobivaKljucPriPokretanju(t *testing.T) {
	m := MjestaU(t.TempDir())
	if err := os.MkdirAll(m.Program, 0o755); err != nil {
		t.Fatal(err)
	}
	skripta := "#!/bin/sh\ncase \"$1\" in\n-upravitelj) printf '%s' \"$" + izdanje.OkolinaKljucaPostave + "\" > kljuc.txt; exec sleep 30;;\nesac\n"
	if err := os.WriteFile(m.Gocop(), []byte(skripta), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(izdanje.OkolinaKljucaPostave, "stari-iz-okoline-postave")
	c := NoviCvor(m, nil)
	t.Cleanup(func() {
		if err := c.Zaustavi(50 * time.Millisecond); err != nil {
			t.Error(err)
		}
	})
	var kljucevi []string
	for i := 0; i < 2; i++ {
		if err := c.Pokreni(); err != nil {
			t.Fatal(err)
		}
		k := procitajKljuc(t, filepath.Join(m.Podaci, "kljuc.txt"))
		c.mu.Lock()
		zapamcen := c.kljuc
		c.mu.Unlock()
		if k != zapamcen || len(k) < 20 || k == "stari-iz-okoline-postave" {
			t.Fatalf("pokretanje %d: čvor dobio %q, Postava pamti %q", i+1, k, zapamcen)
		}
		kljucevi = append(kljucevi, k)
		if err := c.Zaustavi(50 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if kljucevi[0] == kljucevi[1] {
		t.Error("ključ se ne mijenja pri ponovnom pokretanju")
	}
}
