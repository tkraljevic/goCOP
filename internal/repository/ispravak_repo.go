package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntityIspravci je naziv ispravaka arhive u knjizi verzija
const EntityIspravci = "arhiva_ispravci"

// IspravakRepository čuva ispravke arhivskih vrijednosti. Arhiva ostaje
// netaknuta; ispravak je zaseban zapis koji se pri čitanju stavlja preko nje.
type IspravakRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewIspravakRepository(db *sql.DB, rec *ledger.Recorder) *IspravakRepository {
	return &IspravakRepository{db: db, rec: rec}
}

const ispravakColumns = `id, letva, velicina, korak, vrijeme, vrijednost, staro,
	razlog, ispravio, created_at, updated_at`

func scanIspravak(sc interface{ Scan(...any) error }) (models.ArhivaIspravak, error) {
	var i models.ArhivaIspravak
	var id string
	var staro sql.NullFloat64
	if err := sc.Scan(&id, &i.Letva, &i.Velicina, &i.Korak, &i.Vrijeme, &i.Vrijednost,
		&staro, &i.Razlog, &i.Ispravio, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return i, err
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return i, fmt.Errorf("neispravan identifikator ispravka %q: %w", id, err)
	}
	i.ID = parsed
	if staro.Valid {
		v := staro.Float64
		i.Staro = &v
	}
	return i, nil
}

// ZaNiz vraća ispravke jednog niza u razdoblju, ključem po trenutku.
func (r *IspravakRepository) ZaNiz(ctx context.Context, letva, velicina, korak string,
	od, do time.Time) (map[int64]models.ArhivaIspravak, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+ispravakColumns+` FROM arhiva_ispravci
		WHERE letva=? AND velicina=? AND korak=? AND vrijeme BETWEEN ? AND ?`,
		letva, velicina, korak, od.UTC(), do.UTC())
	if err != nil {
		return nil, fmt.Errorf("dohvat ispravaka: %w", err)
	}
	defer rows.Close()
	out := map[int64]models.ArhivaIspravak{}
	for rows.Next() {
		i, err := scanIspravak(rows)
		if err != nil {
			return nil, err
		}
		out[i.Vrijeme.UTC().Unix()] = i
	}
	return out, rows.Err()
}

// Broj vraća koliko ispravaka niz ima ukupno.
func (r *IspravakRepository) Broj(ctx context.Context, letva, velicina, korak string) (int, error) {
	if r == nil {
		return 0, nil
	}
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM arhiva_ispravci
		WHERE letva=? AND velicina=? AND korak=?`, letva, velicina, korak).Scan(&n)
	return n, err
}

// Spremi upisuje ispravke u jednoj transakciji, svaki sa svojom verzijom.
// Ponovni ispravak istog trenutka mijenja postojeći, ne dodaje drugi.
func (r *IspravakRepository) Spremi(ctx context.Context, ispravci []models.ArhivaIspravak) (int, error) {
	if len(ispravci) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	n := 0
	for idx := range ispravci {
		i := &ispravci[idx]
		var postojeci string
		err := tx.QueryRowContext(ctx, `SELECT id FROM arhiva_ispravci
			WHERE letva=? AND velicina=? AND korak=? AND vrijeme=?`,
			i.Letva, i.Velicina, i.Korak, i.Vrijeme.UTC()).Scan(&postojeci)
		if err == nil {
			if id, e := uuid.Parse(postojeci); e == nil {
				i.ID = id
			}
		} else if err != sql.ErrNoRows {
			return n, err
		}
		if i.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return n, err
			}
			i.ID = id
		}
		now := time.Now().UTC()
		if i.CreatedAt.IsZero() {
			i.CreatedAt = now
		}
		i.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO arhiva_ispravci (`+ispravakColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET vrijednost=excluded.vrijednost, staro=excluded.staro,
				razlog=excluded.razlog, ispravio=excluded.ispravio, updated_at=excluded.updated_at`,
			i.ID.String(), i.Letva, i.Velicina, i.Korak, i.Vrijeme.UTC(), i.Vrijednost,
			i.Staro, i.Razlog, i.Ispravio, i.CreatedAt, i.UpdatedAt); err != nil {
			return n, fmt.Errorf("upis ispravka: %w", err)
		}
		if _, err := r.rec.Record(ctx, tx, EntityIspravci, i.ID.String(), i); err != nil {
			return n, err
		}
		n++
	}
	return n, tx.Commit()
}
