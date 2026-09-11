package ulaganje

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Popis uloženog je padao na skeniranju: min() i max() gube tip stupca, pa ih
// upravljač vraća kao tekst. Greška je bila progutana i odjeljak se jednostavno
// nije pojavljivao — nitko nije mogao ni doznati zašto.
func TestPospremivoCitaRazdobljeIzAgregata(t *testing.T) {
	baza := probnaBaza(t)
	defer baza.Close()

	p, err := Pospremivo(context.Background(), baza)
	if err != nil {
		t.Fatalf("popis uloženog je pao: %v", err)
	}
	if len(p) != 1 {
		t.Fatalf("na popisu %d letvi", len(p))
	}
	z := p[0]
	if z.Letva != "batina" || z.Broj != 2 {
		t.Errorf("popis kaže %+v", z)
	}
	if z.Od.IsZero() || z.Do.IsZero() {
		t.Errorf("razdoblje se nije pročitalo: %s .. %s", z.Od, z.Do)
	}
	if !z.Do.After(z.Od) {
		t.Errorf("„do“ nije poslije „od“: %s .. %s", z.Od, z.Do)
	}
	// Neuloženo se ne nudi na pospremanje.
	if z.Broj != 2 {
		t.Errorf("na pospremanje ponuđeno %d, a uloženo je 2", z.Broj)
	}
}

// Zaboravljanje ne briše ništa ako i jedna vrijednost nije u arhivi.
func TestZaboravljanjeStajeKadVrijednostNijeUArhivi(t *testing.T) {
	baza := probnaBaza(t)
	defer baza.Close()
	prazna := filepath.Join(t.TempDir(), "prazna.db")
	a, err := sql.Open("sqlite", prazna)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, vrijeme INTEGER,
		vrijednost REAL, izvor TEXT, korak TEXT, vrsta TEXT, tocnost REAL)`); err != nil {
		t.Fatal(err)
	}
	a.Close()

	if _, err := Zaboravi(context.Background(), baza, prazna, "st-1", nil); err == nil {
		t.Fatal("zaboravljanje je prošlo iako vrijednosti nema u arhivi")
	}
	var n int
	if err := baza.QueryRow(`SELECT count(*) FROM readings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("nakon pale provjere ostalo %d očitanja, a mora ostati svih 3", n)
	}
}

func probnaBaza(t *testing.T) *sql.DB {
	t.Helper()
	put := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`
		CREATE TABLE stations (id TEXT PRIMARY KEY, code TEXT, name TEXT);
		CREATE TABLE readings (id TEXT PRIMARY KEY, station_id TEXT, measured_at DATETIME,
			level_cm INTEGER, quality TEXT DEFAULT '', source TEXT DEFAULT '',
			observer TEXT DEFAULT '', note TEXT DEFAULT '', vrsta_biljeske TEXT DEFAULT '',
			izdanje TEXT DEFAULT '', updated_at DATETIME);
		CREATE TABLE record_versions (entity TEXT, entity_id TEXT);
		INSERT INTO stations VALUES ('st-1','batina','Batina');`); err != nil {
		t.Fatal(err)
	}
	kad := time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC)
	for i, r := range []struct {
		id      string
		sati    int
		izdanje string
	}{
		{"r1", 0, "2026-09-10"},
		{"r2", 6, "2026-09-10"},
		{"r3", 12, ""}, // još nije uloženo
	} {
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, izdanje)
			VALUES (?,?,?,?,?)`, r.id, "st-1", kad.Add(time.Duration(r.sati)*time.Hour),
			-150+i, r.izdanje); err != nil {
			t.Fatal(err)
		}
	}
	return baza
}
