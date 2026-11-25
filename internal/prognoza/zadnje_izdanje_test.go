package prognoza

import (
	"path/filepath"
	"testing"
)

// Zadnje izdanje preskakanjem kroz ključ isto je kao max(izdano): prazna
// tablica nema izdanja, a najkasnije izdanje može imati samo jedna letva ili
// samo protok.
func TestZadnjeIzdanjeIstoKaoMax(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, ima, err := ZadnjeIzdanje(db); err != nil || ima {
		t.Fatalf("prazna baza: ima %v, greška %v", ima, err)
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
	provjeri := func(htio int64) {
		t.Helper()
		var ocekivano int64
		if err := db.QueryRow(`SELECT max(izdano) FROM izdane`).Scan(&ocekivano); err != nil {
			t.Fatal(err)
		}
		sat, ima, err := ZadnjeIzdanje(db)
		if err != nil || !ima || sat != ocekivano || sat != htio {
			t.Errorf("zadnje izdanje %d (ima %v, greška %v), max(izdano) %d, očekivano %d", sat, ima, err, ocekivano, htio)
		}
	}
	provjeri(1009)
	upisi("b-letva", "protok", 1200) // samo protok jedne letve
	provjeri(1200)
	upisi("a-letva", "vodostaj", 1300) // prva letva
	provjeri(1300)
}
