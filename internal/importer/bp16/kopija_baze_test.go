package bp16

import (
	"database/sql"
	"path/filepath"
	"testing"

	"gocop/internal/db"
)

// Testovi grešaka trebaju svaki svoju bazu, a dizanje sheme je sporo (pod
// -race i preko sekunde). Zato se baza složi jednom, a svaki slučaj dobije
// njezinu presliku.

// kopijaBaze daje novu bazu s istim sadržajem kao predložak
func kopijaBaze(t *testing.T, predlozak *sql.DB) *sql.DB {
	t.Helper()
	put := filepath.Join(t.TempDir(), "kopija.db")
	if _, err := predlozak.Exec(`VACUUM INTO ?`, put); err != nil {
		t.Fatal(err)
	}
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	return baza
}

// zatvorenaBaza je baza koja odbija svaki upit, bez sheme jer do nje ni ne
// dođe
func zatvorenaBaza(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zatvorena.db"))
	if err != nil {
		t.Fatal(err)
	}
	baza.Close()
	return baza
}
