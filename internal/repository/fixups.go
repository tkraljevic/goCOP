package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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
	{
		name: "batina-zero-datum-2025",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			var id string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM stations WHERE code = 'batina'`).Scan(&id); err == sql.ErrNoRows {
				return 0, nil
			} else if err != nil {
				return 0, err
			}

			if _, err := tx.ExecContext(ctx, `UPDATE stations SET
				zero_datum_new = 80.189,
				zero_datum_new_system = 'HVRS71',
				zero_datum_source = 'Geodetski elaborat 250 BATINA, CADCOM',
				zero_datum_method = 'Preuzeta zadana kota i transformirana u HVRS71; letva nije pronađena na terenu.',
				zero_datum_survey_date = '2024-09-10',
				zero_datum_document_date = '2025-01',
				updated_at = ?
				WHERE id = ?`, time.Now().UTC(), id); err != nil {
				return 0, err
			}
			st, err := getStationTx(ctx, tx, id)
			if err != nil {
				return 0, err
			}
			if _, err := rec.Record(ctx, tx, EntityStations, id, st); err != nil {
				return 0, err
			}
			return 1, nil
		},
	},
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
