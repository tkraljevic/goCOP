package izvori

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Uvoz godišnjaka uz kontekst prekida se kad kontekst istekne, a pdftotext
// se gasi.
func TestUvozGodisnjakaSPrekidom(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("lažni pdftotext je skripta ljuske")
	}
	mapa := t.TempDir()
	if err := os.WriteFile(filepath.Join(mapa, "pdftotext"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", mapa)
	f, _ := Nadji("godisnjak")
	ctx, prekini := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer prekini()
	pocetak := time.Now()
	_, err := f.UveziUz(ctx, []Datoteka{{Ime: "1991.pdf", Sadrzaj: []byte("%PDF")}}, Zadatak{Koren: t.TempDir(), Postaje: "42010=bezdan"}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("uvoz s isteklim kontekstom: %v", err)
	}
	if trajalo := time.Since(pocetak); trajalo > 4*time.Second {
		t.Errorf("prekid je trajao %v", trajalo)
	}
}
