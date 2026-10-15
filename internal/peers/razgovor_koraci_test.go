package peers

import (
	"encoding/json"
	"testing"
)

// Poruka koja se ne da zapisati kao JSON ne ide na žicu: posalji vrati
// grešku zapisa neomotanu, prije nego što dirne vezu ili čvor. Sve poruke
// razgovora danas se daju zapisati, pa ovu granu razgovor ne pogodi.
func TestPosaljiNeZapisivuPoruku(t *testing.T) {
	r := &razgovorSCvorom{}
	err := r.posalji(kindDone, func() {})
	// tip se provjerava izravno, a ne s errors.As, jer errors.As prođe i
	// kroz omotanu grešku
	if _, ok := err.(*json.UnsupportedTypeError); !ok {
		t.Fatalf("greška %v (%T), a očekivana je neomotana greška zapisa JSON-a", err, err)
	}
}
