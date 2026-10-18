package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava spremanje datoteke iz obrasca: upisana je cijela i zatvorena, a
// datoteka u koju se ne da pisati javlja grešku umjesto da se poslije čita
// kao spremljena.
func TestSpremiUcitano(t *testing.T) {
	put := filepath.Join(t.TempDir(), "paket.cop")
	out, err := os.Create(put)
	if err != nil {
		t.Fatal(err)
	}
	n, err := spremiUcitano(out, strings.NewReader("sadržaj paketa"))
	if err != nil || n != int64(len("sadržaj paketa")) {
		t.Fatalf("spremljeno %d bajtova, greška %v", n, err)
	}
	b, err := os.ReadFile(put)
	if err != nil || string(b) != "sadržaj paketa" {
		t.Errorf("na disku %q, greška %v", b, err)
	}
	if err := out.Close(); err == nil {
		t.Error("datoteka nije zatvorena")
	}
	// Datoteka je već zatvorena, pa pisanje ne uspije.
	if _, err := spremiUcitano(out, strings.NewReader("x")); err == nil {
		t.Error("neuspjelo spremanje javljeno je kao uspjelo")
	}
}
