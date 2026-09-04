package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// Popravci podataka koji se izvode jednom, a mijenjaju sinkronizirane
// zapise. Za razliku od migracija sheme, ovi prolaze kroz knjigu verzija:
// ispravak titule je nova verzija zapisa kao i svaki drugi upis, pa stiže
// na ostale čvorove i ostaje u povijesti. Svaki popravak ima ime i izvodi
// se samo jednom po čvoru (tablica data_fixups).

type fixup struct {
	name string
	run  func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error)
}

var fixups = []fixup{
	{"titule-bez-organizacije-2026-09", fixupTitles},
	{"korisnicko-ime-tkraljevic-2026-09", fixupAuthorUsername},
	{"kontakti-autor-i-admin-2026-09", fixupContacts},
	{"admin-alias-eposta-2026-09", fixupAdminEmail},
	{"novo-virje-jedna-letva-2026-09", fixupNovoVirje},
	{"uzvodne-postaje-i-kote-2026-09", fixupUpstreamStations},
}

// RunFixups izvodi popravke koji na ovom čvoru još nisu izvedeni
func RunFixups(ctx context.Context, db *sql.DB, rec *ledger.Recorder) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS data_fixups (
		name TEXT PRIMARY KEY, applied_at DATETIME NOT NULL, changed INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return err
	}
	for _, f := range fixups {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM data_fixups WHERE name = ?`, f.name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		changed, err := f.run(ctx, tx, rec)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("popravak %s: %w", f.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO data_fixups (name, applied_at, changed) VALUES (?, ?, ?)`,
			f.name, time.Now().UTC(), changed); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if changed > 0 {
			log.Printf("Popravak podataka %s: promijenjeno %d zapisa", f.name, changed)
		}
	}
	return nil
}

// orgInTitle hvata naziv organizacije zalijepljen za titulu ("dipl.ing.građ.
// Hrvatske vode") — trag uvoza iz imenika gdje su stupci bili spojeni
var orgInTitle = regexp.MustCompile(`\s+(Hrvatske vode|VGO\b.*|VGI\b.*|.*d\.o\.o\..*)$`)

// CleanTitle vraća titulu bez organizacije i bez očitih zatipaka
func CleanTitle(title string) string {
	t := strings.TrimSpace(title)
	t = orgInTitle.ReplaceAllString(t, "")
	t = strings.ReplaceAll(t, "ing. aedif.", "ing.aedif.")
	t = strings.ReplaceAll(t, "ing.grad.", "ing.građ.")
	if strings.HasSuffix(t, "aedif") {
		t += "."
	}
	return strings.TrimSpace(t)
}

// fixupTitles čisti titule svih djelatnika kojima je uvoz zalijepio
// organizaciju ili ostavio zatipak; svaka promjena je nova verzija zapisa
func fixupTitles(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, title FROM users WHERE title <> ''`)
	if err != nil {
		return 0, err
	}
	type row struct{ id, title string }
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title); err != nil {
			rows.Close()
			return 0, err
		}
		if clean := CleanTitle(r.title); clean != r.title {
			todo = append(todo, row{r.id, clean})
		}
	}
	rows.Close()

	for _, r := range todo {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET title = ?, updated_at = ? WHERE id = ?`,
			r.title, time.Now().UTC(), r.id); err != nil {
			return 0, err
		}
		saved, err := getUserTx(ctx, tx, r.id)
		if err != nil {
			return 0, err
		}
		if _, err := rec.Record(ctx, tx, EntityUsers, r.id, versionOfUser(saved)); err != nil {
			return 0, err
		}
	}
	return len(todo), nil
}

// fixupAuthorUsername: račun autora dobiva korisničko ime po istom pravilu
// kao i svi ostali (početno slovo imena + prezime); "tomislav" je bio
// ostatak prvih dana razvoja
func fixupAuthorUsername(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username = 'tomislav'`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var taken int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = 'tkraljevic'`).Scan(&taken); err != nil {
		return 0, err
	}
	if taken > 0 {
		return 0, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET username = 'tkraljevic', updated_at = ? WHERE id = ?`,
		time.Now().UTC(), id); err != nil {
		return 0, err
	}
	saved, err := getUserTx(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if _, err := rec.Record(ctx, tx, EntityUsers, id, versionOfUser(saved)); err != nil {
		return 0, err
	}
	return 1, nil
}

// adminAliasEmail je adresa rezervnog računa "admin". Nije copos@voda.hr:
// ta adresa nije sandučić nego popis svih sudionika obrane od poplava, pa
// bi poruka upućena administratoru otišla svima. Dok se administratorski
// računi ne dodijele informatičarima i COP-ovima na njihove službene
// adrese, rezervni račun drži održavatelj programa.
const adminAliasEmail = "tomislav.kraljevic@voda.hr"

// fixupAdminEmail mijenja adresu rezervnog računa "admin" na čvorovima koji
// su ranije dobili copos@voda.hr
func fixupAdminEmail(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
	var id, email string
	err := tx.QueryRowContext(ctx, `SELECT id, email FROM users WHERE username = 'admin'`).Scan(&id, &email)
	if err == sql.ErrNoRows || (err == nil && email == adminAliasEmail) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET email = ?, updated_at = ? WHERE id = ?`,
		adminAliasEmail, time.Now().UTC(), id); err != nil {
		return 0, err
	}
	saved, err := getUserTx(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if _, err := rec.Record(ctx, tx, EntityUsers, id, versionOfUser(saved)); err != nil {
		return 0, err
	}
	return 1, nil
}

// fixupContacts vraća službene kontakte na dva računa prvog čvora:
// autoru, kojem su pri probama ostali izmišljeni brojevi, i aliasu "admin",
// koji je pri prvom punjenju baze dobio autorove osobne brojeve umjesto
// službenog kontakta Centra obrane od poplava.
func fixupContacts(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
	type contact struct{ phone, mobile, short, email string }
	want := map[string]contact{
		"tkraljevic": {"031-252-852", "099-267-9587", "2442", "tomislav.kraljevic@voda.hr"},
		"admin":      {"031/252-802", "", "2802", adminAliasEmail},
	}
	changed := 0
	for username, c := range want {
		var id, phone, mobile, short, email string
		err := tx.QueryRowContext(ctx,
			`SELECT id, phone, mobile_phone, short_phone, email FROM users WHERE username = ?`, username).
			Scan(&id, &phone, &mobile, &short, &email)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return changed, err
		}
		if phone == c.phone && mobile == c.mobile && short == c.short && email == c.email {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE users SET phone = ?, mobile_phone = ?, short_phone = ?, email = ?, updated_at = ? WHERE id = ?`,
			c.phone, c.mobile, c.short, c.email, time.Now().UTC(), id); err != nil {
			return changed, err
		}
		saved, err := getUserTx(ctx, tx, id)
		if err != nil {
			return changed, err
		}
		if _, err := rec.Record(ctx, tx, EntityUsers, id, versionOfUser(saved)); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// fixupNovoVirje spaja "Novo Virje" i "Novo Virje-skela" u jedan zapis.
// To je ista letva na Dravi na rkm 200,60: dokumentacija je u sektoru A
// zove skelom, a u sektoru B bez toga, pa je pri čitanju nastala dvojnica s
// istim pragovima i istim zabilježenim maksimumom. Dionice obiju zapisa
// prelaze na preživjeli, a dvojnica se briše.
func fixupNovoVirje(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
	var keepID, dropID, voda, vodaSifra, izvor string
	err := tx.QueryRowContext(ctx, `SELECT id FROM stations WHERE code = 'novo-virje'`).Scan(&keepID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	err = tx.QueryRowContext(ctx,
		`SELECT id, watercourse, watercourse_code, source_name FROM stations WHERE code = 'novo-virje-skela'`).
		Scan(&dropID, &voda, &vodaSifra, &izvor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	// dionice dvojnice prelaze na preživjeli zapis
	rows, err := tx.QueryContext(ctx, `SELECT section_code FROM section_stations WHERE station_id = ?`, dropID)
	if err != nil {
		return 0, err
	}
	var sections []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return 0, err
		}
		sections = append(sections, code)
	}
	rows.Close()

	changed := 0
	keepUUID, err := uuid.Parse(keepID)
	if err != nil {
		return 0, err
	}
	for _, code := range sections {
		linkID, err := uuid.NewV7()
		if err != nil {
			return changed, err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO section_stations (id, section_code, station_id, created_at)
			VALUES (?, ?, ?, ?) ON CONFLICT(section_code, station_id) DO NOTHING`,
			linkID.String(), code, keepID, time.Now().UTC())
		if err != nil {
			return changed, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			if _, err := rec.Record(ctx, tx, EntitySectionStations, sectionStationKey(code, keepUUID),
				map[string]string{"id": linkID.String(), "section_code": code, "station_id": keepID}); err != nil {
				return changed, err
			}
			changed++
		}
		dropUUID, err := uuid.Parse(dropID)
		if err != nil {
			return changed, err
		}
		if _, err := rec.Archive(ctx, tx, EntitySectionStations, sectionStationKey(code, dropUUID),
			map[string]string{"section_code": code, "station_id": dropID}); err != nil {
			return changed, err
		}
	}

	// voda je bila upisana samo na dvojnici
	if _, err := tx.ExecContext(ctx, `UPDATE stations SET
			watercourse = CASE WHEN watercourse = '' THEN ? ELSE watercourse END,
			watercourse_code = CASE WHEN watercourse_code = '' THEN ? ELSE watercourse_code END,
			notes = TRIM(notes || CASE WHEN notes = '' THEN '' ELSE ' ' END || 'U dokumentaciji sektora A vodi se kao „Novo Virje-skela“; ista letva.'),
			updated_at = ?
		WHERE id = ?`, voda, vodaSifra, time.Now().UTC(), keepID); err != nil {
		return changed, err
	}
	saved, err := getStationTx(ctx, tx, keepID)
	if err != nil {
		return changed, err
	}
	if _, err := rec.Record(ctx, tx, EntityStations, keepID, saved); err != nil {
		return changed, err
	}
	changed++

	dropped, err := getStationTx(ctx, tx, dropID)
	if err != nil {
		return changed, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM section_stations WHERE station_id = ?`, dropID); err != nil {
		return changed, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM stations WHERE id = ?`, dropID); err != nil {
		return changed, err
	}
	if _, err := rec.Archive(ctx, tx, EntityStations, dropID, dropped); err != nil {
		return changed, err
	}
	changed++
	return changed, nil
}

// Postaje koje centar prati u svojoj tablici, a nema ih u Privitku kao
// mjerodavne, pa ih registar nije imao: uzvodne strane postaje na Muri,
// Dravi i Dunavu te Donji Miholjac.
//
// Kote nule dunavskih postaja vođene su nad Baltikom, kako piše u zaglavlju
// tablice (m n.B.m. + 0,675 = m n.J.m.). Naše i alpske postaje su u Trstu.
// Zato svaka postaja nosi svoj visinski sustav umjesto da se brojka
// prepravlja; preračun bez zapisanog sustava je najbrži put do krive kote.
type upstreamStation struct {
	code, name, watercourse, stationing string
	zero                                float64
	system                              string
	prep, regular, emerg, state         int
	recordCm, minCm                     int
	country                             string
	record                              string // zapis maksimuma iz Privitka, kad ga ima
}

var upstreamStations = []upstreamStation{
	{code: "gornja-radgona", name: "Gornja Radgona (SI)", watercourse: "Mura", stationing: "rkm 108,50", zero: 202.34, system: "TRST", recordCm: 472, minCm: 90, country: "Slovenija"},
	{code: "lavamund", name: "Lavamünd (AT)", watercourse: "Drava", stationing: "rkm 455,80", system: "TRST", country: "Austrija"},
	{code: "donji-miholjac", name: "Donji Miholjac", watercourse: "Drava", stationing: "rkm 80,60", zero: 88.570, system: "TRST",
		prep: 300, regular: 400, emerg: 480, state: 500, recordCm: 538, minCm: -145, record: "+538 (22.07.1972.)"},
	{code: "bratislava", name: "Bratislava (SK)", watercourse: "Dunav", stationing: "rkm 1872,0", zero: 129.08, system: "BALTIK", regular: 650, emerg: 750, state: 850, recordCm: 1032, minCm: 106, country: "Slovačka"},
	{code: "komarno", name: "Komárno (SK)", watercourse: "Dunav", stationing: "rkm 1770,0", zero: 104.56, system: "BALTIK", regular: 600, emerg: 640, state: 710, recordCm: 888, minCm: 22, country: "Slovačka"},
	{code: "esztergom", name: "Esztergom (HU)", watercourse: "Dunav", stationing: "rkm 1718,5", zero: 101.64, system: "BALTIK", regular: 500, emerg: 600, state: 650, recordCm: 813, minCm: -21, country: "Mađarska"},
	{code: "budapest", name: "Budapest (HU)", watercourse: "Dunav", stationing: "rkm 1646,5", zero: 95.65, system: "BALTIK", regular: 620, emerg: 700, state: 800, recordCm: 891, minCm: 33, country: "Mađarska"},
	{code: "dunafoldvar", name: "Dunaföldvár (HU)", watercourse: "Dunav", stationing: "rkm 1560,6", zero: 89.58, system: "BALTIK", regular: 600, emerg: 750, state: 850, recordCm: 721, minCm: -199, country: "Mađarska"},
	{code: "baja", name: "Baja (HU)", watercourse: "Dunav", stationing: "rkm 1478,7", zero: 81.72, system: "BALTIK", regular: 700, emerg: 800, state: 900, recordCm: 989, minCm: 27, country: "Mađarska"},
	{code: "mohacs", name: "Mohács (HU)", watercourse: "Dunav", stationing: "rkm 1446,9", zero: 79.88, system: "BALTIK", regular: 700, emerg: 850, state: 950, recordCm: 984, minCm: 50, country: "Mađarska"},
	{code: "bezdan", name: "Bezdan (RS)", watercourse: "Dunav", stationing: "rkm 1425,6", zero: 80.64, system: "BALTIK", regular: 550, emerg: 700, recordCm: 776, minCm: -146, country: "Srbija"},
	{code: "apatin", name: "Apatin (RS)", watercourse: "Dunav", stationing: "rkm 1401,9", zero: 78.84, system: "BALTIK", regular: 600, emerg: 750, recordCm: 825, minCm: -118, country: "Srbija"},
	{code: "bogojevo", name: "Bogojevo (RS)", watercourse: "Dunav", stationing: "rkm 1367,3", zero: 77.46, system: "BALTIK", regular: 600, emerg: 700, recordCm: 817, minCm: -86, country: "Srbija"},
}

// Kote nule iz tablice centra za postaje koje ih u registru nisu imale.
// Ondje gdje ih registar već ima, brojke se poklapaju do zadnje znamenke,
// pa se postojeće ne diraju.
var missingZeroDatums = map[string]float64{
	"mursko-sredisce": 156.29, "botovo": 121.55, "novo-virje": 108.87, "terezino-polje": 100.67,
	"vrbovka": 93.21, "moslavina": 90.94, "belisce": 83.99, "osijek": 81.48, "batina": 80.45,
	"tikves": 79.33, "aljmas": 78.08, "dalj": 75.20, "vukovar": 76.19, "ilok": 73.97,
}

func intOrNull(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func floatOrNull(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

// fixupUpstreamStations otvara uzvodne postaje i popunjava kote nule.
// Pragove postojećih postaja ne dira: mjerodavan je Privitak, a tablica
// centra zna nositi zastario podatak.
func fixupUpstreamStations(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
	changed := 0
	now := time.Now().UTC()
	for _, u := range upstreamStations {
		var id string
		var zero sql.NullFloat64
		var prep sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT id, zero_datum, prep_cm FROM stations WHERE code = ?`, u.code).Scan(&id, &zero, &prep)
		switch {
		case err == sql.ErrNoRows:
			id = db.StableID("station", u.code).String()
			napomena := "Uzvodna postaja koju Centar obrane od poplava prati radi najave vodnog vala."
			if u.country != "" {
				napomena = u.country + ". " + napomena
			}
			if u.minCm != 0 {
				napomena += fmt.Sprintf(" Najniži zabilježeni vodostaj %d cm.", u.minCm)
			}
			zapisMax := u.record
			if zapisMax == "" && u.recordCm != 0 {
				zapisMax = fmt.Sprintf("+%d", u.recordCm)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO stations
				(id, code, name, watercourse, watercourse_source, water_area, stationing,
				 zero_datum, zero_datum_system, zero_datum_new_system,
				 prep_cm, prep_raw, regular_cm, regular_raw, emergency_cm, emergency_raw, state_cm, state_raw,
				 record_cm, record_raw, notes, source_name, needs_review, review_note, created_at, updated_at,
				 watercourse_code)
				VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, 'HVRS71', ?, '', ?, '', ?, '', ?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, u.code, u.name, u.watercourse, models.WatercourseFromOperator, u.stationing,
				floatOrNull(u.zero), u.system,
				intOrNull(u.prep), intOrNull(u.regular), intOrNull(u.emerg), intOrNull(u.state),
				intOrNull(u.recordCm), zapisMax, napomena, u.name,
				boolAsInt(u.prep == 0 && u.regular == 0), reviewNote(u),
				now, now, watercourseCodeFor(u.watercourse)); err != nil {
				return changed, fmt.Errorf("postaja %s: %w", u.code, err)
			}
		case err != nil:
			return changed, err
		default:
			// postoji: dopuni samo ono čega nema
			if zero.Valid && prep.Valid {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE stations SET
					zero_datum = COALESCE(zero_datum, ?), zero_datum_system = CASE WHEN zero_datum IS NULL THEN ? ELSE zero_datum_system END,
					prep_cm = COALESCE(prep_cm, ?), regular_cm = COALESCE(regular_cm, ?),
					emergency_cm = COALESCE(emergency_cm, ?), state_cm = COALESCE(state_cm, ?),
					record_cm = COALESCE(record_cm, ?), updated_at = ?
				WHERE id = ?`, floatOrNull(u.zero), u.system,
				intOrNull(u.prep), intOrNull(u.regular), intOrNull(u.emerg), intOrNull(u.state), intOrNull(u.recordCm),
				now, id); err != nil {
				return changed, err
			}
		}
		saved, err := getStationTx(ctx, tx, id)
		if err != nil {
			return changed, err
		}
		if _, err := rec.Record(ctx, tx, EntityStations, id, saved); err != nil {
			return changed, err
		}
		changed++
	}

	// kote nule postaja koje ih nisu imale
	for code, zero := range missingZeroDatums {
		var id string
		var have sql.NullFloat64
		err := tx.QueryRowContext(ctx, `SELECT id, zero_datum FROM stations WHERE code = ?`, code).Scan(&id, &have)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return changed, err
		}
		if have.Valid {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE stations SET zero_datum = ?, zero_datum_system = 'TRST', updated_at = ? WHERE id = ?`,
			zero, now, id); err != nil {
			return changed, err
		}
		saved, err := getStationTx(ctx, tx, id)
		if err != nil {
			return changed, err
		}
		if _, err := rec.Record(ctx, tx, EntityStations, id, saved); err != nil {
			return changed, err
		}
		changed++
	}

	// Privitak za B.34.14 ima dva dijela: od Svetog Đurađa do mosta Donji
	// Miholjac mjerodavan je vodomjer Donji Miholjac, a odande do Dravice
	// Moslavina. Registar je imao samo drugi dio, pa je i vodomjer bio jedan.
	var miholjacID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM stations WHERE code = 'donji-miholjac'`).Scan(&miholjacID); err == nil {
		var postoji int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sections WHERE code = 'B.34.14'`).Scan(&postoji); err != nil {
			return changed, err
		}
		if postoji > 0 {
			linkID, err := uuid.NewV7()
			if err != nil {
				return changed, err
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO section_stations (id, section_code, station_id, created_at)
				VALUES (?, 'B.34.14', ?, ?) ON CONFLICT(section_code, station_id) DO NOTHING`, linkID.String(), miholjacID, now)
			if err != nil {
				return changed, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				stationUUID, err := uuid.Parse(miholjacID)
				if err != nil {
					return changed, err
				}
				if _, err := rec.Record(ctx, tx, EntitySectionStations, sectionStationKey("B.34.14", stationUUID),
					map[string]string{"id": linkID.String(), "section_code": "B.34.14", "station_id": miholjacID}); err != nil {
					return changed, err
				}
				changed++
			}
		}
	}
	return changed, nil
}

func boolAsInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func reviewNote(u upstreamStation) string {
	if u.prep == 0 && u.regular == 0 {
		return "uzvodna postaja bez pragova obrane u tablici centra"
	}
	return ""
}

// watercourseCodeFor veže uzvodnu postaju na vodu iz registra kad je ime očito
func watercourseCodeFor(name string) string {
	switch name {
	case "Dunav":
		return "rijeka-dunav"
	case "Drava":
		return "rijeka-drava"
	case "Mura":
		return "rijeka-mura"
	}
	return ""
}
