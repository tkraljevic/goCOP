package repository

import (
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/peers"
)

// Opoziv vrijedi i kad iza njegove žive verzije stoji arhivirana: opoziv se
// ne poništava, pa ga ni razmjena ni obnova ne smiju izostaviti iz tablice.
func TestOpozivSArhiviranomVerzijomOstajeUTablici(t *testing.T) {
	const id = "01a00000-0000-7000-8000-00000000f001"
	op := peers.Opoziv{ID: id, Vrsta: peers.OpozivClanstva, NodeID: "cop-proba-opozvani", PublicKey: "kljuc-probe",
		IssuedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), OpozvanoAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}
	ziva := verzijaZa(t, peers.EntityOpozivi, id, vid(1), op)
	ponistenje := verzijaZa(t, peers.EntityOpozivi, id, vid(2), op)
	ponistenje.Archived = true

	t.Run("razmjena", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		nuzno(t, primiPaket(t, c, ziva, ponistenje))
		if !imaRedak(t, c, "opozivi", id) {
			t.Error("opoziv nije u tablici kad ga razmjena donese s arhiviranom verzijom")
		}
	})
	t.Run("obnova", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		samoUKnjigu(t, c, []ledger.Version{ziva, ponistenje})
		o, err := ObnoviPovrsinu(ctxRaz, c.db, c.rec)
		nuzno(t, err)
		if o.Upisano[peers.EntityOpozivi] != 1 || !imaRedak(t, c, "opozivi", id) {
			t.Errorf("obnova nije vratila opoziv: %s", o)
		}
	})
	// bez žive verzije nema opoziva koji bi se upisao
	t.Run("samo arhivirana", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "cop-proba")
		nuzno(t, primiPaket(t, c, ponistenje))
		if imaRedak(t, c, "opozivi", id) {
			t.Error("arhivirana verzija bez žive upisala je opoziv")
		}
	})
}
