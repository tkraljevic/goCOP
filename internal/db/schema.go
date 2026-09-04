package db

import (
	"database/sql"
	"fmt"

	"gocop/internal/ledger"
)

// InitSchema kreira tablice prilagođene punoj granularnosti sustava obrane od poplava
func InitSchema(database *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sectors (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			vgo_name TEXT NOT NULL,
			center_cop TEXT NOT NULL,
			address TEXT,
			phone TEXT,
			email TEXT
		);`,

		`CREATE TABLE IF NOT EXISTS areas (
			id INTEGER PRIMARY KEY,
			sector_id TEXT NOT NULL REFERENCES sectors(id),
			name TEXT NOT NULL,
			vgi_name TEXT NOT NULL,
			subcenter TEXT,
			contractor_name TEXT
		);`,

		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			full_name TEXT NOT NULL,
			title TEXT,
			is_global_admin INTEGER NOT NULL DEFAULT 0,
			must_change_password INTEGER NOT NULL DEFAULT 1,
			org_type TEXT NOT NULL,
			org_name TEXT,
			phone TEXT,
			mobile_phone TEXT,
			short_phone TEXT,
			email TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			last_login_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS duties (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			role TEXT NOT NULL,
			scope_type TEXT NOT NULL,
			sector_id TEXT REFERENCES sectors(id),
			area_id INTEGER REFERENCES areas(id),
			section_codes TEXT,
			is_primary INTEGER NOT NULL DEFAULT 1,
			is_temporary INTEGER NOT NULL DEFAULT 0,
			reason TEXT,
			assigned_by TEXT REFERENCES users(id),
			created_at DATETIME NOT NULL,
			expires_at DATETIME,
			is_active INTEGER NOT NULL DEFAULT 1
		);`,

		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			viewing_as TEXT REFERENCES users(id) ON DELETE SET NULL,
			ip_address TEXT,
			user_agent TEXT,
			expires_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS sync_log (
			id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL,
			table_name TEXT NOT NULL,
			record_id TEXT NOT NULL,
			action TEXT NOT NULL,
			data_json TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,

		// description je izvorni opis dionice (voda, obala, obuhvat, stacionaža);
		// strukturirani dio živi u watercourse_code, bank, rkm_from, rkm_to.
		`CREATE TABLE IF NOT EXISTS sections (
			code TEXT PRIMARY KEY,
			area_id INTEGER NOT NULL REFERENCES areas(id),
			sector_id TEXT NOT NULL REFERENCES sectors(id),
			description TEXT NOT NULL,
			watercourse_code TEXT NOT NULL DEFAULT '',
			bank TEXT NOT NULL DEFAULT '',
			rkm_from REAL,
			rkm_to REAL,
			protected_area TEXT,
			embankments TEXT,
			structures TEXT,
			gauges TEXT,
			notes TEXT,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS counties (
			id INTEGER PRIMARY KEY,
			code TEXT UNIQUE,
			name TEXT NOT NULL,
			seat TEXT NOT NULL,
			prefect TEXT,
			area_sqkm INTEGER,
			population INTEGER,
			email TEXT,
			phone TEXT
		);`,

		`CREATE TABLE IF NOT EXISTS municipalities (
			id INTEGER PRIMARY KEY,
			county_id INTEGER NOT NULL REFERENCES counties(id),
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			head_title TEXT,
			head_name TEXT,
			postal_code TEXT,
			area_sqkm REAL,
			population INTEGER
		);`,

		`CREATE TABLE IF NOT EXISTS settlements (
			id INTEGER PRIMARY KEY,
			municipality_id INTEGER NOT NULL REFERENCES municipalities(id),
			county_id INTEGER NOT NULL REFERENCES counties(id),
			name TEXT NOT NULL,
			postal_code TEXT,
			population INTEGER
		);`,

		`CREATE TABLE IF NOT EXISTS section_territories (
			id TEXT PRIMARY KEY,
			section_code TEXT NOT NULL REFERENCES sections(code) ON DELETE CASCADE,
			county_id INTEGER NOT NULL REFERENCES counties(id),
			municipality_id INTEGER NOT NULL REFERENCES municipalities(id),
			settlement_id INTEGER REFERENCES settlements(id),
			created_at DATETIME NOT NULL
		);`,

		// Vodomjerne postaje — globalni registar. Ista postaja mjerodavna je za
		// više dionica, pa veza ide preko section_stations.
		// Pragovi su u centimetrima na vodomjeru i popunjeni su samo kad su takvi
		// i u dokumentaciji; zadani kao apsolutna kota ili kao uputa iz pravilnika
		// ostaju u pripadajućem *_raw stupcu i ne ulaze u izračun faze obrane.
		`CREATE TABLE IF NOT EXISTS stations (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			-- voda na kojoj vodomjer stoji; prazno kad dokumentacija ne tvrdi
			watercourse TEXT NOT NULL DEFAULT '',
			watercourse_source TEXT NOT NULL DEFAULT '',
			water_area TEXT NOT NULL DEFAULT '',
			stationing TEXT NOT NULL DEFAULT '',

			-- Kota nule vodomjera u dva visinska sustava. zero_datum je preuzeta
			-- iz dokumentacije dionica i vodi se u starom sustavu; zero_datum_new
			-- upisuje se ručno i ne izvodi se preračunom iz stare.
			zero_datum REAL,
			zero_datum_system TEXT NOT NULL DEFAULT 'TRST',
			zero_datum_new REAL,
			zero_datum_new_system TEXT NOT NULL DEFAULT 'HVRS71',

			prep_cm INTEGER,
			prep_raw TEXT NOT NULL DEFAULT '',
			regular_cm INTEGER,
			regular_raw TEXT NOT NULL DEFAULT '',
			emergency_cm INTEGER,
			emergency_raw TEXT NOT NULL DEFAULT '',
			state_cm INTEGER,
			state_raw TEXT NOT NULL DEFAULT '',
			record_cm INTEGER,
			record_raw TEXT NOT NULL DEFAULT '',

			notes TEXT NOT NULL DEFAULT '',
			source_name TEXT NOT NULL DEFAULT '',
			needs_review INTEGER NOT NULL DEFAULT 0,
			review_note TEXT NOT NULL DEFAULT '',
			latitude REAL,
			longitude REAL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,

		// Registar vodnih tijela. Kostur je Odluka o popisu voda I. reda
		// (NN 79/2010), dopunjen vodama koje na tom popisu nisu.
		`CREATE TABLE IF NOT EXISTS watercourses (
			code TEXT PRIMARY KEY,
			official_name TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			subcategory TEXT NOT NULL DEFAULT '',
			wiki_slug TEXT NOT NULL DEFAULT '',
			origin TEXT NOT NULL DEFAULT '',
			length_km REAL,
			basin_km2 REAL,
			avg_flow_m3s REAL,
			source TEXT NOT NULL DEFAULT '',
			mouth TEXT NOT NULL DEFAULT '',
			flows_into TEXT NOT NULL DEFAULT ''
		);`,

		// Mjerodavni vodomjeri pojedine dionice
		`CREATE TABLE IF NOT EXISTS section_stations (
			id TEXT PRIMARY KEY,
			section_code TEXT NOT NULL REFERENCES sections(code) ON DELETE CASCADE,
			station_id TEXT NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
			created_at DATETIME NOT NULL,
			UNIQUE(section_code, station_id)
		);`,

		// Poznati čvorovi. Identitet čvora je njegov javni ključ; adresa je
		// promjenjiva. Popis se sinkronizira kao i sve drugo.
		`CREATE TABLE IF NOT EXISTS peers (
			node_id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			public_key TEXT NOT NULL,
			addresses TEXT NOT NULL DEFAULT '[]',
			is_bootstrap INTEGER NOT NULL DEFAULT 0,
			last_seen DATETIME,
			last_sync DATETIME,
			last_sync_note TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL
		);`,

		// Mreža kojoj ovaj čvor pripada: naziv i javni ključ mreže. Privatni
		// ključ mreže NIJE u bazi — leži u datoteci network-key samo na
		// čvorovima čiji vlasnici smiju primati članove.
		`CREATE TABLE IF NOT EXISTS network (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			name TEXT NOT NULL,
			public_key TEXT NOT NULL,
			joined_at DATETIME NOT NULL
		);`,

		// Članstva: potpis mrežnog ključa nad javnim ključem čvora. Povjerenje
		// na vratima razmjene = važeće članstvo NAŠE mreže, ne "postoji u
		// popisu". Sinkroniziraju se; opoziv je arhiviranje.
		`CREATE TABLE IF NOT EXISTS memberships (
			node_id TEXT PRIMARY KEY,
			public_key TEXT NOT NULL,
			network TEXT NOT NULL,
			issued_by TEXT NOT NULL,
			issued_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			signature TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,

		// Hidrotehnički objekti: crpne stanice, ustave, sifoni... Zaseban zapis s
		// vlastitim podacima, na koji se vežu očitanja i dnevnik rada.
		`CREATE TABLE IF NOT EXISTS structures (
			id TEXT PRIMARY KEY,
			code TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			sector_id TEXT NOT NULL REFERENCES sectors(id),
			area_id INTEGER NOT NULL REFERENCES areas(id),
			watercourse_code TEXT NOT NULL DEFAULT '',
			station_id TEXT NOT NULL DEFAULT '',
			zero_datum REAL,
			zero_datum_system TEXT NOT NULL DEFAULT '',
			capacity_text TEXT NOT NULL DEFAULT '',
			start_cm INTEGER,
			start_text TEXT NOT NULL DEFAULT '',
			stop_cm INTEGER,
			stop_text TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			origin TEXT NOT NULL DEFAULT '',
			latitude REAL,
			longitude REAL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS section_structures (
			id TEXT PRIMARY KEY,
			section_code TEXT NOT NULL REFERENCES sections(code) ON DELETE CASCADE,
			structure_id TEXT NOT NULL REFERENCES structures(id) ON DELETE CASCADE,
			created_at DATETIME NOT NULL,
			UNIQUE(section_code, structure_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_structures_area ON structures(area_id);`,
		`CREATE INDEX IF NOT EXISTS idx_section_structures_section ON section_structures(section_code);`,

		// Očitanja vodostaja: jedan tok za letve postaja i objekata.
		// Letva je ili postaja (station_id) ili objekt (structure_id).
		`CREATE TABLE IF NOT EXISTS readings (
			id TEXT PRIMARY KEY,
			station_id TEXT NOT NULL DEFAULT '',
			structure_id TEXT NOT NULL DEFAULT '',
			measured_at DATETIME NOT NULL,
			level_cm INTEGER,
			level2_cm INTEGER,
			source TEXT NOT NULL DEFAULT 'RUČNO',
			origin TEXT NOT NULL DEFAULT '',
			source_ref TEXT NOT NULL DEFAULT '',
			observer TEXT NOT NULL DEFAULT '',
			user_id TEXT NOT NULL DEFAULT '',
			structure_state TEXT NOT NULL DEFAULT '',
			gate TEXT NOT NULL DEFAULT '',
			ag_hours_1 INTEGER,
			ag_hours_2 INTEGER,
			ag_hours_3 INTEGER,
			note TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_readings_station_time ON readings(station_id, measured_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_readings_structure_time ON readings(structure_id, measured_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_readings_source_ref ON readings(source_ref);`,

		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);`,
		`CREATE INDEX IF NOT EXISTS idx_duties_user ON duties(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_duties_sector ON duties(sector_id);`,
		`CREATE INDEX IF NOT EXISTS idx_duties_area ON duties(area_id);`,
		`CREATE INDEX IF NOT EXISTS idx_duties_active ON duties(is_active);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sync_log_created ON sync_log(created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_sections_area ON sections(area_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sections_sector ON sections(sector_id);`,
		`CREATE INDEX IF NOT EXISTS idx_municipalities_county ON municipalities(county_id);`,
		`CREATE INDEX IF NOT EXISTS idx_settlements_municipality ON settlements(municipality_id);`,
		`CREATE INDEX IF NOT EXISTS idx_settlements_county ON settlements(county_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sec_terr_section ON section_territories(section_code);`,
		`CREATE INDEX IF NOT EXISTS idx_sec_terr_muni ON section_territories(municipality_id);`,
		`CREATE INDEX IF NOT EXISTS idx_stations_code ON stations(code);`,
		`CREATE INDEX IF NOT EXISTS idx_stations_watercourse ON stations(watercourse);`,
		`CREATE INDEX IF NOT EXISTS idx_sec_stat_section ON section_stations(section_code);`,
		`CREATE INDEX IF NOT EXISTS idx_sec_stat_station ON section_stations(station_id);`,
		`CREATE INDEX IF NOT EXISTS idx_watercourses_name ON watercourses(name);`,
	}

	for _, query := range queries {
		if _, err := database.Exec(query); err != nil {
			return fmt.Errorf("greška pri shemi (%s): %w", query, err)
		}
	}

	// Knjiga verzija — svaki upis u sinkroniziranu tablicu ostavlja verziju
	if _, err := database.Exec(ledger.Schema); err != nil {
		return fmt.Errorf("greška pri shemi knjige verzija: %w", err)
	}

	return migrateSchema(database)
}

// renamedColumns su stupci preimenovani nakon što su baze već postojale
var renamedColumns = []struct {
	table, from, to string
}{
	// opis dionice nije vodotok — vodotok je watercourse_code
	{"sections", "watercourse", "description"},
}

// migrateSchema dodaje stupce koji su uvedeni nakon što je baza već stvorena.
//
// CREATE TABLE IF NOT EXISTS ne mijenja postojeću tablicu, a baze žive na
// terenskim laptopima koji se ne stvaraju iznova — bez ovoga bi stara baza
// pucala na prvom upitu koji traži novi stupac.
func migrateSchema(database *sql.DB) error {
	for _, r := range renamedColumns {
		oldExists, err := columnExists(database, r.table, r.from)
		if err != nil {
			return err
		}
		newExists, err := columnExists(database, r.table, r.to)
		if err != nil {
			return err
		}
		if !oldExists || newExists {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", r.table, r.from, r.to)
		if _, err := database.Exec(stmt); err != nil {
			return fmt.Errorf("greška pri preimenovanju stupca %s.%s: %w", r.table, r.from, err)
		}
	}

	added := []struct {
		table, column, definition string
	}{
		{"stations", "watercourse_source", "TEXT NOT NULL DEFAULT ''"},
		{"stations", "watercourse_code", "TEXT NOT NULL DEFAULT ''"},
		{"sections", "watercourse_code", "TEXT NOT NULL DEFAULT ''"},
		{"sections", "bank", "TEXT NOT NULL DEFAULT ''"},
		{"sections", "rkm_from", "REAL"},
		{"sections", "rkm_to", "REAL"},
		{"watercourses", "origin", "TEXT NOT NULL DEFAULT ''"},
		{"users", "last_login_at", "DATETIME"},
		{"sessions", "viewing_as", "TEXT"},
	}

	// Vrijednosti koje su promijenile ime nakon što su upisane
	dataFixups := []string{
		`UPDATE stations SET zero_datum_system = 'TRST' WHERE zero_datum_system = 'STARI'`,
		`UPDATE stations SET zero_datum_new_system = 'HVRS71' WHERE zero_datum_new_system = 'NOVI'`,
	}

	justAdded := map[string]bool{}
	for _, c := range added {
		exists, err := columnExists(database, c.table, c.column)
		if err != nil {
			return err
		}
		if exists {
			continue
		}

		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.column, c.definition)
		if _, err := database.Exec(stmt); err != nil {
			return fmt.Errorf("greška pri dodavanju stupca %s.%s: %w", c.table, c.column, err)
		}
		justAdded[c.table+"."+c.column] = true
	}

	// Prijave otprije stupca: sesije su jedini trag, i to lokalni, pa svaki
	// čvor popunjava ono što je sam vidio. Samo jednom, u trenutku kad stupac
	// nastane — inače bi svako pokretanje vratilo trag koji je netko namjerno
	// obrisao, a stare sesije žive dulje od svoje valjanosti.
	if justAdded["users.last_login_at"] {
		if _, err := database.Exec(`
			UPDATE users SET last_login_at = (
				SELECT MAX(s.created_at) FROM sessions s WHERE s.user_id = users.id
			) WHERE EXISTS (SELECT 1 FROM sessions s WHERE s.user_id = users.id)`); err != nil {
			return fmt.Errorf("greška pri popunjavanju zadnjih prijava: %w", err)
		}
	}

	for _, fix := range dataFixups {
		if _, err := database.Exec(fix); err != nil {
			return fmt.Errorf("greška pri usklađivanju podataka (%s): %w", fix, err)
		}
	}

	return rekeySeedIdentities(database)
}

// rekeySeedIdentities prevodi zapise seedane starim, nasumičnim UUID-ovima
// na determinističke — bez toga bi dva čvora imala istu postaju pod dva
// identiteta i sinkronizacija bi pukla. Prekodiraju se i sve reference:
// veze s dionicama, zaduženja, sesije i knjiga verzija (i sadržaj verzija,
// koji identifikator nosi kao tekst).
func rekeySeedIdentities(database *sql.DB) error {
	type rename struct{ old, new string }

	// postaje: identitet iz šifre
	var stations []rename
	rows, err := database.Query(`SELECT id, code FROM stations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, code string
		if err := rows.Scan(&id, &code); err != nil {
			rows.Close()
			return err
		}
		if want := StableID("station", code).String(); want != id {
			stations = append(stations, rename{id, want})
		}
	}
	rows.Close()

	// korisnici: identitet iz korisničkog imena
	var users []rename
	rows, err = database.Query(`SELECT id, username FROM users`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, username string
		if err := rows.Scan(&id, &username); err != nil {
			rows.Close()
			return err
		}
		if want := StableID("user", username).String(); want != id {
			users = append(users, rename{id, want})
		}
	}
	rows.Close()

	if len(stations) == 0 && len(users) == 0 {
		return nil
	}

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Roditelj i djeca mijenjaju identifikator u istoj transakciji; provjera
	// stranih ključeva se odgađa do commita, kad je sve opet dosljedno
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}

	replaceEverywhere := func(old, new string, stmts ...string) error {
		for _, st := range stmts {
			if _, err := tx.Exec(st, new, old); err != nil {
				return fmt.Errorf("prekodiranje %s → %s (%s): %w", old, new, st, err)
			}
		}
		// knjiga verzija nosi identifikator i u ključu i u sadržaju
		if _, err := tx.Exec(`UPDATE record_versions SET entity_id = REPLACE(entity_id, ?, ?), payload = REPLACE(payload, ?, ?)
			WHERE entity_id LIKE '%' || ? || '%' OR payload LIKE '%' || ? || '%'`, old, new, old, new, old, old); err != nil {
			return err
		}
		return nil
	}

	for _, r := range stations {
		if err := replaceEverywhere(r.old, r.new,
			`UPDATE stations SET id = ? WHERE id = ?`,
			`UPDATE section_stations SET station_id = ? WHERE station_id = ?`,
		); err != nil {
			return err
		}
	}
	// veze postaja i dionica: i njihov id slijedi iz para
	linkRows, err := tx.Query(`SELECT id, section_code, station_id FROM section_stations`)
	if err != nil {
		return err
	}
	var links []rename
	for linkRows.Next() {
		var id, code, station string
		if err := linkRows.Scan(&id, &code, &station); err != nil {
			linkRows.Close()
			return err
		}
		if want := StableID("section_station", code+"|"+station).String(); want != id {
			links = append(links, rename{id, want})
		}
	}
	linkRows.Close()
	for _, r := range links {
		if _, err := tx.Exec(`UPDATE section_stations SET id = ? WHERE id = ?`, r.new, r.old); err != nil {
			return err
		}
	}

	for _, r := range users {
		if err := replaceEverywhere(r.old, r.new,
			`UPDATE users SET id = ? WHERE id = ?`,
			`UPDATE duties SET user_id = ? WHERE user_id = ?`,
			`UPDATE duties SET assigned_by = ? WHERE assigned_by = ?`,
			`UPDATE sessions SET user_id = ? WHERE user_id = ?`,
		); err != nil {
			return err
		}
	}
	// zaduženja: identitet iz korisničkog imena i rednog broja, kao u seedu
	dutyRows, err := tx.Query(`
		SELECT d.id, u.username FROM duties d JOIN users u ON u.id = d.user_id
		ORDER BY u.username, d.rowid`)
	if err != nil {
		return err
	}
	var duties []rename
	index := map[string]int{}
	for dutyRows.Next() {
		var id, username string
		if err := dutyRows.Scan(&id, &username); err != nil {
			dutyRows.Close()
			return err
		}
		want := StableID("duty", fmt.Sprintf("%s|%d", username, index[username])).String()
		index[username]++
		if want != id {
			duties = append(duties, rename{id, want})
		}
	}
	dutyRows.Close()
	for _, r := range duties {
		if err := replaceEverywhere(r.old, r.new, `UPDATE duties SET id = ? WHERE id = ?`); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if len(stations)+len(users)+len(duties)+len(links) > 0 {
		fmt.Printf("Identiteti prekodirani na determinističke: %d postaja, %d veza, %d korisnika, %d zaduženja\n",
			len(stations), len(links), len(users), len(duties))
	}
	return nil
}

func columnExists(database *sql.DB, table, column string) (bool, error) {
	var count int
	err := database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("greška pri provjeri stupca %s.%s: %w", table, column, err)
	}
	return count > 0, nil
}
