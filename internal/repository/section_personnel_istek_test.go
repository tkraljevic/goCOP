package repository

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
)

// Dužnost kojoj je istekao rok ne daje osobu dionice (ni primatelja akta),
// isto kao što je ne vide ovlasti (GetDutiesForUser); dužnost s rokom u
// budućnosti i stalna dužnost daju
func TestOsobljeDioniceBezIsteklihDuznosti(t *testing.T) {
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, parts, created_at, updated_at)
		 VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '[]', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		user("u1", "pperic", "Pero Perić"),
		user("u2", "iivic", "Ivo Ivić"),
		user("u3", "mmaric", "Marko Marić"),
		duty("d1", "u1", "SECTION_LEADER", "SECTION", "P", "1", `'P.1.1'`),
		duty("d2", "u2", "SECTION_DEPUTY", "SECTION", "P", "1", `'P.1.1'`),
		duty("d3", "u3", "SECTION_DEPUTY", "SECTION", "P", "1", `'P.1.1'`),
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("priprema (%s): %v", stmt, err)
		}
	}
	// rok se upisuje u UTC-u, kao kad ga upisuje program
	sad := time.Now().UTC()
	if _, err := database.Exec(`UPDATE duties SET is_temporary = 1, expires_at = ? WHERE id = 'd2'`, sad.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE duties SET is_temporary = 1, expires_at = ? WHERE id = 'd3'`, sad.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	repo := NewSectionRepository(database, ledger.New(database, "test"))
	ljudi, err := repo.GetSectionPersonnel("P.1.1", 1, "P")
	if err != nil {
		t.Fatal(err)
	}
	var imena []string
	for _, o := range ljudi {
		imena = append(imena, o.FullName)
	}
	if strings.Join(imena, ", ") != "Pero Perić, Marko Marić" {
		t.Errorf("osoblje dionice: %v, očekivano Pero Perić i Marko Marić (Ivi Iviću je dužnost istekla)", imena)
	}
}
