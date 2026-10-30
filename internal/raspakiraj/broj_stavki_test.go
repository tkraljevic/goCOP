package raspakiraj

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// zipSaStavki slaže ZIP s n praznih stavki.
func zipSaStavki(t *testing.T, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := range n {
		if _, err := zw.Create(string(rune('a' + i%26))); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// ZIP s više stavki od granice odbija se; s točno toliko prolazi. Potpis
// zaglavlja koji se proteže preko dva čitanja broji se jednom.
func TestProvjeriBrojStavki(t *testing.T) {
	z := zipSaStavki(t, 5)
	for _, velicina := range []int{1 << 20, 5, 7} {
		velicinaCitanja = velicina
		if err := ProvjeriBrojStavki(bytes.NewReader(z), int64(len(z)), 5); err != nil {
			t.Errorf("čitanje po %d: pet stavki uz granicu pet: %v", velicina, err)
		}
		if err := ProvjeriBrojStavki(bytes.NewReader(z), int64(len(z)), 4); err == nil || !strings.Contains(err.Error(), "najviše 4") {
			t.Errorf("čitanje po %d: pet stavki uz granicu četiri: %v", velicina, err)
		}
	}
	velicinaCitanja = 1 << 20
	if err := ProvjeriBrojStavki(bytes.NewReader(nil), 0, 0); err != nil {
		t.Errorf("prazna datoteka: %v", err)
	}
}

type pokvareniCitac struct{}

var errCitanja = errors.New("disk")

func (pokvareniCitac) ReadAt([]byte, int64) (int, error) { return 0, errCitanja }

// Greška čitanja datoteke vraća se.
func TestProvjeriBrojStavkiGreskaCitanja(t *testing.T) {
	if err := ProvjeriBrojStavki(pokvareniCitac{}, 100, 5); !errors.Is(err, errCitanja) {
		t.Errorf("greška čitanja: %v", err)
	}
}
