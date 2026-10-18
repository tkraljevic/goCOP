package peers

import (
	"testing"

	"gocop/internal/ledger"
)

// Ploča broji što drugi čvor još nema od granice zapamćene u razmjeni. Ta
// granica mora uključiti i poslano u istoj razmjeni, inače server pokazuje
// „šalje se još 101 verzija” odmah nakon što ih je poslao.
func TestGranicaUkljucujePoslano(t *testing.T) {
	stara := map[string]string{"server": "0001", "server|letva/osijek": "0005"}
	nova := pomakniGranicu(stara, []ledger.Version{
		{VersionID: "0003", NodeID: "server"},
		{VersionID: "0002", NodeID: "server"},
		{VersionID: "0004", NodeID: "server", Channel: "letva/osijek"},
		{VersionID: "0009", NodeID: "server", Channel: "arhiva"},
	})
	for k, ocekivano := range map[string]string{"server": "0003", "server|letva/osijek": "0005", "server|arhiva": "0009"} {
		if nova[k] != ocekivano {
			t.Errorf("%s: %q, očekivano %q", k, nova[k], ocekivano)
		}
	}
	if stara["server"] != "0001" {
		t.Error("stara granica se ne smije mijenjati")
	}
}
