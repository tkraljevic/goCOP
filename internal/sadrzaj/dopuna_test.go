package sadrzaj

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Spremište iz vremena prije kanala dobije stupce i kazalo po kanalu, a
// traženje po kanalu ide kazalom, ne čitanjem svakog dokumenta. Greške
// dopune javljaju se, ne prešućuju.
func TestDopunaStarogSpremista(t *testing.T) {
	put := filepath.Join(t.TempDir(), "sadrzaj.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE sadrzaj (otisak TEXT PRIMARY KEY, vrsta TEXT NOT NULL, bajtova INTEGER NOT NULL,
			podaci BLOB NOT NULL, primljeno DATETIME NOT NULL, izvor TEXT NOT NULL DEFAULT 'ovdje')`,
		`CREATE TABLE sadrzaj_zeljen (otisak TEXT PRIMARY KEY, vrsta TEXT NOT NULL, bajtova INTEGER NOT NULL,
			trazeno DATETIME, razlog TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Otvori(put)
	if err != nil {
		t.Fatalf("staro spremište: %v", err)
	}
	defer s.Zatvori() //nolint:errcheck // proba, zatvaranje nije predmet
	var plan, id, roditelj, nista string
	if err := s.db.QueryRow(`EXPLAIN QUERY PLAN SELECT otisak FROM sadrzaj WHERE kanal = 'ocitanja/1/2026'`).Scan(&id, &roditelj, &nista, &plan); err != nil {
		t.Fatal(err)
	}
	if plan != "SEARCH sadrzaj USING INDEX idx_sadrzaj_kanal (kanal=?)" {
		t.Errorf("traženje po kanalu: %q", plan)
	}

	bez, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bez.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bez.Close()
	if err := dopuniSpremiste(bez); err == nil {
		t.Error("spremište bez tablica: nema greške stupca")
	}
	if _, err := bez.Exec(`CREATE TABLE sadrzaj (kanal TEXT); CREATE TABLE sadrzaj_zeljen (kanal TEXT);
		CREATE TABLE idx_sadrzaj_kanal (x)`); err != nil {
		t.Fatal(err)
	}
	if err := dopuniSpremiste(bez); err == nil {
		t.Error("ime kazala zauzeto: nema greške kazala")
	}
	if err := bez.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dopuniSpremiste(bez); err == nil {
		t.Error("zatvorena baza: nema greške")
	}
}
