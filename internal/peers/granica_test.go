package peers

import (
	"testing"

	"gocop/internal/ledger"
)

// Ploča broji što drugi čvor još nema od granice zapamćene u razmjeni. Ta
// granica mora uključiti i poslano u istoj razmjeni, inače Unraid pokazuje
// „šalje se još 101 verzija” odmah nakon što ih je poslao.
func TestGranicaUkljucujePoslano(t *testing.T) {
	stara := map[string]string{"unraid": "0001", "unraid|letva/osijek": "0005"}
	nova := pomakniGranicu(stara, []ledger.Version{
		{VersionID: "0003", NodeID: "unraid"},
		{VersionID: "0002", NodeID: "unraid"},
		{VersionID: "0004", NodeID: "unraid", Channel: "letva/osijek"},
		{VersionID: "0009", NodeID: "unraid", Channel: "arhiva"},
	})
	for k, ocekivano := range map[string]string{"unraid": "0003", "unraid|letva/osijek": "0005", "unraid|arhiva": "0009"} {
		if nova[k] != ocekivano {
			t.Errorf("%s: %q, očekivano %q", k, nova[k], ocekivano)
		}
	}
	if stara["unraid"] != "0001" {
		t.Error("stara granica se ne smije mijenjati")
	}
}
