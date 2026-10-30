package his2000

import (
	"path/filepath"
	"testing"

	"gocop/internal/trajno"
)

// Zaključava da se izvornik i datoteka krivulja pišu sa strane i spuštaju na
// disk. Datoteka krivulja pisala se izravno preko stare, pa je nestanak
// struje usred upisa mogao ostaviti napola zapisanu krivulju.
func TestKrivuljeSeZapisujuTrajno(t *testing.T) {
	s := izvozKrivulja("5150", godina(2001, 2001, "7"))
	s.Ime = "krivulje.csv"
	hq := filepath.Join(t.TempDir(), "hq")
	popis, prestani := trajno.Prati()
	defer prestani()
	if _, err := ZapisiKrivulje(filepath.Join(hq, "letva_hq_krivulje.csv"), []byte("csv"), s, []byte("izvoz")); err != nil {
		t.Fatal(err)
	}
	izvornik := filepath.Join(hq, "izvornik")
	if !popis.ImaUMapi(hq) || !popis.Ima(hq) || !popis.ImaUMapi(izvornik) || !popis.Ima(izvornik) {
		t.Error("krivulje ili izvornik nisu spušteni na disk")
	}
}
