package repository

import (
	"context"
	"testing"

	"gocop/internal/ledger"
)

// Brisanje nacrta u svim kanalima zapisa vraća grešku, i kad se kanali ne
// daju pročitati i kad se spomenik ne da upisati; ništa se ne prešuti.
func TestArhivirajUKanalimaZapisaGreske(t *testing.T) {
	c := noviCvor(t, "posluzitelj")
	kanal := ledger.ChannelFor(ledger.ChannelPrijave, 16, 2026)

	t.Run("kanali se ne daju pročitati", func(t *testing.T) {
		tx, err := c.db.BeginTx(ctxRaz, nil)
		nuzno(t, err)
		defer tx.Rollback()
		otkazan, otkazi := context.WithCancel(ctxRaz)
		otkazi()
		if err := arhivirajUKanalimaZapisa(otkazan, tx, c.rec, kanal, EntityPrijave, "nacrt-1", struct{}{}); err == nil {
			t.Fatal("otkazano čitanje kanala nije javilo grešku")
		}
	})

	t.Run("spomenik se ne da upisati", func(t *testing.T) {
		tx, err := c.db.BeginTx(ctxRaz, nil)
		nuzno(t, err)
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctxRaz, `CREATE TEMP TRIGGER zabrana BEFORE INSERT ON record_versions BEGIN SELECT RAISE(ABORT, 'upis zabranjen'); END`); err != nil {
			t.Fatal(err)
		}
		if err := arhivirajUKanalimaZapisa(ctxRaz, tx, c.rec, kanal, EntityPrijave, "nacrt-2", struct{}{}); err == nil {
			t.Fatal("odbijen upis spomenika nije javio grešku")
		}
	})
}
