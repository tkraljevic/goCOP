package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

// Integracijski test nad svježom bazom: puni seed, pa zaključane brojke
// izvođenja. Ove su brojke tijekom razvoja provjeravane ručno desetak puta;
// odavde ih čuva test. Ako se namjerno promijene (novi izvor, bolji parser),
// promijeni se i ovdje — svjesno, s razlogom u commitu.

func freshDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatalf("baza se ne može otvoriti: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := InitSchema(database); err != nil {
		t.Fatalf("shema: %v", err)
	}
	if !UseRepoImenik() {
		t.Skip("imenik.json nije dostupan — osobni podaci djelatnika stoje izvan repozitorija (data/imenik.json)")
	}
	if err := SeedInitialData(database); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return database
}

func count(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("upit %q: %v", query, err)
	}
	return n
}

// Brojke od 5.9.2026., nakon prijepisa dionica s poddionicama: Donji Miholjac
// je vraćen kao vodomjer B.34.14 (postaja i veza više), vode poddionica ulaze
// u registar (devet potoka i kanala retencija), a opis dionice s više
// poddionica počinje prvom pa se obala i stacionaža čitaju iz više dionica.

func TestSeedNaSvjezojBaziDajePoznateBrojke(t *testing.T) {
	database := freshDatabase(t)

	expect := []struct {
		what  string
		query string
		want  int
	}{
		{"dionica", `SELECT COUNT(*) FROM sections`, 465},
		{"vodomjernih postaja", `SELECT COUNT(*) FROM stations`, 255},
		{"veza postaja–dionica", `SELECT COUNT(*) FROM section_stations`, 481},
		{"vodnih tijela", `SELECT COUNT(*) FROM watercourses`, 495},
		{"vodnih tijela iz Odluke", `SELECT COUNT(*) FROM watercourses WHERE origin = 'ODLUKA'`, 367},
		{"dionica s vodom", `SELECT COUNT(*) FROM sections WHERE watercourse_code <> ''`, 464},
		{"postaja s vodom", `SELECT COUNT(*) FROM stations WHERE watercourse_code <> ''`, 227},
		{"dionica s obalom", `SELECT COUNT(*) FROM sections WHERE bank <> ''`, 384},
		{"dionica s rasponom stacionaže", `SELECT COUNT(*) FROM sections WHERE rkm_from IS NOT NULL`, 378},
		{"postaja s kotom u sustavu Trst", `SELECT COUNT(*) FROM stations WHERE zero_datum_system = 'TRST'`, 255},
		{"postaja s potvrđenom HVRS71 kotom", `SELECT COUNT(*) FROM stations WHERE zero_datum_new IS NOT NULL`, 1},
	}

	for _, e := range expect {
		if got := count(t, database, e.query); got != e.want {
			t.Errorf("%s: %d, očekivano %d", e.what, got, e.want)
		}
	}
}

// Seed mora biti idempotentan — svaki start aplikacije ga pokreće
func TestSeedJeIdempotentan(t *testing.T) {
	database := freshDatabase(t)

	before := map[string]int{}
	tables := []string{"sections", "stations", "section_stations", "watercourses", "users", "duties", "counties", "section_territories"}
	for _, tbl := range tables {
		before[tbl] = count(t, database, `SELECT COUNT(*) FROM `+tbl)
	}

	if err := InitSchema(database); err != nil {
		t.Fatalf("druga shema: %v", err)
	}
	if !UseRepoImenik() {
		t.Skip("imenik.json nije dostupan — osobni podaci djelatnika stoje izvan repozitorija (data/imenik.json)")
	}
	if err := SeedInitialData(database); err != nil {
		t.Fatalf("drugi seed: %v", err)
	}

	for _, tbl := range tables {
		if got := count(t, database, `SELECT COUNT(*) FROM `+tbl); got != before[tbl] {
			t.Errorf("%s: drugi seed promijenio broj redaka %d → %d", tbl, before[tbl], got)
		}
	}
}

// Nijedan tekst iz seeda ne smije biti neispravan UTF-8 — jedan takav redak
// već je srušio prikaz stranice
func TestSeedNeOstavljaNeispravanUTF8(t *testing.T) {
	database := freshDatabase(t)

	checks := []struct{ table, column string }{
		{"stations", "name"}, {"stations", "watercourse"}, {"stations", "source_name"},
		{"sections", "description"}, {"watercourses", "official_name"}, {"watercourses", "name"},
	}
	for _, c := range checks {
		rows, err := database.Query(`SELECT ` + c.column + ` FROM ` + c.table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			if !utf8.ValidString(v) {
				t.Errorf("%s.%s: neispravan UTF-8 u %q", c.table, c.column, v)
			}
		}
		rows.Close()
	}
}

// Postaja stoji na jednoj vodi, mjerodavna je za dionice drugih voda —
// provjera na bazi, ne samo na nacrtima
func TestBatinaIVukovarSuNaDunavu(t *testing.T) {
	database := freshDatabase(t)

	for _, name := range []string{"Batina", "Vukovar"} {
		var water, source string
		if err := database.QueryRow(
			`SELECT COALESCE(w.name, ''), st.watercourse_source
			 FROM stations st LEFT JOIN watercourses w ON w.code = st.watercourse_code
			 WHERE st.name = ?`, name).Scan(&water, &source); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if water != "Dunav" {
			t.Errorf("%s → voda %q [%s], očekivano Dunav", name, water, source)
		}
	}

	// Batina je mjerodavna i za dionice potoka Karašice — veza mora postojati
	if got := count(t, database, `
		SELECT COUNT(*) FROM section_stations ss
		JOIN stations st ON st.id = ss.station_id
		JOIN sections s ON s.code = ss.section_code
		JOIN watercourses w ON w.code = s.watercourse_code
		WHERE st.name = 'Batina' AND w.official_name = 'potok Karašica (Baranja)'`); got == 0 {
		t.Error("Batina nije mjerodavna ni za jednu dionicu potoka Karašice (Baranja)")
	}
}

func TestBatinaImaPotvrđenuKotuNuleIzElaborata(t *testing.T) {
	database := freshDatabase(t)

	var oldDatum, newDatum float64
	var oldSystem, newSystem, source, method, surveyDate, documentDate string
	if err := database.QueryRow(`SELECT
		zero_datum, zero_datum_system, zero_datum_new, zero_datum_new_system,
		zero_datum_source, zero_datum_method, zero_datum_survey_date, zero_datum_document_date
		FROM stations WHERE code = 'batina'`).Scan(
		&oldDatum, &oldSystem, &newDatum, &newSystem,
		&source, &method, &surveyDate, &documentDate,
	); err != nil {
		t.Fatal(err)
	}
	if oldDatum != 80.450 || oldSystem != "TRST" {
		t.Errorf("stara kota = %.3f %s, očekivano 80.450 TRST", oldDatum, oldSystem)
	}
	if newDatum != 80.189 || newSystem != "HVRS71" {
		t.Errorf("nova kota = %.3f %s, očekivano 80.189 HVRS71", newDatum, newSystem)
	}
	if source == "" || method == "" || surveyDate != "2024-09-10" || documentDate != "2025-01" {
		t.Errorf("nepotpuno porijeklo kote: source=%q method=%q teren=%q elaborat=%q", source, method, surveyDate, documentDate)
	}
}

// Migracija mora preživjeti bazu stvorenu prije preimenovanja stupca
func TestMigracijaPreimenujeStariStupacDionice(t *testing.T) {
	database, err := OpenDB(filepath.Join(t.TempDir(), "stara.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// stara shema: dionica još ima stupac watercourse
	if _, err := database.Exec(`
		CREATE TABLE sections (
			code TEXT PRIMARY KEY, area_id INTEGER NOT NULL, sector_id TEXT NOT NULL,
			watercourse TEXT NOT NULL, protected_area TEXT, embankments TEXT, structures TEXT,
			gauges TEXT, notes TEXT, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO sections (code, area_id, sector_id, watercourse, created_at, updated_at)
		VALUES ('X.1.1', 1, 'D', 'rijeka Sava, l.o.; rkm 1+000 - 2+000', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}

	if err := InitSchema(database); err != nil {
		t.Fatalf("migracija: %v", err)
	}

	var desc string
	if err := database.QueryRow(`SELECT description FROM sections WHERE code = 'X.1.1'`).Scan(&desc); err != nil {
		t.Fatalf("stupac description nakon migracije: %v", err)
	}
	if desc == "" {
		t.Error("opis dionice izgubljen pri preimenovanju stupca")
	}
	if exists, _ := columnExists(database, "sections", "watercourse"); exists {
		t.Error("stari stupac watercourse još postoji nakon migracije")
	}
}

// Baza seedana starim, nasumičnim identitetima mora se prekodirati na
// determinističke — uključivo veze, zaduženja i knjigu verzija — a strani
// ključevi to ne smiju spriječiti
func TestMigracijaPrekodiraNasumicneIdentitete(t *testing.T) {
	database := freshDatabase(t)

	stableStation := StableID("station", "zupanja").String()
	stableUser := StableID("user", "admin").String()
	oldStation := "01a0698c-4428-7144-9d8d-532c9c75ff0e"
	oldUser := "01a067c8-5682-718e-a5ef-398ad6be1c2f"

	// vrati bazu u stanje prije determinističkih identiteta
	tx, _ := database.Begin()
	tx.Exec(`PRAGMA defer_foreign_keys = ON`)
	for _, stmt := range []string{
		`UPDATE section_stations SET station_id = '` + oldStation + `' WHERE station_id = '` + stableStation + `'`,
		`UPDATE stations SET id = '` + oldStation + `' WHERE id = '` + stableStation + `'`,
		`UPDATE duties SET user_id = '` + oldUser + `' WHERE user_id = '` + stableUser + `'`,
		`UPDATE users SET id = '` + oldUser + `' WHERE id = '` + stableUser + `'`,
		`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version)
		 VALUES ('01a0ffff-0000-7000-8000-000000000001', 'stations', '` + oldStation + `', 'x', '', 0, '{"id":"` + oldStation + `"}', CURRENT_TIMESTAMP, 1)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			t.Fatalf("priprema: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := InitSchema(database); err != nil {
		t.Fatalf("migracija: %v", err)
	}

	if count(t, database, `SELECT COUNT(*) FROM stations WHERE id = ?`, stableStation) != 1 {
		t.Error("postaja nije prekodirana na deterministički identitet")
	}
	if count(t, database, `SELECT COUNT(*) FROM section_stations WHERE station_id = ?`, oldStation) != 0 {
		t.Error("veze još pokazuju na stari identitet postaje")
	}
	if count(t, database, `SELECT COUNT(*) FROM users WHERE id = ?`, stableUser) != 1 {
		t.Error("korisnik nije prekodiran")
	}
	if count(t, database, `SELECT COUNT(*) FROM duties WHERE user_id = ?`, oldUser) != 0 {
		t.Error("zaduženja još pokazuju na stari identitet korisnika")
	}
	if count(t, database, `SELECT COUNT(*) FROM record_versions WHERE entity = 'stations' AND entity_id = ? AND payload LIKE '%' || ? || '%'`,
		stableStation, stableStation) != 1 {
		t.Error("knjiga verzija (ključ i sadržaj) nije prekodirana")
	}
	if count(t, database, `SELECT COUNT(*) FROM section_stations ss LEFT JOIN stations s ON s.id = ss.station_id WHERE s.id IS NULL`) != 0 {
		t.Error("nakon migracije postoje veze bez postaje")
	}
}

// Poddionica nosi mjerodavne postaje u svom zapisu, pa ih prekodiranje mora
// zahvatiti i ondje. Kad ih promaši, zapis pokazuje na identitet kojeg više
// nema i povezivanje poddionica pri sljedećem pokretanju pukne na stranom
// ključu — program se tada uopće ne digne. Baza se ovdje slaže ručno, da
// provjera ne ovisi o imeniku i prijepisu koji stoje izvan repozitorija.
func TestPrekodiranjeZahvacaIPostajeUPoddionicama(t *testing.T) {
	database, err := OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := InitSchema(database); err != nil {
		t.Fatal(err)
	}

	stara := "01a0698c-4428-7144-9d8d-532c9c75ff0e"
	stabilna := StableID("station", "batina").String()
	parts := `[{"seq":1,"station_ids":["` + stara + `"]}]`
	for _, stmt := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (34, 'B', 'BP 34', 'VGI Baranja')`,
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('` + stara + `', 'batina', 'Batina', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO sections (code, area_id, sector_id, description, parts, created_at, updated_at)
		 VALUES ('B.34.1', 34, 'B', 'r. Dunav', '` + parts + `', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at)
		 VALUES ('veza-1', 'B.34.1', '` + stara + `', CURRENT_TIMESTAMP)`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("priprema (%s): %v", stmt, err)
		}
	}

	if err := InitSchema(database); err != nil {
		t.Fatalf("migracija: %v", err)
	}

	if count(t, database, `SELECT COUNT(*) FROM stations WHERE id = ?`, stabilna) != 1 {
		t.Fatal("postaja nije prekodirana na deterministički identitet")
	}
	if n := count(t, database, `SELECT COUNT(*) FROM sections WHERE parts LIKE '%' || ? || '%'`, stara); n != 0 {
		t.Errorf("%d poddionica još navodi stari identitet postaje", n)
	}
	if count(t, database, `SELECT COUNT(*) FROM sections WHERE parts LIKE '%' || ? || '%'`, stabilna) != 1 {
		t.Error("poddionica ne navodi novi identitet postaje")
	}
	// pravi ispit: upravo je ovaj korak pucao pri pokretanju
	if err := LinkAllSections(context.Background(), database); err != nil {
		t.Errorf("povezivanje poddionica nakon prekodiranja: %v", err)
	}
}

// Županije na terenskim računalima nastale su prije stupca za web stranicu;
// migracija ga mora dodati, a postojeći zapisi preživjeti.
func TestMigracijaDodajeWebStranicuZupanije(t *testing.T) {
	database, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// stara shema: županija još nema web stranicu
	if _, err := database.Exec(`
		CREATE TABLE counties (
			id INTEGER PRIMARY KEY, code TEXT UNIQUE, name TEXT NOT NULL, seat TEXT NOT NULL,
			prefect TEXT, area_sqkm INTEGER, population INTEGER, email TEXT, phone TEXT
		);
		INSERT INTO counties (id, code, name, seat) VALUES (14, 'OBŽ', 'Osječko-baranjska županija', 'Osijek');`); err != nil {
		t.Fatal(err)
	}

	if err := InitSchema(database); err != nil {
		t.Fatalf("migracija: %v", err)
	}

	exists, err := columnExists(database, "counties", "website")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("tablica counties nema stupac website nakon migracije")
	}

	var name, site string
	if err := database.QueryRow(`SELECT name, website FROM counties WHERE id = 14`).Scan(&name, &site); err != nil {
		t.Fatalf("čitanje županije nakon migracije: %v", err)
	}
	if name == "" {
		t.Error("zapis županije izgubljen pri migraciji")
	}
	if site != "" {
		t.Errorf("stara županija je dobila adresu %q, očekivano prazno", site)
	}
}
