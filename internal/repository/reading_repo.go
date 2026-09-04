package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// EntityReadings je naziv entiteta očitanja u knjizi verzija
const EntityReadings = "readings"

const readingColumns = `id, station_id, structure_id, measured_at, level_cm, level2_cm, source, origin, source_ref,
	observer, user_id, structure_state, gate, ag_hours_1, ag_hours_2, ag_hours_3, note, created_at, updated_at`

// ReadingRepository čuva očitanja vodostaja. Svaki upis ostavlja verziju u
// knjizi — i uvoz, jer uvoz radi jedan čvor, a očitanja trebaju svi.
type ReadingRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewReadingRepository(db *sql.DB, rec *ledger.Recorder) *ReadingRepository {
	return &ReadingRepository{db: db, rec: rec}
}

func readingArgs(rd *models.Reading) []any {
	return []any{
		rd.ID.String(), rd.StationID, rd.StructureID, rd.MeasuredAt.UTC(), rd.LevelCm, rd.Level2Cm,
		rd.Source, rd.Origin, rd.SourceRef, rd.Observer, rd.UserID, rd.StructureState, rd.Gate,
		rd.AgHours1, rd.AgHours2, rd.AgHours3, rd.Note, rd.CreatedAt.UTC(), rd.UpdatedAt.UTC(),
	}
}

func scanReading(scanner interface{ Scan(...any) error }) (models.Reading, error) {
	var rd models.Reading
	var id string
	var level, level2, ag1, ag2, ag3 sql.NullInt64
	err := scanner.Scan(&id, &rd.StationID, &rd.StructureID, &rd.MeasuredAt, &level, &level2,
		&rd.Source, &rd.Origin, &rd.SourceRef, &rd.Observer, &rd.UserID, &rd.StructureState, &rd.Gate,
		&ag1, &ag2, &ag3, &rd.Note, &rd.CreatedAt, &rd.UpdatedAt)
	if err != nil {
		return rd, err
	}
	rd.ID, _ = uuid.Parse(id)
	rd.LevelCm = nullInt(level)
	rd.Level2Cm = nullInt(level2)
	rd.AgHours1 = nullInt(ag1)
	rd.AgHours2 = nullInt(ag2)
	rd.AgHours3 = nullInt(ag3)
	return rd, nil
}

func nullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// ReadingFilter sužava popis očitanja; prazno polje ne filtrira
type ReadingFilter struct {
	StationID   string
	StructureID string
	From        time.Time
	To          time.Time
	Limit       int
}

// List vraća očitanja od najnovijeg prema starijem
func (r *ReadingRepository) List(ctx context.Context, f ReadingFilter) ([]models.Reading, error) {
	var where []string
	var args []any
	if f.StationID != "" {
		where = append(where, "station_id = ?")
		args = append(args, f.StationID)
	}
	if f.StructureID != "" {
		where = append(where, "structure_id = ?")
		args = append(args, f.StructureID)
	}
	if !f.From.IsZero() {
		where = append(where, "measured_at >= ?")
		args = append(args, f.From.UTC())
	}
	if !f.To.IsZero() {
		where = append(where, "measured_at <= ?")
		args = append(args, f.To.UTC())
	}
	q := "SELECT " + readingColumns + " FROM readings"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY measured_at DESC, created_at DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Reading
	for rows.Next() {
		rd, err := scanReading(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rd)
	}
	return out, rows.Err()
}

// Get vraća jedno očitanje ili nil
func (r *ReadingRepository) Get(ctx context.Context, id uuid.UUID) (*models.Reading, error) {
	return getReadingTx(ctx, r.db, id.String())
}

func getReadingTx(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (*models.Reading, error) {
	rd, err := scanReading(q.QueryRowContext(ctx, "SELECT "+readingColumns+" FROM readings WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rd, nil
}

// LatestPerGauge vraća zadnja dva očitanja svake letve (ključ GaugeKey),
// prvo najnovije — dovoljno za pregled sa smjerom promjene.
func (r *ReadingRepository) LatestPerGauge(ctx context.Context) (map[string][]models.Reading, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+readingColumns+` FROM (
		SELECT `+readingColumns+`,
			ROW_NUMBER() OVER (PARTITION BY station_id, structure_id ORDER BY measured_at DESC, created_at DESC) AS rn
		FROM readings) WHERE rn <= 2 ORDER BY measured_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]models.Reading{}
	for rows.Next() {
		rd, err := scanReading(rows)
		if err != nil {
			return nil, err
		}
		out[rd.GaugeKey()] = append(out[rd.GaugeKey()], rd)
	}
	return out, rows.Err()
}

// CountPerGauge broji očitanja po letvi
func (r *ReadingRepository) CountPerGauge(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT station_id, structure_id, COUNT(*) FROM readings GROUP BY station_id, structure_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st, obj string
		var n int
		if err := rows.Scan(&st, &obj, &n); err != nil {
			return nil, err
		}
		out[(models.Reading{StationID: st, StructureID: obj}).GaugeKey()] = n
	}
	return out, rows.Err()
}

// Create upisuje novo očitanje i bilježi verziju
func (r *ReadingRepository) Create(ctx context.Context, rd *models.Reading) error {
	if rd.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		rd.ID = id
	}
	now := time.Now().UTC()
	rd.CreatedAt, rd.UpdatedAt = now, now

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO readings (`+readingColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, readingArgs(rd)...); err != nil {
		return fmt.Errorf("greška pri upisu očitanja: %w", err)
	}
	saved, err := getReadingTx(ctx, tx, rd.ID.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityReadings, rd.ID.String(), saved); err != nil {
		return err
	}
	return tx.Commit()
}

// Update mijenja postojeće očitanje i bilježi verziju
func (r *ReadingRepository) Update(ctx context.Context, rd *models.Reading) error {
	rd.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE readings SET measured_at = ?, level_cm = ?, level2_cm = ?, source = ?,
		observer = ?, structure_state = ?, gate = ?, ag_hours_1 = ?, ag_hours_2 = ?, ag_hours_3 = ?, note = ?, updated_at = ?
		WHERE id = ?`,
		rd.MeasuredAt.UTC(), rd.LevelCm, rd.Level2Cm, rd.Source, rd.Observer, rd.StructureState, rd.Gate,
		rd.AgHours1, rd.AgHours2, rd.AgHours3, rd.Note, rd.UpdatedAt, rd.ID.String())
	if err != nil {
		return fmt.Errorf("greška pri izmjeni očitanja: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("očitanje ne postoji")
	}
	saved, err := getReadingTx(ctx, tx, rd.ID.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityReadings, rd.ID.String(), saved); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete briše očitanje i arhivira verziju
func (r *ReadingRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, err := getReadingTx(ctx, tx, id.String())
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("očitanje ne postoji")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM readings WHERE id = ?`, id.String()); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityReadings, id.String(), existing); err != nil {
		return err
	}
	return tx.Commit()
}

// ExistingIDs vraća skup identifikatora očitanja s danim podrijetlom — uvoz
// preskače što već ima, pa se smije ponavljati dok stari sustav još radi.
func (r *ReadingRepository) ExistingIDs(ctx context.Context, origin string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM readings WHERE origin = ?`, origin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ImportBatch upisuje niz očitanja u jednoj transakciji, svako s verzijom.
// Postojeći identifikatori se preskaču (identitet uvoza je stabilan).
func (r *ReadingRepository) ImportBatch(ctx context.Context, readings []models.Reading) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO readings (`+readingColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	inserted := 0
	for i := range readings {
		rd := &readings[i]
		if rd.ID == uuid.Nil {
			return inserted, fmt.Errorf("očitanje bez identifikatora")
		}
		if rd.CreatedAt.IsZero() {
			rd.CreatedAt = time.Now().UTC()
		}
		if rd.UpdatedAt.IsZero() {
			rd.UpdatedAt = rd.CreatedAt
		}
		res, err := stmt.ExecContext(ctx, readingArgs(rd)...)
		if err != nil {
			return inserted, fmt.Errorf("greška pri uvozu očitanja %s: %w", rd.SourceRef, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if _, err := r.rec.Record(ctx, tx, EntityReadings, rd.ID.String(), rd); err != nil {
			return inserted, err
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// Stats vraća ukupan broj očitanja i raspon vremena
func (r *ReadingRepository) Stats(ctx context.Context) (total int, first, last time.Time, err error) {
	var f, l sql.NullTime
	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*), MIN(measured_at), MAX(measured_at) FROM readings`).Scan(&total, &f, &l)
	if f.Valid {
		first = f.Time
	}
	if l.Valid {
		last = l.Time
	}
	return
}

// GaugeHabit je koliko je puta i u koje doba dana osoba očitavala jednu letvu
type GaugeHabit struct {
	Count    int
	UsualMin int // prosječna minuta u danu (hrvatsko vrijeme)
}

// HabitsFor vraća letve koje je osoba očitavala od zadanog trenutka: ili je
// upisala kao korisnik goCOP-a, ili je zapisana kao očitavač (uvezeni zapisi)
func (r *ReadingRepository) HabitsFor(ctx context.Context, userID, observer string, since time.Time) (map[string]GaugeHabit, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT station_id, structure_id, measured_at FROM readings
		WHERE measured_at >= ? AND ((user_id != '' AND user_id = ?) OR (observer != '' AND observer = ?))`,
		since.UTC(), userID, observer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sum := map[string]int{}
	out := map[string]GaugeHabit{}
	for rows.Next() {
		var st, obj string
		var at time.Time
		if err := rows.Scan(&st, &obj, &at); err != nil {
			return nil, err
		}
		key := (models.Reading{StationID: st, StructureID: obj}).GaugeKey()
		lt := at.In(models.Zagreb)
		h := out[key]
		h.Count++
		sum[key] += lt.Hour()*60 + lt.Minute()
		out[key] = h
	}
	for key, h := range out {
		h.UsualMin = sum[key] / h.Count
		out[key] = h
	}
	return out, rows.Err()
}
