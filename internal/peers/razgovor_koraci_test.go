package peers

import (
	"encoding/json"
	"errors"
	"testing"
)

// Poruka koja se ne da zapisati kao JSON ne ide na žicu: posalji vrati
// grešku zapisa neomotanu, prije nego što dirne vezu ili čvor. Sve poruke
// razgovora danas se daju zapisati, pa ovu granu razgovor ne pogodi.
func TestPosaljiNeZapisivuPoruku(t *testing.T) {
	r := &razgovorSCvorom{}
	err := r.posalji(kindDone, func() {})
	var nepodrzan *json.UnsupportedTypeError
	if !errors.As(err, &nepodrzan) {
		t.Fatalf("greška %v, a očekivana je greška zapisa JSON-a", err)
	}
}
