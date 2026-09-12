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

// EntityDnevnaIzvjesca je naziv entiteta dnevnih izvješća u knjizi verzija
const EntityDnevnaIzvjesca = "dnevna_izvjesca"

// Izvješće putuje kanalom svog dnevnika COP-a kad ga ima, inače zajedničkim
const izvjesceUpsert = `INSERT INTO dnevna_izvjesca (id, journal_id, section_code, dan, stadij, sadrzaj, izradio_id, izradio, izradeno_at, predano_at, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET journal_id = excluded.journal_id, section_code = excluded.section_code, dan = excluded.dan,
		stadij = excluded.stadij, sadrzaj = excluded.sadrzaj, izradio_id = excluded.izradio_id, izradio = excluded.izradio,
		izradeno_at = excluded.izradeno_at, predano_at = excluded.predano_at, created_at = excluded.created_at, updated_at = excluded.updated_at`

func izvjesceArgs(i *models.DnevnoIzvjesce) ([]any, error) {
	sadrzaj, err := json.Marshal(i.Sadrzaj)
	if err != nil {
		return nil, err
	}
	var predano any
	if i.PredanoAt != nil {
		predano = i.PredanoAt.UTC()
	}
	return []any{i.ID, i.JournalID, i.SectionCode, i.DanKey(), string(i.Stadij), string(sadrzaj), i.IzradioID, i.Izradio, i.IzradenoAt.UTC(), predano, i.CreatedAt, i.UpdatedAt}, nil
}

const izvjesceColumns = `i.id, i.journal_id, i.section_code, i.dan, i.stadij, i.sadrzaj, i.izradio_id, i.izradio, i.izradeno_at, i.predano_at, i.created_at, i.updated_at,
	COALESCE(s.sector_id, ''), COALESCE(a.name, '')`
const izvjesceFrom = ` FROM dnevna_izvjesca i LEFT JOIN sections s ON s.code = i.section_code LEFT JOIN areas a ON a.id = s.area_id`

func scanIzvjesce(row interface{ Scan(...any) error }) (*models.DnevnoIzvjesce, error) {
	var i models.DnevnoIzvjesce
	var dan, stadij, sadrzaj string
	var predano sql.NullTime
	if err := row.Scan(&i.ID, &i.JournalID, &i.SectionCode, &dan, &stadij, &sadrzaj, &i.IzradioID, &i.Izradio, &i.IzradenoAt, &predano, &i.CreatedAt, &i.UpdatedAt,
		&i.Sektor, &i.Podrucje); err != nil {
		return nil, err
	}
	i.Dan = parseDay(dan)
	i.Stadij = models.DefensePhase(stadij)
	if sadrzaj != "" {
		if err := json.Unmarshal([]byte(sadrzaj), &i.Sadrzaj); err != nil {
			return nil, fmt.Errorf("sadržaj izvješća %s: %w", i.ID, err)
		}
	}
	i.Vodotok = i.Sadrzaj.Vodotok
	i.IzradenoAt = i.IzradenoAt.In(models.Zagreb)
	if predano.Valid {
		t := predano.Time.In(models.Zagreb)
		i.PredanoAt = &t
	}
	return &i, nil
}

type IzvjescaRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewIzvjescaRepository(db *sql.DB, rec *ledger.Recorder) *IzvjescaRepository {
	return &IzvjescaRepository{db: db, rec: rec}
}

// Save upisuje ili mijenja izvješće i bilježi verziju
func (r *IzvjescaRepository) Save(ctx context.Context, i *models.DnevnoIzvjesce) error {
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
	args, err := izvjesceArgs(i)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, izvjesceUpsert, args...); err != nil {
		return fmt.Errorf("upis dnevnog izvješća: %w", err)
	}
	if i.JournalID != "" {
		_, err = r.rec.RecordIn(ctx, tx, channelOfJournal(ctx, tx, i.JournalID), EntityDnevnaIzvjesca, i.ID, i)
	} else {
		_, err = r.rec.Record(ctx, tx, EntityDnevnaIzvjesca, i.ID, i)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Get vraća izvješće; nil kad ga nema
func (r *IzvjescaRepository) Get(ctx context.Context, id string) (*models.DnevnoIzvjesce, error) {
	i, err := scanIzvjesce(r.db.QueryRowContext(ctx, `SELECT `+izvjesceColumns+izvjesceFrom+` WHERE i.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return i, err
}

// ZaDan vraća izvješće dionice za dan; nil kad ga nema
func (r *IzvjescaRepository) ZaDan(ctx context.Context, sectionCode string, dan time.Time) (*models.DnevnoIzvjesce, error) {
	i, err := scanIzvjesce(r.db.QueryRowContext(ctx, `SELECT `+izvjesceColumns+izvjesceFrom+` WHERE i.section_code = ? AND i.dan = ?`, sectionCode, dayKey(dan)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return i, err
}

// List vraća izvješća, najnoviji dan prvi; filtri su po dnevniku, dionici
// ili danu, prazno znači sve
func (r *IzvjescaRepository) List(ctx context.Context, journalID, sectionCode, sektor string, dan *time.Time) ([]models.DnevnoIzvjesce, error) {
	q := `SELECT ` + izvjesceColumns + izvjesceFrom + ` WHERE 1=1`
	var args []any
	if journalID != "" {
		q += ` AND i.journal_id = ?`
		args = append(args, journalID)
	}
	if sectionCode != "" {
		q += ` AND i.section_code = ?`
		args = append(args, sectionCode)
	}
	if sektor != "" {
		q += ` AND s.sector_id = ?`
		args = append(args, sektor)
	}
	if dan != nil {
		q += ` AND i.dan = ?`
		args = append(args, dayKey(*dan))
	}
	q += ` ORDER BY i.dan DESC, i.section_code`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.DnevnoIzvjesce
	for rows.Next() {
		i, err := scanIzvjesce(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

// Broj broji sva izvješća, za razdjelnicu
func (r *IzvjescaRepository) Broj(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM dnevna_izvjesca`).Scan(&n)
	return n, err
}

// Arhiviraj miče izvješće s površine; u knjizi ostaje arhivirano
func (r *IzvjescaRepository) Arhiviraj(ctx context.Context, i *models.DnevnoIzvjesce) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM dnevna_izvjesca WHERE id = ?`, i.ID); err != nil {
		return err
	}
	if i.JournalID != "" {
		_, err = r.rec.ArchiveIn(ctx, tx, channelOfJournal(ctx, tx, i.JournalID), EntityDnevnaIzvjesca, i.ID, i)
	} else {
		_, err = r.rec.Archive(ctx, tx, EntityDnevnaIzvjesca, i.ID, i)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
