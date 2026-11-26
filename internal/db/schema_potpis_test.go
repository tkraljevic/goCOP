package db

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// knjigaIzdanja0068 je tablica knjige verzija kakvu je napravilo izdanje
// 0.0.68-alfa, prije stupaca za potpis autora
const knjigaIzdanja0068 = `
CREATE TABLE record_versions (
	version_id TEXT PRIMARY KEY,
	entity TEXT NOT NULL,
	entity_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	supersedes TEXT NOT NULL DEFAULT '',
	archived INTEGER NOT NULL DEFAULT 0,
	payload TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	schema_version INTEGER NOT NULL DEFAULT 1,
	channel TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_versions_entity ON record_versions(entity, entity_id, version_id);
CREATE INDEX idx_versions_node ON record_versions(node_id, version_id);
CREATE INDEX idx_versions_shema ON record_versions(entity, schema_version) WHERE schema_version > 1;
CREATE INDEX idx_versions_arhivirane ON record_versions(entity, entity_id, version_id) WHERE archived = 1;
`

// bajtoviKnjige su svi stupci knjige iz 0.0.68-alfa, s vrstom vrijednosti,
// redom verzija
func bajtoviKnjige(t *testing.T, database *sql.DB) string {
	t.Helper()
	return tekst(t, database, `SELECT group_concat(redak, char(10)) FROM (
		SELECT quote(version_id) || quote(entity) || quote(entity_id) || quote(node_id) || quote(supersedes)
			|| quote(archived) || quote(payload) || quote(created_at) || typeof(created_at)
			|| quote(schema_version) || quote(channel) AS redak
		FROM record_versions ORDER BY version_id)`)
}

// stupciKnjige su ime, vrsta, NOT NULL i zadana vrijednost svakog stupca
func stupciKnjige(t *testing.T, database *sql.DB) []string {
	t.Helper()
	rows, err := database.Query(`SELECT name, type, "notnull", coalesce(dflt_value, 'NULL') FROM pragma_table_info('record_versions') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ime, vrsta, zadano string
		var obavezan int
		if err := rows.Scan(&ime, &vrsta, &obavezan, &zadano); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s %s %d %s", ime, vrsta, obavezan, zadano))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Baza izdanja 0.0.68-alfa dobije stupce potpis, kljuc_autora i primljeno.
// Nijedan postojeći bajt knjige se ne mijenja, stare verzije ostaju bez
// potpisa i bez vremena primanja, a tablica izgleda isto kao u novoj bazi.
func TestMigracijaStupciPotpisaAutora(t *testing.T) {
	database := praznaBaza(t)
	izvedi(t, database, `DROP TABLE record_versions`, knjigaIzdanja0068,
		`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel) VALUES
			('0190a1b2-0000-7000-8000-000000000001', 'users', 'u1', 'cvor-a', '', 0, '{"full_name":"Ana Horvat"}', '2026-11-20 07:15:00+00:00', 1, ''),
			('0190a1b2-0000-7000-8000-000000000002', 'users', 'u1', 'cvor-b', '0190a1b2-0000-7000-8000-000000000001', 1, '{"full_name":"Ana Horvat","napomena":"čšžćđ"}', '2026-11-21 08:00:00.123456789+00:00', 2, ''),
			('0190a1b2-0000-7000-8000-000000000003', 'readings', 'r1', 'cvor-a', '', 0, '{"value_cm":312}', '2026-11-22 06:00:00+00:00', 1, 'ocitanja/16/2026')`)
	prije := bajtoviKnjige(t, database)

	for range 2 { // drugo pokretanje ne smije ništa promijeniti
		if err := InitSchema(database); err != nil {
			t.Fatal(err)
		}
	}

	if poslije := bajtoviKnjige(t, database); poslije != prije {
		t.Errorf("knjiga se promijenila:\nprije\n%s\nposlije\n%s", prije, poslije)
	}
	if n := tekst(t, database, `SELECT count(*) FROM record_versions WHERE potpis = '' AND kljuc_autora = '' AND primljeno IS NULL`); n != "3" {
		t.Errorf("stare verzije bez potpisa i vremena primanja: %s od 3", n)
	}
	if got, want := strings.Join(stupciKnjige(t, database), "\n"), strings.Join(stupciKnjige(t, praznaBaza(t)), "\n"); got != want {
		t.Errorf("stupci nakon migracije:\n%s\nu novoj bazi:\n%s", got, want)
	}

	// upis kakav knjiga radi danas, bez novih stupaca, dobije zadane vrijednosti
	izvedi(t, database, `INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		VALUES ('0190a1b2-0000-7000-8000-000000000004', 'users', 'u2', 'cvor-a', '', 0, '{}', '2026-11-26 07:00:00+00:00', 1, '')`)
	if got := tekst(t, database, `SELECT quote(potpis) || quote(kljuc_autora) || quote(primljeno) FROM record_versions WHERE version_id = '0190a1b2-0000-7000-8000-000000000004'`); got != "''''NULL" {
		t.Errorf("zadane vrijednosti novog upisa: %s", got)
	}
}

// redakKnjige su svi stupci verzije kao tekst, za usporedbu bajt za bajt
func redakKnjige(t *testing.T, database *sql.DB, versionID string) string {
	t.Helper()
	var r string
	if err := database.QueryRow(`SELECT quote(entity) || quote(entity_id) || quote(node_id) || quote(supersedes) || quote(archived)
		|| quote(payload) || quote(created_at) || quote(schema_version) || quote(channel) || quote(potpis) || quote(kljuc_autora)
		FROM record_versions WHERE version_id = ?`, versionID).Scan(&r); err != nil {
		t.Fatalf("verzija %s: %v", versionID, err)
	}
	return r
}

// Izmjene knjige na mjestu pri otvaranju baze (kanal i stanje prijava,
// prekodiranje postaja) preskaču potpisane verzije, jer bi im potpis
// prestao vrijediti; nepotpisane mijenjaju kao i do sada.
func TestOtvaranjeBazeNeDiraPotpisane(t *testing.T) {
	database := praznaBaza(t)
	staraPostaja := "00000000-0000-4000-8000-0000000000aa"
	for _, q := range []string{
		`INSERT INTO prijave (id, user_id, area_id, datum, created_at, updated_at) VALUES ('pr1', 'u1', 16, '2026-05-01', '2026-05-01', '2026-05-01')`,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('` + staraPostaja + `', 'PROBA', 'Proba', '2026-05-01', '2026-05-01')`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	verzije := []struct{ id, entitet, entitetID, payload, potpis string }{
		{"v-prijava-potpisana", "prijave", "pr1", `{"status":"ARHIVIRANA","arhivirao_id":"u1"}`, "UA=="},
		{"v-prijava", "prijave", "pr1", `{"status":"ARHIVIRANA","arhivirao_id":"u1"}`, ""},
		{"v-postaja-potpisana", "stations", staraPostaja, `{"id":"` + staraPostaja + `"}`, "UA=="},
		{"v-postaja", "stations", staraPostaja, `{"id":"` + staraPostaja + `"}`, ""},
	}
	for _, v := range verzije {
		if _, err := database.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at, potpis, kljuc_autora)
			VALUES (?, ?, ?, 'racunalo-a', ?, '2026-05-01 07:00:00', ?, ?)`, v.id, v.entitet, v.entitetID, v.payload, v.potpis, v.potpis); err != nil {
			t.Fatal(err)
		}
	}
	prije := map[string]string{}
	for _, v := range verzije {
		prije[v.id] = redakKnjige(t, database, v.id)
	}

	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}

	for _, v := range verzije {
		poslije := redakKnjige(t, database, v.id)
		if v.potpis != "" && poslije != prije[v.id] {
			t.Errorf("potpisana %s promijenjena:\n prije   %s\n poslije %s", v.id, prije[v.id], poslije)
		}
		if v.potpis == "" && poslije == prije[v.id] {
			t.Errorf("nepotpisana %s nije promijenjena, pa test ne provjerava ništa", v.id)
		}
	}
	var kanal, payload string
	if err := database.QueryRow(`SELECT channel, payload FROM record_versions WHERE version_id = 'v-prijava'`).Scan(&kanal, &payload); err != nil {
		t.Fatal(err)
	}
	if kanal != "prijave/16/2026" || !strings.Contains(payload, `"RIJESENA"`) || !strings.Contains(payload, `"rijesio_id"`) {
		t.Errorf("nepotpisana prijava: kanal %q, sadržaj %s", kanal, payload)
	}
}
