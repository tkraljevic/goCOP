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

// EntityEpisodes je naziv epizoda obrane u knjizi verzija
const EntityEpisodes = "defense_episodes"

// EpisodeRepository čuva epizode obrane od poplava
type EpisodeRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewEpisodeRepository(db *sql.DB, rec *ledger.Recorder) *EpisodeRepository {
	return &EpisodeRepository{db: db, rec: rec}
}

const episodeColumns = `e.id, e.section_code, e.station_id, e.started_at, e.ended_at,
	e.phase, e.peak_cm, e.peak_at, e.declared_by, e.basis, e.threshold_at, e.ended_by,
	e.origin, e.note, e.created_at, e.updated_at`

func scanEpisode(scanner interface{ Scan(...any) error }) (models.DefenseEpisode, error) {
	var e models.DefenseEpisode
	var id string
	var ended, peakAt, threshold sql.NullTime
	var peak sql.NullInt64
	var phase string
	if err := scanner.Scan(&id, &e.SectionCode, &e.StationID, &e.StartedAt, &ended,
		&phase, &peak, &peakAt, &e.DeclaredBy, &e.Basis, &threshold, &e.EndedBy,
		&e.Origin, &e.Note, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return e, err
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return e, fmt.Errorf("neispravan identifikator epizode %q: %w", id, err)
	}
	e.ID = parsed
	e.Phase = models.DefensePhase(phase)
	if ended.Valid {
		t := ended.Time
		e.EndedAt = &t
	}
	if peakAt.Valid {
		t := peakAt.Time
		e.PeakAt = &t
	}
	if threshold.Valid {
		t := threshold.Time
		e.ThresholdAt = &t
	}
	if peak.Valid {
		v := int(peak.Int64)
		e.PeakCm = &v
	}
	return e, nil
}

// ListEpisodes vraća epizode dionice, najnovija prva; prazna šifra daje sve.
func (r *EpisodeRepository) ListEpisodes(ctx context.Context, sectionCode string) ([]models.DefenseEpisode, error) {
	q := `SELECT ` + episodeColumns + ` FROM defense_episodes e`
	var args []any
	if sectionCode != "" {
		q += ` WHERE e.section_code = ?`
		args = append(args, sectionCode)
	}
	q += ` ORDER BY e.started_at DESC`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu epizoda obrane: %w", err)
	}
	defer rows.Close()
	var out []models.DefenseEpisode
	for rows.Next() {
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EpisodesByStation vraća epizode svih dionica koje se vode po jednoj letvi.
// Ista letva mjerodavna je za više dionica — Batina za cijelo BP 34 — pa se na
// kartici postaje vidi tko je sve po njoj u obrani.
func (r *EpisodeRepository) EpisodesByStation(ctx context.Context, stationID string, limit int) ([]models.DefenseEpisode, error) {
	q := `SELECT ` + episodeColumns + `, s.name FROM defense_episodes e
		LEFT JOIN sections s ON s.code = e.section_code
		WHERE e.station_id = ? ORDER BY e.started_at DESC`
	args := []any{stationID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu epizoda postaje: %w", err)
	}
	defer rows.Close()
	var out []models.DefenseEpisode
	for rows.Next() {
		e, err := scanEpisodeWithSection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// scanEpisodeWithSection čita redak kojem je pridodan naziv dionice.
func scanEpisodeWithSection(rows *sql.Rows) (models.DefenseEpisode, error) {
	var naziv sql.NullString
	e, err := scanEpisode(scanTail{rows: rows, extra: []any{&naziv}})
	if err != nil {
		return e, err
	}
	e.SectionName = naziv.String
	return e, nil
}

// scanTail dodaje stupce na kraj reda, da scanEpisode ostane jedno mjesto na
// kojem se epizoda čita.
type scanTail struct {
	rows  *sql.Rows
	extra []any
}

func (t scanTail) Scan(dest ...any) error { return t.rows.Scan(append(dest, t.extra...)...) }

// OpenEpisode vraća epizodu koja na dionici još traje, ako je ima.
func (r *EpisodeRepository) OpenEpisode(ctx context.Context, sectionCode string) (*models.DefenseEpisode, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+episodeColumns+` FROM defense_episodes e
		WHERE e.section_code = ? AND e.ended_at IS NULL ORDER BY e.started_at DESC LIMIT 1`, sectionCode)
	e, err := scanEpisode(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// SaveEpisode upisuje ili osvježava epizodu i ostavlja verziju u knjizi.
func (r *EpisodeRepository) SaveEpisode(ctx context.Context, e *models.DefenseEpisode) error {
	if e.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		e.ID = id
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO defense_episodes (id, section_code, station_id, started_at, ended_at,
			phase, peak_cm, peak_at, declared_by, basis, threshold_at, ended_by,
			origin, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			section_code = excluded.section_code, station_id = excluded.station_id,
			started_at = excluded.started_at, ended_at = excluded.ended_at,
			phase = excluded.phase, peak_cm = excluded.peak_cm, peak_at = excluded.peak_at,
			declared_by = excluded.declared_by, basis = excluded.basis,
			threshold_at = excluded.threshold_at, ended_by = excluded.ended_by,
			origin = excluded.origin, note = excluded.note, updated_at = excluded.updated_at`,
		e.ID.String(), e.SectionCode, e.StationID, e.StartedAt.UTC(), nullTime(e.EndedAt),
		string(e.Phase), e.PeakCm, nullTime(e.PeakAt), e.DeclaredBy, e.Basis,
		nullTime(e.ThresholdAt), e.EndedBy,
		e.Origin, e.Note, e.CreatedAt.UTC(), e.UpdatedAt); err != nil {
		return fmt.Errorf("upis epizode obrane: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityEpisodes, e.ID.String(), e); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteEpisodesFrom briše epizode dionice utvrđene računom, da se mogu
// preračunati iz novog niza očitanja. Epizode koje je upisao operater ostaju.
func (r *EpisodeRepository) DeleteEpisodesFrom(ctx context.Context, sectionCode, origin string) (int, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM defense_episodes WHERE section_code = ? AND origin = ?`, sectionCode, origin)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

// getEpisodeTx čita epizodu unutar transakcije, za popravke podataka koji
// izmjenu moraju upisati i u knjigu verzija.
func getEpisodeTx(ctx context.Context, tx *sql.Tx, id string) (models.DefenseEpisode, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+episodeColumns+` FROM defense_episodes e WHERE e.id = ?`, id)
	return scanEpisode(row)
}
