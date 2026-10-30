package web

import (
	"testing"

	"gocop/internal/peers"
)

// Potvrde prava poslužitelja potpisuju razmjenom kad je ima, a bez nje
// nemaju potpis
func TestPotvrdePravaPosluzitelja(t *testing.T) {
	if p := (&Server{}).potvrdePrava(); p.potpis != nil || p.baza() != nil {
		t.Error("bez razmjene potvrde imaju potpis")
	}
	if p := (&Server{peersService: &peers.Service{}}).potvrdePrava(); p.potpis == nil {
		t.Error("s razmjenom potvrde nemaju potpis")
	}
}
