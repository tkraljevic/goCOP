package arhiva

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// Zadnji primljeni paket i koliko je letvi stiglo toga dana po zagrebačkom
// vremenu; bez evidencije i s nečitljivim datumom ništa.
func TestZadnjePrimljeno(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if kad, n := ZadnjePrimljeno(db, Zagreb); !kad.IsZero() || n != 0 {
		t.Errorf("bez tablice: %s %d", kad, n)
	}
	if _, err := db.Exec(shemaPrimljenih); err != nil {
		t.Fatal(err)
	}
	for letva, kad := range map[string]string{
		"a": "2026-11-22T23:30:00Z", // 23. 11. u 0:30 u Zagrebu
		"b": "2026-11-23T08:00:00Z",
		"c": "2026-11-22T20:00:00Z", // 22. 11.
	} {
		if _, err := db.Exec(`INSERT INTO primljena_izdanja (letva, izdanje, otisak, primljeno) VALUES (?, 1, 'x', ?)`, letva, kad); err != nil {
			t.Fatal(err)
		}
	}
	kad, n := ZadnjePrimljeno(db, Zagreb)
	if !kad.Equal(time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)) || n != 2 {
		t.Errorf("zadnje %s, letvi %d", kad, n)
	}
	if _, err := db.Exec(`UPDATE primljena_izdanja SET primljeno = 'nije-datum' WHERE letva = 'b'`); err != nil {
		t.Fatal(err)
	}
	if kad, n := ZadnjePrimljeno(db, Zagreb); !kad.IsZero() || n != 0 {
		t.Errorf("nečitljiv datum: %s %d", kad, n)
	}
}
