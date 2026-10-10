package repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Dionice objekta: po identifikatoru ili nazivu (bez obzira na razmake i
// velika slova), samo na zadanom području; prazan upis ne zadaje objekt, a
// nečitljiva baza je greška
func TestDioniceObjekta(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewStructureRepository(n.db, n.rec)
	sad := time.Now().UTC()
	nuzno(t, izvrsiSQL(n.db, `INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (17, 'B', 'Područje 17', 'VGI')`))
	nuzno(t, izvrsiSQL(n.db, `INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.16.2', 16, 'B', 'druga', ?, ?), ('B.17.1', 17, 'B', 'treća', ?, ?)`, sad, sad, sad, sad))
	objekt := func(naziv string, podrucje int, dionice ...string) string {
		t.Helper()
		id := uuid.NewString()
		nuzno(t, izvrsiSQL(n.db, `INSERT INTO structures (id, code, name, kind, sector_id, area_id, created_at, updated_at) VALUES (?, ?, ?, 'CRPNA_STANICA', 'B', ?, ?, ?)`, id, "o-"+id, naziv, podrucje, sad, sad))
		for _, d := range dionice {
			nuzno(t, izvrsiSQL(n.db, `INSERT INTO section_structures (id, section_code, structure_id, created_at) VALUES (?, ?, ?, ?)`, uuid.NewString(), d, id, sad))
		}
		return id
	}
	crpna := objekt("Crpna stanica Prva", 16, osnovaDionica, "B.16.2")
	objekt("Crpna stanica Prva", 17, "B.17.1")
	objekt("Ustava Prva", 16)
	for _, s := range []struct {
		ime, id, naziv string
		dionice        string
	}{
		{"po identifikatoru", crpna, "", "B.16.1,B.16.2"},
		{"po nazivu", "", "  crpna STANICA prva ", "B.16.1,B.16.2"},
		{"objekt bez dionica", "", "Ustava Prva", ""},
		{"nepoznat objekt", "", "Ustava Druga", ""},
		{"bez upisa", "", "", ""},
	} {
		got, err := repo.DioniceObjekta(ctxRaz, 16, s.id, s.naziv)
		if err != nil || strings.Join(got, ",") != s.dionice {
			t.Errorf("%s: %v %v, očekivano %s", s.ime, got, err, s.dionice)
		}
	}
	nuzno(t, izvrsiSQL(n.db, `DROP TABLE section_structures`))
	if _, err := repo.DioniceObjekta(ctxRaz, 16, crpna, ""); err == nil {
		t.Error("bez greške uz nečitljivu bazu")
	}
}

func izvrsiSQL(baza *sql.DB, q string, args ...any) error {
	_, err := baza.Exec(q, args...)
	return err
}
