package db

import (
	"testing"
	"time"
)

// Mreža iz 0.0.39 dobiva stupac s imenom protokola, i to „gocop”, pod kojim
// su do tada radile sve mreže. Ostatak retka ostaje kakav je bio. Upis kakav
// radi 0.0.39, koji novi stupac ne imenuje, i dalje prolazi, pa se
// nadograđena baza smije vratiti na 0.0.39.
func TestMigracijaPamtiProtokolMreze(t *testing.T) {
	database, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// network kakav ostavlja 0.0.39
	if _, err := database.Exec(`
		CREATE TABLE network (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			name TEXT NOT NULL,
			public_key TEXT NOT NULL,
			joined_at DATETIME NOT NULL
		);
		INSERT INTO network (id, name, public_key, joined_at) VALUES (1, 'Izmišljena mreža', 'izmisljeni-kljuc', '2026-10-18 07:00:00');`); err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(database); err != nil {
		t.Fatalf("migracija: %v", err)
	}
	if ima, err := columnExists(database, "network", "protokol"); err != nil || !ima {
		t.Fatalf("network nema stupac protokol nakon migracije (%v)", err)
	}
	var ime, kljuc, protokol string
	if err := database.QueryRow(`SELECT name, public_key, protokol FROM network WHERE id = 1`).Scan(&ime, &kljuc, &protokol); err != nil {
		t.Fatal(err)
	}
	if ime != "Izmišljena mreža" || kljuc != "izmisljeni-kljuc" || protokol != "gocop" {
		t.Errorf("postojeći redak: %q %q %q", ime, kljuc, protokol)
	}

	// upis kakav radi 0.0.39 (CreateNetwork i joinNetwork u v0.0.39-alfa) ne
	// imenuje novi stupac
	if _, err := database.Exec(`DELETE FROM network`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO network (id, name, public_key, joined_at) VALUES (1, ?, ?, ?)`,
		"Druga izmišljena mreža", "drugi-kljuc", time.Now().UTC()); err != nil {
		t.Fatalf("upis mreže kao 0.0.39: %v", err)
	}
	if err := database.QueryRow(`SELECT protokol FROM network WHERE id = 1`).Scan(&protokol); err != nil || protokol != "gocop" {
		t.Errorf("mreža koju je upisao 0.0.39: protokol %q (%v)", protokol, err)
	}
}
