package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"gocop/internal/ledger"

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
