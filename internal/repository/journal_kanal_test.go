package repository

import (
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Dnevnik bez upisane godine dobiva kanal po godini početka, a bez početka
// po godini upisa, uvijek po zagrebačkom vremenu. Vrijeme upisa iz baze se
// čita u UTC-u, a početak u UTC-u čuva i čvor bez mjesne zone, pa je dnevnik
// od 1. 1. 2027. znao otići u kanal 2026., ponekad tek nakon izmjene
// naslovnice.
func TestKanalDnevnikaPoGodiniZagreba(t *testing.T) {
	n := noviCvor(t, "posluzitelj")
	r := NewJournalRepository(n.db, n.rec)
	novaGodina := time.Date(2027, time.January, 1, 0, 30, 0, 0, models.Zagreb)
	pocetakUTC := novaGodina.UTC()
	for _, j := range []*models.Journal{
		{AreaID: 16, Kind: models.JournalKindDefense, Title: "obrana od poplava", StartedAt: &pocetakUTC},
		{AreaID: 16, Kind: models.JournalKindDefense, Title: "dnevnik bez početka", CreatedAt: novaGodina},
	} {
		nuzno(t, r.SaveJournal(ctxRaz, j))
		procitan, err := r.GetJournal(ctxRaz, j.ID)
		nuzno(t, err)
		procitan.Notes = "izmjena naslovnice"
		nuzno(t, r.SaveJournal(ctxRaz, procitan))
		if _, err := r.ArhivirajDnevnik(ctxRaz, procitan); err != nil {
			t.Fatal(err)
		}
		kanal := ledger.ChannelFor(ledger.ChannelJournals, 16, 2027)
		upit := `SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`
		if u, izvan := brojRedaka(t, n.db, upit, EntityJournals, j.ID), brojRedaka(t, n.db, upit+` AND channel <> ?`, EntityJournals, j.ID, kanal); u != 3 || izvan != 0 {
			t.Errorf("%s: od %d verzija %d je izvan kanala %s", j.Title, u, izvan, kanal)
		}
	}
}
