package repository

import (
	"testing"

	"gocop/internal/ledger"
)

// Spomenici koje primjena prima rijetko: veza letve i dionice iz starije
// knjige, bez sadržaja, nosi dionicu i letvu u identitetu zapisa; spomenik
// opoziva ne poništava opoziv, a spomenik entiteta koji ovaj program ne zna
// ne dira površinu.
func TestRijetkiSpomenici(t *testing.T) {
	c := noviCvor(t, "teren")
	_, err := c.db.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at)
		VALUES ('veza', ?, ?, '2026-01-01')`, osnovaDionica, osnovaLetva.String())
	nuzno(t, err)
	tx, err := c.db.Begin()
	nuzno(t, err)
	defer tx.Rollback()
	for _, v := range []ledger.Version{
		{Entity: EntitySectionStations, EntityID: osnovaDionica + "|" + osnovaLetva.String(), Archived: true, Payload: []byte(`{}`)},
		{Entity: "opozivi", EntityID: "cvor-opozvan", Archived: true, Payload: []byte(`{}`)},
		{Entity: "nepoznati_entitet", EntityID: "1", Archived: true, Payload: []byte(`{}`)},
	} {
		if err := applyOne(ctxRaz, tx, v); err != nil {
			t.Errorf("spomenik %s: %v", v.Entity, err)
		}
	}
	nuzno(t, tx.Commit())
	if n := brojRedaka(t, c.db, `SELECT count(*) FROM section_stations`); n != 0 {
		t.Errorf("veza letve i dionice ostala je na površini (%d)", n)
	}
}
