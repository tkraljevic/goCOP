package peers

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Pregled brisanja broji tuđe i vlastite retke kanala u jednom prolazu, a
// brojke su iste kao iz dva zasebna upita (NOT uvjet i uvjet): uvjet koji
// nije ni istinit ni lažan (NULL) ne broji se ni u jedne, a svaki broj
// različit od nule je vlastito.
func TestPregledBrojiKaoDvaUpita(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "proba.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE stavke (id TEXT, kanal TEXT, vlastito INTEGER);
		INSERT INTO stavke VALUES ('a', 'k/1', 0), ('b', 'k/1', 1), ('c', 'k/1', 2), ('d', 'k/1', NULL),
			('e', 'k/1', 0), ('f', 'k/2', 1), ('g', 'k/2', 0)`); err != nil {
		t.Fatal(err)
	}
	args := []any{sql.Named("kanal", "k/1")}
	for _, k := range []korakBrisanja{
		{"stavke", "kanal = @kanal", "vlastito"},
		{"stavke", "kanal = @kanal", "vlastito = 1"},
		{"stavke", "kanal = 'nema'", "vlastito"},
	} {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		tudjih, vlastitih, err := k.izvedi(ctxRub, tx, args, false)
		if err != nil {
			t.Fatal(err)
		}
		var htioTudjih, htioVlastitih int
		if err := tx.QueryRow("SELECT count(*) FROM stavke WHERE ("+k.uKanalu+") AND NOT ("+k.ostaje+")", args...).Scan(&htioTudjih); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow("SELECT count(*) FROM stavke WHERE ("+k.uKanalu+") AND ("+k.ostaje+")", args...).Scan(&htioVlastitih); err != nil {
			t.Fatal(err)
		}
		if tudjih != htioTudjih || vlastitih != htioVlastitih {
			t.Errorf("%s / %s: tuđih %d, vlastitih %d; želim %d i %d", k.uKanalu, k.ostaje, tudjih, vlastitih, htioTudjih, htioVlastitih)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err := (korakBrisanja{"nema_tablice", "1", "1"}).izvedi(ctxRub, tx, nil, false); err == nil {
		t.Error("tablica koje nema: nema greške")
	}
	if _, _, err := (korakBrisanja{"stavke", "1", "'nije broj'"}).izvedi(ctxRub, tx, nil, false); err == nil {
		t.Error("uvjet koji nije logički izraz: nema greške")
	}
}
