package db

import (
	"testing"
	"time"
)

// Stanje razmjene iz 0.0.39 dobiva stupac za adrese drugog računala koje je
// ovo računalo naučilo, prazan dok ne nauči nijednu. Postojeći redak ostaje
// kakav je bio. Upis kakav radi 0.0.39, koji novi stupac ne imenuje, i dalje
// prolazi, pa se nadograđena baza smije vratiti na 0.0.39.
func TestMigracijaPamtiNauceneAdrese(t *testing.T) {
	database, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// peer_sync kakav ostavlja 0.0.39
	if _, err := database.Exec(`
		CREATE TABLE peer_sync (
			node_id TEXT PRIMARY KEY, their_frontier TEXT NOT NULL DEFAULT '{}', last_attempt DATETIME, last_ok DATETIME,
			last_error TEXT NOT NULL DEFAULT '', applied INTEGER NOT NULL DEFAULT 0, sent INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0, fails INTEGER NOT NULL DEFAULT 0,
			program TEXT NOT NULL DEFAULT '', gradnja TEXT NOT NULL DEFAULT '', program_poznat INTEGER NOT NULL DEFAULT 0,
			tailscale TEXT NOT NULL DEFAULT '',
			mogucnosti TEXT NOT NULL DEFAULT '', vrste TEXT NOT NULL DEFAULT '', ceka_nadogradnju INTEGER NOT NULL DEFAULT 0,
			mogucnosti_poznate INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO peer_sync (node_id, program, program_poznat, tailscale, mogucnosti, mogucnosti_poznate)
		VALUES ('ured-vukovar', '0.0.39-alfa', 1, '["100.101.0.2:4710"]', '["razmjena/1"]', 1);`); err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(database); err != nil {
		t.Fatalf("migracija: %v", err)
	}
	if ima, err := columnExists(database, "peer_sync", "adrese"); err != nil || !ima {
		t.Fatalf("peer_sync nema stupac adrese nakon migracije (%v)", err)
	}
	var adrese, program, tailscale, mogucnosti string
	var poznate int
	if err := database.QueryRow(`SELECT adrese, program, tailscale, mogucnosti, mogucnosti_poznate FROM peer_sync WHERE node_id = 'ured-vukovar'`).
		Scan(&adrese, &program, &tailscale, &mogucnosti, &poznate); err != nil {
		t.Fatal(err)
	}
	if adrese != "" || program != "0.0.39-alfa" || tailscale != `["100.101.0.2:4710"]` || mogucnosti != `["razmjena/1"]` || poznate != 1 {
		t.Errorf("postojeći redak: %q %q %q %q %d", adrese, program, tailscale, mogucnosti, poznate)
	}

	// upisi kakve radi 0.0.39 (status.go i tailscale.go u v0.0.39-alfa) ne
	// imenuju novi stupac
	now := time.Now().UTC()
	if _, err := database.Exec(`
		INSERT INTO peer_sync (node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails, program, gradnja, program_poznat)
		VALUES (?, '{}', ?, ?, '', 0, 0, 1, 0, '0.0.39-alfa', '', 1)`, "laptop-baranja", now, now); err != nil {
		t.Errorf("upis stanja kao 0.0.39: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO peer_sync (node_id, mogucnosti, vrste, ceka_nadogradnju, mogucnosti_poznate) VALUES ('server', '', '', 0, 1)`); err != nil {
		t.Errorf("upis znanja kao 0.0.39: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO peer_sync (node_id, tailscale) VALUES ('ured-osijek', '[]')`); err != nil {
		t.Errorf("upis Tailscale adresa kao 0.0.39: %v", err)
	}
	if err := database.QueryRow(`SELECT adrese FROM peer_sync WHERE node_id = 'laptop-baranja'`).Scan(&adrese); err != nil || adrese != "" {
		t.Errorf("redak koji je upisao 0.0.39: adrese %q (%v)", adrese, err)
	}
}
