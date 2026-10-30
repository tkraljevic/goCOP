package arhiva

import (
	"path/filepath"
	"testing"

	"gocop/internal/trajno"
)

// Zaključava da upis niza u stablo arhive spusti datoteku na disk prije
// preimenovanja i mapu nakon njega. Bez toga je nestanak struje mogao
// ostaviti prazan niz pod pravim imenom ili stari niz uz novi.
func TestUpisNizaSpustaNaDisk(t *testing.T) {
	popis, prestani := trajno.Prati()
	defer prestani()
	put, err := Upisi(t.TempDir(), "dunav", "batina", "his2000", "vodostaj", "satni", redakZaUpis())
	if err != nil {
		t.Fatal(err)
	}
	if mapa := filepath.Dir(put); !popis.ImaUMapi(mapa) || !popis.Ima(mapa) {
		t.Error("niz ili mapa niza nije spuštena na disk")
	}
}
