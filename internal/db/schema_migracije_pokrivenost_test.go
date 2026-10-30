package db

import (
	"database/sql"
	"strings"
	"testing"
)

func izvedi(t *testing.T, database *sql.DB, naredbe ...string) {
	t.Helper()
	for _, n := range naredbe {
		if _, err := database.Exec(n); err != nil {
			t.Fatalf("%v\n%s", err, n)
		}
	}
}

func tekst(t *testing.T, database *sql.DB, upit string, args ...any) string {
	t.Helper()
	var s sql.NullString
	if err := database.QueryRow(upit, args...).Scan(&s); err != nil {
		t.Fatalf("upit %q: %v", upit, err)
	}
	return s.String
}

// Baza otprije zadnje prijave: stupac se popuni iz sesija, samo jednom, i
// samo korisnicima koji sesiju imaju.
func TestMigracijaZadnjePrijaveIzSesija(t *testing.T) {
	database := praznaBaza(t)
	izvedi(t, database,
		`INSERT INTO users (id, username, password_hash, full_name, org_type, created_at, updated_at)
			VALUES ('u1', 'ana.horvat', 'x', 'Ana Horvat', 'VODE', '2026-01-01', '2026-01-01'),
			       ('u2', 'ivo.kovac', 'x', 'Ivo Kovač', 'VODE', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES
			('s1', 'u1', '2026-02-01', '2026-01-05 08:00:00'),
			('s2', 'u1', '2026-02-01', '2026-01-20 09:30:00')`,
		`ALTER TABLE users DROP COLUMN last_login_at`,
	)
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if got := tekst(t, database, `SELECT last_login_at FROM users WHERE id = 'u1'`); !strings.HasPrefix(got, "2026-01-20") {
		t.Errorf("zadnja prijava %q, želim iz zadnje sesije", got)
	}
	if got := tekst(t, database, `SELECT last_login_at FROM users WHERE id = 'u2'`); got != "" {
		t.Errorf("korisnik bez sesije dobio prijavu %q", got)
	}
	// namjerno obrisan trag ne vraća se idućim pokretanjem
	izvedi(t, database, `UPDATE users SET last_login_at = NULL`)
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if got := tekst(t, database, `SELECT last_login_at FROM users WHERE id = 'u1'`); got != "" {
		t.Errorf("trag prijave vraćen pri drugom pokretanju: %q", got)
	}
}

// Baza otprije napomene vode: Dunav dobije napomenu o izvoru opisa, a
// druge vode ne.
func TestMigracijaNapomeneDunava(t *testing.T) {
	database := praznaBaza(t)
	izvedi(t, database,
		`INSERT INTO watercourses (code, official_name, name) VALUES ('rijeka-dunav', 'rijeka Dunav', 'Dunav'), ('rijeka-sava', 'rijeka Sava', 'Sava')`,
		`ALTER TABLE watercourses DROP COLUMN notes`,
	)
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if got := tekst(t, database, `SELECT notes FROM watercourses WHERE code = 'rijeka-dunav'`); !strings.Contains(got, "Wikipedij") {
		t.Errorf("napomena Dunava %q", got)
	}
	if got := tekst(t, database, `SELECT notes FROM watercourses WHERE code = 'rijeka-sava'`); got != "" {
		t.Errorf("napomena Save %q", got)
	}
}

// Baza otprije kanala: očitanje ide u područje svoje letve (preko dionice)
// ili objekta i u godinu mjerenja, dnevnik u svoje područje i godinu, a
// knjiga preuzima kanal s površine. Očitanje bez područja ostaje bez kanala.
func TestMigracijaKanalaStarihZapisa(t *testing.T) {
	database := praznaBaza(t)
	letva := StableID("station", "P-1").String()
	slobodna := StableID("station", "P-2").String()
	izvedi(t, database,
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO P', 'COP P')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (61, 'P', 'Područje 61', 'VGI 61'), (62, 'P', 'Područje 62', 'VGI 62')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.61.1', 61, 'P', 'probna', '2026-01-01', '2026-01-01')`,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('`+letva+`', 'P-1', 'Probna letva', '2026-01-01', '2026-01-01'),
			('`+slobodna+`', 'P-2', 'Letva bez dionice', '2026-01-01', '2026-01-01')`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('ss1', 'P.61.1', '`+letva+`', '2026-01-01')`,
		`INSERT INTO structures (id, code, name, kind, sector_id, area_id, created_at, updated_at)
			VALUES ('cs-1', 'P-CS', 'CS Probna', 'CRPNA_STANICA', 'P', 62, '2026-01-01', '2026-01-01')`,
		`INSERT INTO readings (id, station_id, measured_at, level_cm, created_at, updated_at) VALUES
			('r-letva', '`+letva+`', '2025-06-15 10:00:00', 120, '2025-06-15', '2025-06-15'),
			('r-slobodna', '`+slobodna+`', '2025-06-15 10:00:00', 80, '2025-06-15', '2025-06-15')`,
		`INSERT INTO readings (id, structure_id, measured_at, level_cm, created_at, updated_at) VALUES
			('r-objekt', 'cs-1', '2024-03-01 06:00:00', 300, '2024-03-01', '2024-03-01')`,
		`INSERT INTO journals (id, area_id, kind, year, created_at, updated_at) VALUES
			('j-godina', 61, 'A02', 2024, '2025-01-10', '2025-01-10'),
			('j-bez-godine', 62, 'A02', 0, '2023-05-05 12:00:00', '2023-05-05')`,
		`INSERT INTO journal_sheets (id, journal_id, date, created_at, updated_at) VALUES ('list-1', 'j-godina', '2024-05-01', '2024-05-01', '2024-05-01')`,
		`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at) VALUES
			('v1', 'readings', 'r-letva', 'cop', '{}', '2025-06-15'),
			('v2', 'readings', 'r-slobodna', 'cop', '{}', '2025-06-15'),
			('v3', 'journals', 'j-bez-godine', 'cop', '{}', '2023-05-05'),
			('v4', 'journal_sheets', 'list-1', 'cop', '{}', '2024-05-01'),
			('v5', 'stations', '`+letva+`', 'cop', '{}', '2026-01-01')`,
		`DROP INDEX idx_readings_channel`,
		`ALTER TABLE readings DROP COLUMN channel`,
	)
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	for upit, zelim := range map[string]string{
		`SELECT channel FROM readings WHERE id = 'r-letva'`:                                      "ocitanja/61/2025",
		`SELECT channel FROM readings WHERE id = 'r-objekt'`:                                     "ocitanja/62/2024",
		`SELECT channel FROM readings WHERE id = 'r-slobodna'`:                                   "",
		`SELECT channel FROM journals WHERE id = 'j-godina'`:                                     "dnevnici/61/2024",
		`SELECT channel FROM journals WHERE id = 'j-bez-godine'`:                                 "dnevnici/62/2023",
		`SELECT channel FROM record_versions WHERE version_id = 'v1'`:                            "ocitanja/61/2025",
		`SELECT channel FROM record_versions WHERE version_id = 'v2'`:                            "",
		`SELECT channel FROM record_versions WHERE version_id = 'v3'`:                            "dnevnici/62/2023",
		`SELECT channel FROM record_versions WHERE version_id = 'v4'`:                            "dnevnici/61/2024",
		`SELECT channel FROM record_versions WHERE version_id = 'v5'`:                            "",
		`SELECT COUNT(*) FROM pragma_index_list('readings') WHERE name = 'idx_readings_channel'`: "1",
	} {
		if got := tekst(t, database, upit); got != zelim {
			t.Errorf("%s: %q, želim %q", upit, got, zelim)
		}
	}
}

// Izvornici i fotografije otprije spremišta sadržaja: stara tablica s
// bajtovima skloni se pod starim imenom, netaknuta, a izvornici dobiju novu
// praznu tablicu s otiskom.
func TestMigracijaIzvornikaISlikaUSpremiste(t *testing.T) {
	database := praznaBaza(t)
	izvedi(t, database,
		`DROP TABLE prijave_izvornici`,
		`CREATE TABLE prijave_izvornici (prijava_id TEXT PRIMARY KEY, pdf BLOB, sazetak TEXT, updated_at DATETIME)`,
		`INSERT INTO prijave_izvornici VALUES ('pr-1', X'255044462D', 'abc', '2026-01-01')`,
		`CREATE TABLE prijave_slike (id TEXT PRIMARY KEY, prijava_id TEXT, slika BLOB)`,
		`INSERT INTO prijave_slike VALUES ('sl-1', 'pr-1', X'FFD8FF')`,
	)
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if got := tekst(t, database, `SELECT hex(pdf) FROM prijave_izvornici_stari WHERE prijava_id = 'pr-1'`); got != "255044462D" {
		t.Errorf("stari izvornik %q", got)
	}
	if got := tekst(t, database, `SELECT hex(slika) FROM prijave_slike_stari WHERE id = 'sl-1'`); got != "FFD8FF" {
		t.Errorf("stara slika %q", got)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM prijave_izvornici`); n != 0 {
		t.Errorf("nova tablica izvornika ima %d redaka", n)
	}
	for _, stupac := range []string{"otisak", "bajtova", "vrsta", "sazetak", "updated_at"} {
		if ima, err := columnExists(database, "prijave_izvornici", stupac); err != nil || !ima {
			t.Errorf("nova tablica izvornika bez stupca %s (%v)", stupac, err)
		}
	}
	if ima, _ := columnExists(database, "prijave_izvornici", "pdf"); ima {
		t.Error("nova tablica izvornika i dalje nosi PDF")
	}
	// iduće pokretanje ne dira ništa
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if n := count(t, database, `SELECT COUNT(*) FROM prijave_izvornici_stari`); n != 1 {
		t.Errorf("stari izvornici nakon drugog pokretanja: %d", n)
	}
}

// Stupac koji ne postoji nije obvezan; tablica koja ne postoji nema stupaca.
func TestStupacJeObvezan(t *testing.T) {
	database := praznaBaza(t)
	for _, s := range []struct {
		tablica, stupac string
		zelim           bool
	}{
		{"users", "username", true},
		{"users", "title", false},
		{"users", "nema", false},
		{"nema_tablice", "id", false},
		{"journals", "area_id", false}, // preslagivanje ga je oslobodilo
	} {
		got, err := stupacJeObvezan(database, s.tablica, s.stupac)
		if err != nil || got != s.zelim {
			t.Errorf("%s.%s: %v %v, želim %v", s.tablica, s.stupac, got, err, s.zelim)
		}
	}
	database.Close()
	if _, err := stupacJeObvezan(database, "users", "id"); err == nil {
		t.Error("zatvorena baza bez greške")
	}
	if _, err := columnExists(database, "users", "id"); err == nil {
		t.Error("columnExists nad zatvorenom bazom bez greške")
	}
	if err := InitSchema(database); err == nil {
		t.Error("shema nad zatvorenom bazom bez greške")
	}
}
