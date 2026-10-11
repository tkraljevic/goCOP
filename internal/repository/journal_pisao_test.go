package repository

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Osoba je pisala u dnevnik kad je u njemu otvorila list ili upisala zapis
// (i storniran); list ili zapis drugoga i drugi dnevnik se ne broje, a
// nečitljiva baza je greška
func TestPisaoUDnevnik(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewJournalRepository(n.db, n.rec)
	dan := time.Date(2026, 10, 8, 0, 0, 0, 0, models.Zagreb)
	const pero, ana, ivo = "pero", "ana", "ivo"
	nuzno(t, repo.SaveSheet(ctxRaz, &models.JournalSheet{JournalID: osnovaDnevnik, Date: dan, CreatedBy: pero}))
	nuzno(t, repo.SaveEntry(ctxRaz, &models.JournalEntry{JournalID: osnovaDnevnik, Date: dan, Kind: models.EntryKindNote, Text: "napomena", UserID: ana, Voided: true}))
	for _, x := range []struct {
		dnevnik, osoba string
		pisao          bool
	}{{osnovaDnevnik, pero, true}, {osnovaDnevnik, ana, true}, {osnovaDnevnik, ivo, false}, {"drugi", pero, false}} {
		got, err := repo.PisaoUDnevnik(ctxRaz, x.dnevnik, x.osoba)
		if err != nil || got != x.pisao {
			t.Errorf("%s u %s: %v %v, očekivano %v", x.osoba, x.dnevnik, got, err, x.pisao)
		}
	}
	if _, err := n.db.Exec(`DROP TABLE journal_entries`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PisaoUDnevnik(ctxRaz, osnovaDnevnik, ivo); err == nil {
		t.Error("bez greške uz nečitljivu bazu")
	}
}
