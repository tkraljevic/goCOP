//go:build darwin || linux || windows

package disk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Pravi disk: dvije mape na istom disku daju istu oznaku, a mapa koje nema
// grešku koju nadzor preskače
func TestSustavMjeriPraviDisk(t *testing.T) {
	koren := t.TempDir()
	a, b := filepath.Join(koren, "baza"), filepath.Join(koren, "pakete")
	for _, p := range []string{a, b} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ma, err := Sustav{}.Izmjeri(a)
	if err != nil {
		t.Fatal(err)
	}
	if ma.Disk == "" || ma.Ukupno == 0 || ma.Slobodno > ma.Ukupno {
		t.Errorf("mjera %+v", ma)
	}
	mb, err := Sustav{}.Izmjeri(b)
	if err != nil {
		t.Fatal(err)
	}
	if mb.Disk != ma.Disk {
		t.Errorf("isti disk, različite oznake: %q i %q", ma.Disk, mb.Disk)
	}
	if _, err := (Sustav{}).Izmjeri(filepath.Join(koren, "nema")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("mapa koje nema: %v", err)
	}
}
