package arhiva

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Arhiva se ne gradi iznova zbog jednog stupca — 471 MB se ne prepisuje bez
// potrebe. Zatečena arhiva mora dobiti napomenu dopunom, a ne obnovom.
func TestDopunaShemeDodajeNapomenuBezObnove(t *testing.T) {
	put := filepath.Join(t.TempDir(), "stara.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// arhiva kakva je bila prije stupca
	if _, err := db.Exec(`CREATE TABLE nizovi (
		id INTEGER PRIMARY KEY, zona TEXT NOT NULL DEFAULT '', sliv TEXT NOT NULL,
		letva TEXT NOT NULL, izvor TEXT NOT NULL, velicina TEXT NOT NULL,
		vrsta TEXT NOT NULL, po_danu INTEGER NOT NULL DEFAULT 0,
		od TEXT NOT NULL DEFAULT '', do_ TEXT NOT NULL DEFAULT '',
		zapisa INTEGER NOT NULL DEFAULT 0, otisak TEXT NOT NULL DEFAULT '',
		datoteke TEXT NOT NULL DEFAULT '', osvjezeno TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('dunav', 'batina', 'his2000', 'vodostaj', 'satni', 222598)`); err != nil {
		t.Fatal(err)
	}

	if err := dopuniShemu(db); err != nil {
		t.Fatalf("dopuna: %v", err)
	}

	// stupac postoji, a zapis je ostao
	var napomena string
	var zapisa int
	if err := db.QueryRow(`SELECT napomena, zapisa FROM nizovi WHERE letva='batina'`).
		Scan(&napomena, &zapisa); err != nil {
		t.Fatalf("čitanje nakon dopune: %v", err)
	}
	if napomena != "" {
		t.Errorf("zatečeni niz je dobio napomenu %q", napomena)
	}
	if zapisa != 222598 {
		t.Errorf("dopuna je promijenila zapis: zapisa %d", zapisa)
	}

	// dopuna se smije pokrenuti opet, bez greške
	if err := dopuniShemu(db); err != nil {
		t.Fatalf("ponovljena dopuna: %v", err)
	}
}
