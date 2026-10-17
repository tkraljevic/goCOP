package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// Zaključava da glavna baza piše tako da potvrđen upis preživi nestanak
// struje: svaka veza, ne samo prva, ima synchronous FULL (2). Uz NORMAL
// ovjera akta mogla je nestati, a sljedeća ovjera dati isti broj drugom aktu.
func TestGlavnaBazaPiseTrajno(t *testing.T) {
	baza, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	ctx := context.Background()
	for i := range 2 {
		veza, err := baza.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer veza.Close()
		var synchronous int
		if err := veza.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&synchronous); err != nil {
			t.Fatal(err)
		}
		if synchronous != 2 {
			t.Errorf("%d. veza: synchronous %d, treba 2 (FULL)", i+1, synchronous)
		}
	}
}

// Zaključava da se shema upisuje u jednoj transakciji: naredba koja padne
// vraća i sve prije nje, a greška kaže koja je naredba pala. Zatvorena baza i
// baza u koju se ne smije pisati javljaju grešku prije prve naredbe.
func TestShemaUJednojTransakciji(t *testing.T) {
	put := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()

	err = izvediShemu(baza, []string{`CREATE TABLE prva (x)`, `CREATE TABL druga (x)`})
	if err == nil || !strings.Contains(err.Error(), "CREATE TABL druga") {
		t.Fatalf("greška %v, treba reći koja je naredba pala", err)
	}
	var tablica int
	if err := baza.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'prva'`).Scan(&tablica); err != nil {
		t.Fatal(err)
	}
	if tablica != 0 {
		t.Error("tablica prva ostala je iako je shema pala")
	}

	// Datoteku otvorenu samo za čitanje SQLite u WAL-u pusti do prve naredbe,
	// a query_only odbije već BEGIN IMMEDIATE.
	citac, err := sql.Open("sqlite", put+"?_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer citac.Close()
	if err := izvediShemu(citac, []string{`CREATE TABLE prva (x)`}); err == nil {
		t.Error("shema je upisana u bazu u koju se ne smije pisati")
	}

	zatvorena, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := zatvorena.Close(); err != nil {
		t.Fatal(err)
	}
	if err := izvediShemu(zatvorena, []string{`CREATE TABLE prva (x)`}); err == nil {
		t.Error("shema je prošla bez greške na zatvorenoj bazi")
	}
}
