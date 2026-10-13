package repository

import (
	"encoding/json"
	"errors"
	"testing"

	"gocop/internal/ledger"
)

// Svaki entitet ima primjenu u samo jednoj skupini. Inače bi spajanje
// skupina tiho uzelo jednu od dviju, ovisno o redu skupina.
func TestPrimjenaJeUJednojSkupini(t *testing.T) {
	skupina := map[string]int{}
	for i, s := range skupinePrimjena {
		for e := range s {
			if j, ima := skupina[e]; ima {
				t.Errorf("entitet %s ima primjenu u skupinama %d i %d", e, j, i)
			}
			skupina[e] = i
		}
	}
	if len(primjene) != len(skupina) {
		t.Errorf("primjena je %d, a entiteta u skupinama %d", len(primjene), len(skupina))
	}
}

// Članstvo čiju ovlast primatelja ne treba zapisati u UTC-u (vrijeme koje
// preračunato u UTC pada izvan godina 0–9999) ne ide na površinu: primjena
// vraća grešku i ne upisuje članstvo bez ovlasti.
func TestClanstvoSOvlascuKojaSeNeDaZapisati(t *testing.T) {
	c := noviCvor(t, "b")
	sadrzaj := []byte(`{"network": "probna-mreza", "deviceId": "cvor-probni", "deviceKey": "a2xqdWM=",
		"issuedBy": "cvor-osnivac", "issuedAt": "2026-10-13T08:00:00Z", "expiresAt": "2027-10-13T08:00:00Z",
		"signature": "cG90cGlz",
		"primatelj": {"network": "probna-mreza", "deviceId": "cvor-ovlasteni", "deviceKey": "a2xqdWM=",
			"issuedBy": "cvor-osnivac", "issuedAt": "9999-12-31T23:30:00-01:00", "expiresAt": "9999-12-31T23:30:00-01:00",
			"signature": "cG90cGlz"}}`)
	err := primijeniUTx(t, c, ledger.Version{Entity: "memberships", EntityID: "cvor-probni", Payload: sadrzaj})
	var zapis *json.MarshalerError
	if !errors.As(err, &zapis) {
		t.Fatalf("%v, očekivana greška zapisa ovlasti", err)
	}
	if n := redakaNaPovrsini(t, c, "memberships"); n != 0 {
		t.Errorf("na površini %d članstava", n)
	}
}

// Kad se zapis ne da pretvoriti u argumente upita, upis po argumentima
// vraća tu grešku i ne upisuje ništa.
func TestUpisPoArgumentimaJavljaGreskuArgumenata(t *testing.T) {
	c := noviCvor(t, "b")
	kriva := errors.New("sadržaj izvješća se ne da zapisati")
	upis := upisPoArgumentima(izvjesceUpsert, func(*struct{ ID string }) ([]any, error) { return nil, kriva })
	tx, err := c.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	if err := upis(ctxRaz, tx, ledger.Version{Entity: EntityDnevnaIzvjesca, EntityID: "izvjesce-1", Payload: []byte(`{"ID": "izvjesce-1"}`)}); !errors.Is(err, kriva) {
		t.Fatalf("%v, očekivana greška argumenata", err)
	}
	nuzno(t, tx.Commit())
	if n := redakaNaPovrsini(t, c, "dnevna_izvjesca"); n != 0 {
		t.Errorf("na površini %d izvješća", n)
	}
}
