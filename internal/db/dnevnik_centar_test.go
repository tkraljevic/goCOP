package db

import (
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Dnevnik COP-a je bilježnica jednog CENTRA, ne jednog područja. Sektorski COP
// pokriva pet područja i nijedno nije "njegovo", pa area_id ne smije biti
// obvezan. SQLite to ne zna s ALTER — tablica se radi iznova.
func TestDnevnikSeVezeNaCentar(t *testing.T) {
	baza, err := OpenDB(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := InitSchema(baza); err != nil {
		t.Fatal(err)
	}

	obvezan, err := stupacJeObvezan(baza, "journals", "area_id")
	if err != nil {
		t.Fatal(err)
	}
	if obvezan {
		t.Error("area_id je i dalje obvezan, pa sektorski dnevnik nije moguć")
	}

	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop)
		VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name)
		VALUES (34, 'B', 'BP 34', 'VGI')`); err != nil {
		t.Fatal(err)
	}
	sad := time.Now()

	// Sektorski dnevnik: centar je sektor, područja nema.
	if _, err := baza.Exec(`INSERT INTO journals (id, kind, centar_sektor, created_at, updated_at)
		VALUES ('dn-sektor', 'OBRANA', 'B', ?, ?)`, sad, sad); err != nil {
		t.Fatalf("sektorski dnevnik se ne da otvoriti: %v", err)
	}
	// Područni dnevnik: centar je područje unutar sektora.
	if _, err := baza.Exec(`INSERT INTO journals (id, kind, centar_sektor, centar_podrucje, created_at, updated_at)
		VALUES ('dn-podrucje', 'OBRANA', 'B', 34, ?, ?)`, sad, sad); err != nil {
		t.Fatalf("područni dnevnik se ne da otvoriti: %v", err)
	}
	// Dnevnik usluge i dalje ide po području.
	if _, err := baza.Exec(`INSERT INTO journals (id, kind, area_id, created_at, updated_at)
		VALUES ('dn-a02', 'ODRZAVANJE_A02', 34, ?, ?)`, sad, sad); err != nil {
		t.Fatalf("dnevnik usluge se ne da otvoriti: %v", err)
	}

	var n int
	if err := baza.QueryRow(`SELECT count(*) FROM journals`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("upisano %d dnevnika", n)
	}

	// Centar koji ne postoji se odbija: dnevnik bez centra nije ničiji.
	if _, err := baza.Exec(`INSERT INTO journals (id, kind, centar_sektor, created_at, updated_at)
		VALUES ('dn-x', 'OBRANA', 'NEMA', ?, ?)`, sad, sad); err == nil {
		t.Error("dnevnik s nepostojećim sektorom je prošao")
	}
}

// Selidba ne smije izgubiti ono što je već bilo u dnevnicima, ni pokvariti
// veze djece. Danas ih je nula, ali postupak mora biti isti — sutra neće biti.
func TestSelidbaCuvaZatecenoIVeze(t *testing.T) {
	put := filepath.Join(t.TempDir(), "g.db")
	baza, err := OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B','S','V','C')`)
	baza.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34,'B','BP','V')`)
	sad := time.Now()
	baza.Exec(`INSERT INTO journals (id, area_id, kind, title, created_at, updated_at)
		VALUES ('dn-1', 34, 'ODRZAVANJE_A02', 'Stari dnevnik', ?, ?)`, sad, sad)
	baza.Exec(`INSERT INTO journal_entries (id, journal_id, date, kind, text, created_at, updated_at)
		VALUES ('z-1', 'dn-1', '2026-09-11', 'NAPOMENA', 'zatečeni zapis', ?, ?)`, sad, sad)

	// Selidba se pokreće opet; mora podnijeti da je već obavljena.
	if err := preslozidnevnike(baza); err != nil {
		t.Fatalf("ponovljena selidba pukla: %v", err)
	}

	var naslov string
	if err := baza.QueryRow(`SELECT title FROM journals WHERE id='dn-1'`).Scan(&naslov); err != nil {
		t.Fatalf("zatečeni dnevnik je nestao: %v", err)
	}
	if naslov != "Stari dnevnik" {
		t.Errorf("naslov %q", naslov)
	}
	var zapisa int
	baza.QueryRow(`SELECT count(*) FROM journal_entries WHERE journal_id='dn-1'`).Scan(&zapisa)
	if zapisa != 1 {
		t.Errorf("zapisa uz dnevnik: %d", zapisa)
	}
	// Cjelovitost veza nakon zamjene tablice.
	var loše int
	baza.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&loše)
	if loše != 0 {
		t.Errorf("nakon selidbe %d pokvarenih veza", loše)
	}
}
