package repository

import (
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
)

// Kartica dionice slaže zadužene odozgo: uprava sektora, pa područje, pa
// dionica, pa teren — kako uloge razvrstava katalog. Uz to, šifra dionice mora
// biti cijela stavka popisa: "B.34.1" je prije nalazio i rukovoditelje
// susjednih B.34.10 i B.34.12, pa je kartica pokazivala tuđe ljude.
func TestOsobljeDioniceIdeOdozgoIBezSusjednihDionica(t *testing.T) {
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'BP 34', 'VGI Baranja')`,
		`INSERT INTO sections (code, area_id, sector_id, description, parts, created_at, updated_at)
		 VALUES ('B.34.1', 34, 'B', 'r. Dunav', '[]', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,

		user("u1", "cuvar", "Vodočuvar Terenski"),
		user("u2", "ruk", "Rukovoditelj Dionice"),
		user("u3", "sektor", "Rukovoditelj Sektora"),
		user("u4", "susjed", "Rukovoditelj Susjedne"),

		duty("d1", "u1", "WATER_GUARD", "AREA", "B", "34", "''"),
		duty("d2", "u2", "SECTION_LEADER", "SECTION", "B", "34", `'B.34.1, B.34.7'`),
		duty("d3", "u3", "SECTOR_LEADER", "SECTOR", "B", "NULL", "''"),
		duty("d4", "u4", "SECTION_LEADER", "SECTION", "B", "34", `'B.34.10, B.34.12'`),
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("priprema (%s): %v", stmt, err)
		}
	}

	repo := NewSectionRepository(database, ledger.New(database, "test"))
	ljudi, err := repo.GetSectionPersonnel("B.34.1", 34, "B")
	if err != nil {
		t.Fatal(err)
	}

	var imena []string
	for _, o := range ljudi {
		if o.FullName == "Rukovoditelj Susjedne" {
			t.Error("kartica pokazuje rukovoditelja susjedne dionice (B.34.10)")
		}
		imena = append(imena, o.FullName)
	}
	ocekivano := []string{"Rukovoditelj Sektora", "Rukovoditelj Dionice", "Vodočuvar Terenski"}
	if len(imena) != len(ocekivano) {
		t.Fatalf("na kartici je %v, očekivano %v", imena, ocekivano)
	}
	for i := range ocekivano {
		if imena[i] != ocekivano[i] {
			t.Errorf("na %d. mjestu je %q, očekivano %q", i+1, imena[i], ocekivano[i])
		}
	}
	if ljudi[0].RoleGroup != "Razina 2" || ljudi[1].RoleGroup != "Razina 4" {
		t.Errorf("razine nisu ispisane: %q, %q", ljudi[0].RoleGroup, ljudi[1].RoleGroup)
	}
}

func user(id, username, name string) string {
	return `INSERT INTO users (id, username, password_hash, full_name, org_type, is_active, created_at, updated_at)
		VALUES ('` + id + `', '` + username + `', 'x', '` + name + `', 'HRVATSKE_VODE', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
}

func duty(id, userID, role, scope, sector, area, codes string) string {
	return `INSERT INTO duties (id, user_id, title, role, scope_type, sector_id, area_id, section_codes, is_primary, is_temporary, is_active, created_at)
		VALUES ('` + id + `', '` + userID + `', '` + role + `', '` + role + `', '` + scope + `', '` + sector + `', ` + area + `, ` + codes + `, 1, 0, 1, CURRENT_TIMESTAMP)`
}

// Unutar razine ide rukovoditelj pred zamjenikom, a ista osoba s dvije
// dužnosti na istoj razini stoji jednom: Mario Spajić je i zamjenik
// rukovoditelja sektora i voditelj centra, ali je jedan čovjek s jednim brojem.
func TestUnutarRazineTezinaUlogeIJedanZapisPoOsobi(t *testing.T) {
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'BP 34', 'VGI Baranja')`,
		`INSERT INTO sections (code, area_id, sector_id, description, parts, created_at, updated_at)
		 VALUES ('B.34.1', 34, 'B', 'r. Dunav', '[]', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		user("u1", "spajic", "Mario Spajić"),
		user("u2", "kovacevic", "Željko Kovačević"),
		duty("d1", "u1", "SECTOR_DEPUTY", "SECTOR", "B", "NULL", "''"),
		duty("d2", "u1", "COP_LEADER", "SECTOR", "B", "NULL", "''"),
		duty("d3", "u2", "SECTOR_LEADER", "SECTOR", "B", "NULL", "''"),
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("priprema (%s): %v", stmt, err)
		}
	}

	repo := NewSectionRepository(database, ledger.New(database, "test"))
	ljudi, err := repo.GetSectionPersonnel("B.34.1", 34, "B")
	if err != nil {
		t.Fatal(err)
	}
	if len(ljudi) != 2 {
		t.Fatalf("na kartici je %d zapisa, očekivano 2 (Spajić jednom)", len(ljudi))
	}
	if ljudi[0].FullName != "Željko Kovačević" {
		t.Errorf("prvi je %q, a rukovoditelj sektora mora biti pred zamjenikom", ljudi[0].FullName)
	}
	if !strings.Contains(ljudi[1].DutyTitle, "SECTOR_DEPUTY") || !strings.Contains(ljudi[1].DutyTitle, "COP_LEADER") {
		t.Errorf("obje dužnosti nisu spojene: %q", ljudi[1].DutyTitle)
	}
}
