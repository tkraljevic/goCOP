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
