package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
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
		// Epizode obrane rekonstruirane iz niza očitanja upisane su prije nego
		// što je epizoda znala razlikovati proglašenje od prelaska praga. Sve
		// su počele prelaskom praga, pa im se to i upisuje — bez toga bi na
		// kartici stajale bez osnove, kao da su nastale bez razloga.
		name: "epizode-osnova-prag",
		run: func(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder) (int, error) {
			rows, err := tx.QueryContext(ctx, `SELECT id FROM defense_episodes
				WHERE origin = ? AND (basis = '' OR threshold_at IS NULL)`, models.EpisodeFromReadings)
			if err != nil {
				return 0, err
			}
			var ids []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return 0, err
				}
				ids = append(ids, id)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return 0, err
			}

			now := time.Now().UTC()
			for _, id := range ids {
				if _, err := tx.ExecContext(ctx, `UPDATE defense_episodes
					SET basis = ?, threshold_at = started_at, updated_at = ? WHERE id = ?`,
					models.BasisThreshold, now, id); err != nil {
					return 0, err
				}
				e, err := getEpisodeTx(ctx, tx, id)
				if err != nil {
					return 0, err
				}
				if _, err := rec.Record(ctx, tx, EntityEpisodes, id, e); err != nil {
					return 0, err
				}
			}
			return len(ids), nil
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
