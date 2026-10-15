package db

import (
	"testing"
	"time"
)

// Stanje razmjene iz 0.0.38 dobiva stupce za ono što drugi čvor javlja da
// zna (razmjena/mogucnosti.go), prazne dok se ne čuje. Upis kakav radi
// 0.0.38, koji nove stupce ne imenuje, i dalje prolazi, pa se nadograđena
// baza smije vratiti na 0.0.38.
func TestMigracijaPamtiZnanjeDrugogCvora(t *testing.T) {
	database, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// peer_sync kakav ostavlja 0.0.38
	if _, err := database.Exec(`
		CREATE TABLE peer_sync (
			node_id TEXT PRIMARY KEY, their_frontier TEXT NOT NULL DEFAULT '{}', last_attempt DATETIME, last_ok DATETIME,
			last_error TEXT NOT NULL DEFAULT '', applied INTEGER NOT NULL DEFAULT 0, sent INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0, fails INTEGER NOT NULL DEFAULT 0,
			program TEXT NOT NULL DEFAULT '', gradnja TEXT NOT NULL DEFAULT '', program_poznat INTEGER NOT NULL DEFAULT 0,
			tailscale TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO peer_sync (node_id, program, program_poznat) VALUES ('ured-vukovar', '0.0.38-alfa', 1);`); err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(database); err != nil {
		t.Fatalf("migracija: %v", err)
	}
	for _, stupac := range []string{"mogucnosti", "vrste", "ceka_nadogradnju", "mogucnosti_poznate"} {
		if ima, err := columnExists(database, "peer_sync", stupac); err != nil || !ima {
			t.Errorf("peer_sync nema stupac %s nakon migracije (%v)", stupac, err)
		}
	}
	var mogucnosti, vrste, program string
	var ceka, poznate int
	if err := database.QueryRow(`SELECT mogucnosti, vrste, ceka_nadogradnju, mogucnosti_poznate, program FROM peer_sync WHERE node_id = 'ured-vukovar'`).
		Scan(&mogucnosti, &vrste, &ceka, &poznate, &program); err != nil {
		t.Fatal(err)
	}
	if mogucnosti != "" || vrste != "" || ceka != 0 || poznate != 0 || program != "0.0.38-alfa" {
		t.Errorf("postojeći redak: %q %q %d %d %q", mogucnosti, vrste, ceka, poznate, program)
	}

	// upis kakav radi 0.0.38 (status.go u c0162ca9) ne imenuje nove stupce
	now := time.Now().UTC()
	if _, err := database.Exec(`
		INSERT INTO peer_sync (node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails, program, gradnja, program_poznat)
		VALUES (?, '{}', ?, ?, '', 0, 0, 1, 0, '0.0.38-alfa', '', 1)`, "laptop-baranja", now, now); err != nil {
		t.Errorf("upis kao 0.0.38: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO peer_sync (node_id, tailscale) VALUES ('unraid', '[]')`); err != nil {
		t.Errorf("upis Tailscale adresa kao 0.0.38: %v", err)
	}
}
