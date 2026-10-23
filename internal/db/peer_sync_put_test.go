package db

import (
	"testing"
	"time"
)

// Stanje razmjene iz 0.0.41 dobiva stupce za put zadnje razmjene i
// brojače (F1b), prazne dok ih program ne upiše. Postojeći redak ostaje
// kakav je bio, a upisi kakve radi 0.0.41 i dalje prolaze, pa se
// nadograđena baza smije vratiti na 0.0.41.
func TestMigracijaPamtiPutRazmjene(t *testing.T) {
	database, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// peer_sync kakav ostavlja 0.0.41
	if _, err := database.Exec(`
		CREATE TABLE peer_sync (
			node_id TEXT PRIMARY KEY, their_frontier TEXT NOT NULL DEFAULT '{}', last_attempt DATETIME, last_ok DATETIME,
			last_error TEXT NOT NULL DEFAULT '', applied INTEGER NOT NULL DEFAULT 0, sent INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0, fails INTEGER NOT NULL DEFAULT 0,
			program TEXT NOT NULL DEFAULT '', gradnja TEXT NOT NULL DEFAULT '', program_poznat INTEGER NOT NULL DEFAULT 0,
			tailscale TEXT NOT NULL DEFAULT '',
			mogucnosti TEXT NOT NULL DEFAULT '', vrste TEXT NOT NULL DEFAULT '', ceka_nadogradnju INTEGER NOT NULL DEFAULT 0,
			mogucnosti_poznate INTEGER NOT NULL DEFAULT 0, adrese TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO peer_sync (node_id, program, program_poznat, fails, adrese)
		VALUES ('ured-vukovar', '0.0.41-alfa', 1, 2, '[{"a":"192.168.1.20:4710"}]');`); err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(database); err != nil {
		t.Fatalf("migracija: %v", err)
	}
	stupci := []string{"put", "put_privatna", "rtt_ms", "put_od", "put_razlog", "put_pokusaji", "put_uspjesi", "put_neuspjesi",
		"put_primljeno", "put_poslano", "zadnji_zvao", "priprema_pokusaji", "priprema_uspjesi", "priprema_neuspjesi"}
	for _, s := range stupci {
		if ima, err := columnExists(database, "peer_sync", s); err != nil || !ima {
			t.Errorf("peer_sync nema stupac %s nakon migracije (%v)", s, err)
		}
	}
	var put, razlog, zvao, program, adrese string
	var od *time.Time
	var privatna, rtt, pokusaji, primljeno, priprema, fails int
	if err := database.QueryRow(`SELECT put, put_privatna, rtt_ms, put_od, put_razlog, put_pokusaji, put_primljeno, zadnji_zvao,
		priprema_pokusaji, program, fails, adrese FROM peer_sync WHERE node_id = 'ured-vukovar'`).
		Scan(&put, &privatna, &rtt, &od, &razlog, &pokusaji, &primljeno, &zvao, &priprema, &program, &fails, &adrese); err != nil {
		t.Fatal(err)
	}
	if put != "" || privatna != 0 || rtt != 0 || od != nil || razlog != "" || pokusaji != 0 || primljeno != 0 || zvao != "" || priprema != 0 {
		t.Errorf("novi stupci postojećeg retka: %q %d %d %v %q %d %d %q %d", put, privatna, rtt, od, razlog, pokusaji, primljeno, zvao, priprema)
	}
	if program != "0.0.41-alfa" || fails != 2 || adrese == "" {
		t.Errorf("postojeći redak: %q %d %q", program, fails, adrese)
	}

	// upisi kakve radi 0.0.41 (status.go, tailscale.go i naucene_adrese.go
	// u v0.0.41-alfa) ne imenuju nove stupce
	now := time.Now().UTC()
	for ime, upit := range map[string]string{
		"stanje":    `INSERT INTO peer_sync (node_id, their_frontier, last_attempt, last_ok, last_error, applied, sent, duration_ms, fails, program, gradnja, program_poznat) VALUES ('laptop-baranja', '{}', ?, ?, '', 0, 0, 1, 0, '0.0.41-alfa', '', 1)`,
		"znanje":    `INSERT INTO peer_sync (node_id, mogucnosti, vrste, ceka_nadogradnju, mogucnosti_poznate) VALUES ('server', '', '', 0, 1)`,
		"tailscale": `INSERT INTO peer_sync (node_id, tailscale) VALUES ('ured-osijek', '[]')`,
		"adrese":    `INSERT INTO peer_sync (node_id, adrese) VALUES ('ured-dalj', '')`,
	} {
		args := []any{}
		if ime == "stanje" {
			args = []any{now, now}
		}
		if _, err := database.Exec(upit, args...); err != nil {
			t.Errorf("upis kao 0.0.41 (%s): %v", ime, err)
		}
	}
}
