package arhiva

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Ponovna gradnja preko postojeće arhive gurala je točke korita pod tuđi broj.
// Upsert profila koji ode u UPDATE ne dira last_insert_rowid, pa je LastInsertId
// vraćao broj zadnjeg upisa na toj vezi — a to je bila promjena kote nule, iz
// druge tablice. Bez provjere stranih ključeva točke su odlazile u siročad
// (zatečeno ih je bilo 164), a s provjerom gradnja pada na FOREIGN KEY.
func TestProfilTockeNeIduPodTudiBroj(t *testing.T) {
	koren := t.TempDir()
	mapa := filepath.Join(koren, "dunav", "batina", "profil")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	csv := "# poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80.45\n" +
		"stacionaza_m;visina_m\n0,0;90,113\n6,74;87,593\n14,07;85,763\n"
	if err := os.WriteFile(filepath.Join(mapa, "batina_profil_2010-03-22.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}

	// Prva gradnja: profil se upisuje, sve je uredu i bez ispravka.
	if err := profili(db, koren, "", nil); err != nil {
		t.Fatalf("prva gradnja: %v", err)
	}

	// Druga gradnja, s upisom u drugu tablicu prije nje — točno onaj slijed
	// koji ima prava gradnja: promjene kote nule pa profili.
	if _, err := db.Exec(`INSERT INTO promjene_kote (letva, datum, pomak_cm) VALUES
		('baja','1943-01-01',-200), ('mohacs','1943-01-01',-200)`); err != nil {
		t.Fatal(err)
	}
	if err := profili(db, koren, "", nil); err != nil {
		t.Fatalf("druga gradnja preko postojećeg profila: %v", err)
	}

	var profila, tocaka, sirotista int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM profili),
		(SELECT count(*) FROM profil_tocke),
		(SELECT count(*) FROM profil_tocke WHERE profil NOT IN (SELECT id FROM profili))`).
		Scan(&profila, &tocaka, &sirotista); err != nil {
		t.Fatal(err)
	}
	if profila != 1 {
		t.Errorf("profila %d, očekivan jedan — druga gradnja ne smije stvoriti novi", profila)
	}
	if tocaka != 3 {
		t.Errorf("točaka %d, očekivane tri", tocaka)
	}
	if sirotista != 0 {
		t.Errorf("%d točaka pokazuje na profil kojeg nema", sirotista)
	}
}
