package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntitySektorskaIzvjesca je naziv entiteta sektorskih izvješća u knjizi verzija
const EntitySektorskaIzvjesca = "sektorska_izvjesca"

const sektorskoUpsert = `INSERT INTO sektorska_izvjesca (id, journal_id, sektor, dan, sadrzaj, izradio_id, izradio, izradeno_at, predano_at, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET journal_id = excluded.journal_id, sektor = excluded.sektor, dan = excluded.dan,
		sadrzaj = excluded.sadrzaj, izradio_id = excluded.izradio_id, izradio = excluded.izradio,
		izradeno_at = excluded.izradeno_at, predano_at = excluded.predano_at, created_at = excluded.created_at, updated_at = excluded.updated_at`

func sektorskoArgs(i *models.SektorskoIzvjesce) ([]any, error) {
	sadrzaj, err := json.Marshal(i.Sadrzaj)
	if err != nil {
		return nil, err
	}
	var predano any
	if i.PredanoAt != nil {
		predano = i.PredanoAt.UTC()
	}
	return []any{i.ID, i.JournalID, i.Sektor, i.DanKey(), string(sadrzaj), i.IzradioID, i.Izradio, i.IzradenoAt.UTC(), predano, i.CreatedAt, i.UpdatedAt}, nil
}

const sektorskoColumns = `id, journal_id, sektor, dan, sadrzaj, izradio_id, izradio, izradeno_at, predano_at, created_at, updated_at FROM sektorska_izvjesca`

func scanSektorsko(row interface{ Scan(...any) error }) (*models.SektorskoIzvjesce, error) {
	var i models.SektorskoIzvjesce
	var dan, sadrzaj string
	var predano sql.NullTime
	if err := row.Scan(&i.ID, &i.JournalID, &i.Sektor, &dan, &sadrzaj, &i.IzradioID, &i.Izradio, &i.IzradenoAt, &predano, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	i.Dan = parseDay(dan)
	if sadrzaj != "" {
		if err := json.Unmarshal([]byte(sadrzaj), &i.Sadrzaj); err != nil {
			return nil, fmt.Errorf("sadržaj sektorskog izvješća %s: %w", i.ID, err)
		}
	}
	i.IzradenoAt = i.IzradenoAt.In(models.Zagreb)
	if predano.Valid {
		t := predano.Time.In(models.Zagreb)
		i.PredanoAt = &t
	}
	return &i, nil
}

type SektorskaIzvjescaRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewSektorskaIzvjescaRepository(db *sql.DB, rec *ledger.Recorder) *SektorskaIzvjescaRepository {
	return &SektorskaIzvjescaRepository{db: db, rec: rec}
}

// Save upisuje ili mijenja izvješće i bilježi verziju; kanalom dnevnika
// COP-a kad ga ima
func (r *SektorskaIzvjescaRepository) Save(ctx context.Context, i *models.SektorskoIzvjesce) error {
	now := time.Now().UTC()
	if i.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		i.ID = id.String()
	}
	if i.CreatedAt.IsZero() {
		i.CreatedAt = now
	}
	i.UpdatedAt = now
	args, err := sektorskoArgs(i)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, sektorskoUpsert, args...); err != nil {
		return fmt.Errorf("upis sektorskog izvješća: %w", err)
	}
	if i.JournalID != "" {
		_, err = r.rec.RecordIn(ctx, tx, channelOfJournal(ctx, tx, i.JournalID), EntitySektorskaIzvjesca, i.ID, i)
	} else {
		_, err = r.rec.Record(ctx, tx, EntitySektorskaIzvjesca, i.ID, i)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Get vraća izvješće; nil kad ga nema
func (r *SektorskaIzvjescaRepository) Get(ctx context.Context, id string) (*models.SektorskoIzvjesce, error) {
	i, err := scanSektorsko(r.db.QueryRowContext(ctx, `SELECT `+sektorskoColumns+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return i, err
}

// ZaDan vraća izvješće sektora za dan; nil kad ga nema
func (r *SektorskaIzvjescaRepository) ZaDan(ctx context.Context, sektor string, dan time.Time) (*models.SektorskoIzvjesce, error) {
	i, err := scanSektorsko(r.db.QueryRowContext(ctx, `SELECT `+sektorskoColumns+` WHERE sektor = ? AND dan = ?`, sektor, dayKey(dan)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return i, err
}

// List vraća izvješća sektora, najnoviji dan prvi; prazan sektor znači sva
func (r *SektorskaIzvjescaRepository) List(ctx context.Context, sektor string) ([]models.SektorskoIzvjesce, error) {
	q, args := `SELECT `+sektorskoColumns, []any{}
	if sektor != "" {
		q += ` WHERE sektor = ?`
		args = append(args, sektor)
	}
	rows, err := r.db.QueryContext(ctx, q+` ORDER BY dan DESC, sektor`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SektorskoIzvjesce
	for rows.Next() {
		i, err := scanSektorsko(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

// Arhiviraj miče izvješće s površine; u knjizi ostaje arhivirano
func (r *SektorskaIzvjescaRepository) Arhiviraj(ctx context.Context, i *models.SektorskoIzvjesce) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sektorska_izvjesca WHERE id = ?`, i.ID); err != nil {
		return err
	}
	if i.JournalID != "" {
		_, err = r.rec.ArchiveIn(ctx, tx, channelOfJournal(ctx, tx, i.JournalID), EntitySektorskaIzvjesca, i.ID, i)
	} else {
		_, err = r.rec.Archive(ctx, tx, EntitySektorskaIzvjesca, i.ID, i)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
