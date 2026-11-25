package web

import (
	"path/filepath"
	"testing"

	"gocop/internal/prognoza"
)

// Broj izdanja preskakanjem kroz ključ isti je kao count(DISTINCT izdano):
// sat izdanja koji ima samo jedna letva ili samo protok broji se jednom, isti
// sat na više letava i veličina također jednom; prazna tablica daje nulu.
func TestBrojIzdanjaIstiKaoDistinct(t *testing.T) {
	db, err := prognoza.Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := &CitacPrognoza{db: db}
	if _, n := c.Brojke(); n != 0 {
		t.Fatalf("prazna baza: %d izdanja", n)
	}
	upisi := func(letva, velicina string, izdano int64) {
		t.Helper()
		for ciljni := izdano; ciljni < izdano+3; ciljni++ {
			if _, err := db.Exec(`INSERT INTO izdane (letva, velicina, izdano, ciljni, vrijednost, dolje, gore, model)
				VALUES (?, ?, ?, ?, 100, 90, 110, 'proba')`, letva, velicina, izdano, ciljni); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, letva := range []string{"a-letva", "b-letva", "c-letva"} {
		for sat := int64(1000); sat < 1010; sat++ {
			upisi(letva, "vodostaj", sat)
		}
	}
	upisi("b-letva", "protok", 1003)
	upisi("b-letva", "protok", 2000) // samo protok jedne letve
	upisi("c-letva", "vodostaj", 999)
	upisi("z-letva", "vodostaj", 5)
	var ocekivano int
	if err := db.QueryRow(`SELECT count(DISTINCT izdano) FROM izdane`).Scan(&ocekivano); err != nil {
		t.Fatal(err)
	}
	if _, n := c.Brojke(); n != ocekivano || n != 13 {
		t.Errorf("izdanja %d, count(DISTINCT) %d, očekivano 13", n, ocekivano)
	}
}
