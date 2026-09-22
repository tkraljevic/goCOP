package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Postaja koja dobije determinirani identitet mora povesti sve svoje sa
// sobom. Jednom nije: letvi su ostala 245 očitanja vezana uz identitet kojeg
// više nema, pa se digla prazna, a niz je visio u zraku. Test drži da se to
// ne ponovi ni za jednu tablicu koja na postaju pokazuje.
func TestPrekodiranjePostajeVodiSvojeSaSobom(t *testing.T) {
	baza, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatalf("baza se ne može otvoriti: %v", err)
	}
	defer baza.Close()
	if err := InitSchema(baza); err != nil {
		t.Fatalf("shema: %v", err)
	}

	// postaja upisana starim, nasumičnim identitetom — takve zatječe migracija
	const star = "01a0c822-3dcc-707d-ba40-ac2b338fb178"
	const sifra = "proba-letva"
	const kad = "2026-09-22 11:00:00 +0000 UTC"
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES (?,?,?,?,?)`,
		star, sifra, "Proba", kad, kad); err != nil {
		t.Fatalf("upis postaje: %v", err)
	}
	if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, created_at, updated_at)
		VALUES ('r1', ?, ?, -72, 'UVOZ', ?, ?)`, star, kad, kad, kad); err != nil {
		t.Fatalf("upis očitanja: %v", err)
	}

	if err := rekeySeedIdentities(baza); err != nil {
		t.Fatalf("prekodiranje: %v", err)
	}

	nov := StableID("station", sifra).String()
	if nov == star {
		t.Fatal("test ne mjeri ništa: identitet se nije promijenio")
	}
	var id string
	if err := baza.QueryRow(`SELECT id FROM stations WHERE code = ?`, sifra).Scan(&id); err != nil {
		t.Fatalf("postaja poslije prekodiranja: %v", err)
	}
	if id != nov {
		t.Fatalf("postaja ima %s, očekivano %s", id, nov)
	}

	for _, p := range []struct{ tablica, opis string }{
		{"readings", "očitanje"},
	} {
		var uz, sirotana int
		if err := baza.QueryRow(`SELECT COUNT(*) FROM `+p.tablica+` WHERE station_id = ?`, nov).Scan(&uz); err != nil {
			t.Fatalf("%s: %v", p.tablica, err)
		}
		if err := baza.QueryRow(`SELECT COUNT(*) FROM ` + p.tablica + `
			WHERE station_id IS NOT NULL AND station_id <> ''
			AND station_id NOT IN (SELECT id FROM stations)`).Scan(&sirotana); err != nil {
			t.Fatalf("%s, sirotani: %v", p.tablica, err)
		}
		if uz != 1 {
			t.Errorf("%s nije pošlo za postajom: uz novi identitet ih je %d", p.opis, uz)
		}
		if sirotana != 0 {
			t.Errorf("%s: %d bez postaje nakon prekodiranja", p.tablica, sirotana)
		}
	}
}

// Popis tablica u migraciji mora pratiti shemu: nova tablica sa station_id
// koju nitko ne dopiše u prekodiranje tiho izgubi vezu s postajom, a to se
// vidi tek kad zatreba.
func TestSvakaTablicaSaStationIDJeUPrekodiranju(t *testing.T) {
	baza, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatalf("baza se ne može otvoriti: %v", err)
	}
	defer baza.Close()
	if err := InitSchema(baza); err != nil {
		t.Fatalf("shema: %v", err)
	}

	obuhvacene := map[string]bool{
		"section_stations": true, "readings": true,
		"structures": true, "defense_episodes": true, "akti": true,
	}
	rows, err := baza.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var t_ string
		if err := rows.Scan(&t_); err != nil {
			t.Fatal(err)
		}
		if !imaStupac(t, baza, t_, "station_id") || obuhvacene[t_] {
			continue
		}
		t.Errorf("tablica %s ima station_id, a nije u rekeySeedIdentities — postaja bi je pri prekodiranju ostavila", t_)
	}
}

func imaStupac(t *testing.T, baza *sql.DB, tablica, stupac string) bool {
	t.Helper()
	rows, err := baza.Query(`SELECT name FROM pragma_table_info(?)`, tablica)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ime string
		if err := rows.Scan(&ime); err != nil {
			t.Fatal(err)
		}
		if ime == stupac {
			return true
		}
	}
	return false
}
