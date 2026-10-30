package repository

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Primljeni zapis čvora na površinu dolazi s imenom do 128 bajtova i s
// najviše 16 adresa od najviše 256 bajtova; običan zapis ostaje isti
func TestPrimljeniZapisCvoraOgradjen(t *testing.T) {
	b := noviCvor(t, "laptop-baranja")
	adrese := []string{"192.168.24.70:4710", strings.Repeat("a", 300)}
	for i := range 30 {
		adrese = append(adrese, fmt.Sprintf("10.0.0.%d:4710", i))
	}
	i := 0
	for _, z := range []struct {
		id, ime string
		adrese  []string
	}{{"ured-vukovar", strings.Repeat("ž", 100), adrese}, {"cop-osijek", "COP Osijek", []string{"192.168.24.5:4710"}}} {
		i++
		payload, err := json.Marshal(map[string]any{"node_id": z.id, "name": z.ime, "public_key": "k", "addresses": z.adrese, "created_at": time.Now()})
		nuzno(t, err)
		primi(t, b, []ledger.Version{{VersionID: fmt.Sprintf("0190a000-0000-7000-8000-%012d", i), Entity: "peers", EntityID: z.id, NodeID: z.id, Payload: payload, CreatedAt: time.Now(), SchemaVersion: 1}})
	}
	var ime, sAdresama string
	nuzno(t, b.db.QueryRow(`SELECT name, addresses FROM peers WHERE node_id = 'ured-vukovar'`).Scan(&ime, &sAdresama))
	var spremljene []string
	nuzno(t, json.Unmarshal([]byte(sAdresama), &spremljene))
	if len(ime) != 128 || len(spremljene) != 16 || spremljene[0] != adrese[0] || spremljene[1] != adrese[2] {
		t.Errorf("ime %d bajtova, adrese %d: %v", len(ime), len(spremljene), spremljene[:2])
	}
	nuzno(t, b.db.QueryRow(`SELECT name, addresses FROM peers WHERE node_id = 'cop-osijek'`).Scan(&ime, &sAdresama))
	if ime != "COP Osijek" || sAdresama != `["192.168.24.5:4710"]` {
		t.Errorf("običan zapis: %s %s", ime, sAdresama)
	}
	if skratiTekst("abc", 5) != "abc" || skratiTekst("čć", 3) != "č" {
		t.Error("skraćivanje")
	}
}
