package prognoza

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// Pomoćnici testova računa (osvježavanje, prenesena i pričuvna prognoza,
// zamjena s druge obale): male baze u t.TempDir, bez mreže.

// praznaOcitanjaRacuna je operativna baza s postajama i očitanjima, bez ijednog
// očitanja.
func praznaOcitanjaRacuna(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ocitanja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE stations (id TEXT PRIMARY KEY, code TEXT);
		CREATE TABLE readings (station_id TEXT, measured_at DATETIME, level_cm INTEGER, flow_m3s REAL);`); err != nil {
		t.Fatal(err)
	}
	return db
}

// dodajOcitanje upisuje jedno očitanje letve; postaju doda kad je nema.
// Vodostaj ili protok smije biti nil.
func dodajOcitanje(t *testing.T, db *sql.DB, letva string, kad time.Time, cm, q any) {
	t.Helper()
	if _, err := db.Exec(`INSERT OR IGNORE INTO stations VALUES (?, ?)`, letva, letva); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm, flow_m3s) VALUES (?,?,?,?)`,
		letva, kad, cm, q); err != nil {
		t.Fatal(err)
	}
}

// dodajSatnaOcitanja upisuje vodostaj letve u svakom punom satu od od do do,
// uključivo; cm dobiva redni broj sata od od.
func dodajSatnaOcitanja(t *testing.T, db *sql.DB, letva string, od, do time.Time, cm func(h int) int) {
	t.Helper()
	if _, err := db.Exec(`INSERT OR IGNORE INTO stations VALUES (?, ?)`, letva, letva); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	h := 0
	for kad := od; !kad.After(do); kad = kad.Add(time.Hour) {
		if _, err := tx.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES (?,?,?)`, letva, kad, cm(h)); err != nil {
			t.Fatal(err)
		}
		h++
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// arhivaDnevnihOdDana je arhiva s dnevnim vodostajima: nizovi[letva][d] je
// vodostaj d dana nakon pocetka, u ponoć UTC.
func arhivaDnevnihOdDana(t *testing.T, pocetak time.Time, nizovi map[string]map[int]float64) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "arhiva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL, izvor TEXT, vrsta TEXT, tocnost REAL)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for letva, niz := range nizovi {
		for d, v := range niz {
			if _, err := tx.Exec(`INSERT INTO spoj VALUES (?, 'vodostaj', 'dnevni', ?, ?, 'his2000', 'srednjak', 0)`,
				letva, pocetak.AddDate(0, 0, d).Unix(), v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db
}

// arhivaKrivulja je arhiva s tablicama krivulja protoka, bez ijedne krivulje.
func arhivaKrivulja(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "krivulje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE hq_krivulje (id INTEGER, letva TEXT, vrijedi_od TEXT, vrijedi_do TEXT);
		CREATE TABLE hq_odsjecci (krivulja INTEGER, od_cm INTEGER, do_cm INTEGER, oblik TEXT,
			p1 REAL, p2 REAL, p3 REAL, p4 REAL);`); err != nil {
		t.Fatal(err)
	}
	return db
}

// dodajPravcuKrivulju upisuje krivulju Q = 100·H + 50 (H u metrima),
// umjerenu od 0 do 500 cm, koja vrijedi od vrijediOd (do je prazno: i dalje).
func dodajPravcuKrivulju(t *testing.T, db *sql.DB, id int, letva, vrijediOd, vrijediDo string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO hq_krivulje VALUES (?, ?, ?, ?)`, id, letva, vrijediOd, vrijediDo); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO hq_odsjecci VALUES (?, 0, 500, 'polinom', 0, 100, 50, 0)`, id); err != nil {
		t.Fatal(err)
	}
}
