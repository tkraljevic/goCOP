package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Nazivi entiteta dnevnika u knjizi verzija
const (
	EntityJournals       = "journals"
	EntityJournalSheets  = "journal_sheets"
	EntityJournalEntries = "journal_entries"
)

// JournalRepository čuva građevinske dnevnike: naslovnice, listove i upise
type JournalRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewJournalRepository(db *sql.DB, rec *ledger.Recorder) *JournalRepository {
	return &JournalRepository{db: db, rec: rec}
}

// dan pretvara datum u tekst dana; dnevnik radi po danima, ne po trenucima
func dayKey(t time.Time) string { return t.In(models.Zagreb).Format("2006-01-02") }

func parseDay(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, models.Zagreb)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullDay(t *time.Time) any {
	if t == nil {
		return nil
	}
	return dayKey(*t)
}

// --- dnevnik ---

const journalColumns = `id, area_id, kind, title, year, contract, reconstruction, section_code, structure_id, contractor, contractor_lead,
	contractor_lead_act, supervisor, supervisor_act, supervisor_deputy, chief_supervisor, investor, started_at, ended_at,
	latitude, longitude, gauges, notes, created_by, created_at, updated_at`

func scanJournal(row rowScanner) (models.Journal, error) {
	var j models.Journal
	var started, ended sql.NullTime
	var recon int
	err := row.Scan(&j.ID, &j.AreaID, &j.Kind, &j.Title, &j.Year, &j.Contract, &recon, &j.SectionCode, &j.StructureID, &j.Contractor,
		&j.ContractorLead, &j.ContractorLeadAct, &j.Supervisor, &j.SupervisorAct, &j.SupervisorDeputy, &j.ChiefSupervisor,
		&j.Investor, &started, &ended, &j.Latitude, &j.Longitude, &j.Gauges, &j.Notes, &j.CreatedBy, &j.CreatedAt, &j.UpdatedAt)
	j.Reconstruction = recon != 0
	if started.Valid {
		t := started.Time
		j.StartedAt = &t
	}
	if ended.Valid {
		t := ended.Time
		j.EndedAt = &t
	}
	return j, err
}

func journalArgs(j *models.Journal) []any {
	return []any{j.ID, j.AreaID, j.Kind, j.Title, j.Year, j.Contract, boolInt(j.Reconstruction), j.SectionCode, j.StructureID, j.Contractor, j.ContractorLead,
		j.ContractorLeadAct, j.Supervisor, j.SupervisorAct, j.SupervisorDeputy, j.ChiefSupervisor, j.Investor, j.StartedAt, j.EndedAt,
		j.Latitude, j.Longitude, j.Gauges, j.Notes, j.CreatedBy, j.CreatedAt, j.UpdatedAt}
}

const journalUpsert = `INSERT INTO journals (` + journalColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		area_id = excluded.area_id, kind = excluded.kind, title = excluded.title, year = excluded.year, contract = excluded.contract,
		reconstruction = excluded.reconstruction,
		section_code = excluded.section_code, structure_id = excluded.structure_id, contractor = excluded.contractor,
		contractor_lead = excluded.contractor_lead, contractor_lead_act = excluded.contractor_lead_act, supervisor = excluded.supervisor,
		supervisor_act = excluded.supervisor_act, supervisor_deputy = excluded.supervisor_deputy, chief_supervisor = excluded.chief_supervisor,
		investor = excluded.investor, started_at = excluded.started_at, ended_at = excluded.ended_at, latitude = excluded.latitude,
		longitude = excluded.longitude, gauges = excluded.gauges, notes = excluded.notes, updated_at = excluded.updated_at`

// ListJournals vraća dnevnike područja, najnoviji prvi; 0 = sva područja
func (r *JournalRepository) ListJournals(ctx context.Context, areaID int) ([]models.Journal, error) {
	q := `SELECT ` + journalColumns + ` FROM journals`
	var args []any
	if areaID > 0 {
		q += ` WHERE area_id = ?`
		args = append(args, areaID)
	}
	q += ` ORDER BY year DESC, kind, section_code, title`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Journal
	for rows.Next() {
		j, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		r.decorateJournal(ctx, &out[i])
	}
	return out, nil
}

func (r *JournalRepository) decorateJournal(ctx context.Context, j *models.Journal) {
	var last sql.NullString
	r.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(date) FROM journal_sheets WHERE journal_id = ?`, j.ID).Scan(&j.SheetCount, &last)
	if last.Valid {
		j.LastSheetOn = last.String
	}
	r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_entries WHERE journal_id = ? AND kind = ? AND status = ? AND voided = 0`,
		j.ID, models.EntryKindTask, models.TaskOpen).Scan(&j.OpenTasks)
	r.db.QueryRowContext(ctx, `SELECT name FROM areas WHERE id = ?`, j.AreaID).Scan(&j.AreaName)
}

// GetJournal vraća dnevnik
func (r *JournalRepository) GetJournal(ctx context.Context, id string) (*models.Journal, error) {
	j, err := scanJournal(r.db.QueryRowContext(ctx, `SELECT `+journalColumns+` FROM journals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.decorateJournal(ctx, &j)
	return &j, nil
}

// SaveJournal upisuje novi dnevnik ili mijenja naslovnicu
// journalChannel je kanal dnevnika: njegovo područje i godina
func journalChannel(j *models.Journal) string {
	year := j.Year
	if year == 0 && j.StartedAt != nil {
		year = j.StartedAt.Year()
	}
	if year == 0 {
		year = j.CreatedAt.Year()
	}
	return ledger.ChannelFor(ledger.ChannelJournals, j.AreaID, year)
}

// channelOfJournal čita kanal dnevnika kojem list ili upis pripada
func channelOfJournal(ctx context.Context, q rowQuerier, journalID string) string {
	var ch string
	_ = q.QueryRowContext(ctx, `SELECT channel FROM journals WHERE id = ?`, journalID).Scan(&ch)
	return ch
}

func (r *JournalRepository) SaveJournal(ctx context.Context, j *models.Journal) error {
	now := time.Now().UTC()
	if j.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		j.ID = id.String()
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now
	}
	j.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, journalUpsert, journalArgs(j)...); err != nil {
		return fmt.Errorf("greška pri upisu dnevnika: %w", err)
	}
	channel := journalChannel(j)
	if _, err := tx.ExecContext(ctx, `UPDATE journals SET channel = ? WHERE id = ?`, channel, j.ID); err != nil {
		return err
	}
	if _, err := r.rec.RecordIn(ctx, tx, channel, EntityJournals, j.ID, j); err != nil {
		return err
	}
	return tx.Commit()
}

// --- list ---

const sheetColumns = `id, journal_id, number, date, label, conditions, temperature, wind_from, wind_to, pressure, precipitation,
	weather_source, water_levels, rating, rating_note, staff, machines, contractor_confirmed_by, contractor_confirmed_at,
	supervisor_confirmed_by, supervisor_confirmed_at, created_by, created_at, updated_at`

func scanSheet(row rowScanner) (models.JournalSheet, error) {
	var s models.JournalSheet
	var date string
	var cAt, sAt sql.NullTime
	err := row.Scan(&s.ID, &s.JournalID, &s.Number, &date, &s.Label, &s.Conditions, &s.Temperature, &s.WindFrom, &s.WindTo, &s.Pressure,
		&s.Precipitation, &s.WeatherSource, &s.WaterLevels, &s.Rating, &s.RatingNote, &s.Staff, &s.Machines,
		&s.ContractorConfirmedBy, &cAt, &s.SupervisorConfirmedBy, &sAt, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	s.Date = parseDay(date)
	if cAt.Valid {
		t := cAt.Time
		s.ContractorConfirmedAt = &t
	}
	if sAt.Valid {
		t := sAt.Time
		s.SupervisorConfirmedAt = &t
	}
	return s, err
}

func sheetArgs(s *models.JournalSheet) []any {
	return []any{s.ID, s.JournalID, s.Number, dayKey(s.Date), s.Label, s.Conditions, s.Temperature, s.WindFrom, s.WindTo, s.Pressure,
		s.Precipitation, s.WeatherSource, s.WaterLevels, s.Rating, s.RatingNote, s.Staff, s.Machines,
		s.ContractorConfirmedBy, s.ContractorConfirmedAt, s.SupervisorConfirmedBy, s.SupervisorConfirmedAt, s.CreatedBy, s.CreatedAt, s.UpdatedAt}
}

const sheetUpsert = `INSERT INTO journal_sheets (` + sheetColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		number = excluded.number, date = excluded.date, label = excluded.label, conditions = excluded.conditions, temperature = excluded.temperature,
		wind_from = excluded.wind_from, wind_to = excluded.wind_to, pressure = excluded.pressure, precipitation = excluded.precipitation,
		weather_source = excluded.weather_source, water_levels = excluded.water_levels, rating = excluded.rating,
		rating_note = excluded.rating_note, staff = excluded.staff, machines = excluded.machines,
		contractor_confirmed_by = excluded.contractor_confirmed_by, contractor_confirmed_at = excluded.contractor_confirmed_at,
		supervisor_confirmed_by = excluded.supervisor_confirmed_by, supervisor_confirmed_at = excluded.supervisor_confirmed_at,
		updated_at = excluded.updated_at`

// ListSheets vraća listove dnevnika, najnoviji prvi
func (r *JournalRepository) ListSheets(ctx context.Context, journalID string) ([]models.JournalSheet, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sheetColumns+` FROM journal_sheets WHERE journal_id = ? ORDER BY date DESC, number DESC`, journalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.JournalSheet
	for rows.Next() {
		s, err := scanSheet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_entries WHERE sheet_id = ?`, out[i].ID).Scan(&out[i].EntryCount)
	}
	return out, nil
}

// GetSheet vraća list po identitetu
func (r *JournalRepository) GetSheet(ctx context.Context, id string) (*models.JournalSheet, error) {
	s, err := scanSheet(r.db.QueryRowContext(ctx, `SELECT `+sheetColumns+` FROM journal_sheets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveSheet upisuje list; novi dobiva sljedeći redni broj u dnevniku
func (r *JournalRepository) SaveSheet(ctx context.Context, s *models.JournalSheet) error {
	now := time.Now().UTC()
	if s.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		s.ID = id.String()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.Number == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM journal_sheets WHERE journal_id = ?`, s.JournalID).Scan(&s.Number); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, sheetUpsert, sheetArgs(s)...); err != nil {
		return fmt.Errorf("greška pri upisu lista: %w", err)
	}
	if _, err := r.rec.RecordIn(ctx, tx, channelOfJournal(ctx, tx, s.JournalID), EntityJournalSheets, s.ID, s); err != nil {
		return err
	}
	return tx.Commit()
}

// --- upis ---

const entryColumns = `e.id, e.journal_id, e.sheet_id, e.number, e.date, e.kind, e.side, e.maintained_water_id, e.section_code, e.place,
	e.work_item_id, e.text, e.hours, e.due_date, e.status, e.parent_id, e.voided, e.void_reason, e.voided_by,
	e.user_id, e.user_name, e.created_at, e.updated_at,
	COALESCE(w.official_name, st.name, mw.name, ''), COALESCE(wi.description, ''), COALESCE(wi.number, '')`

const entryFrom = ` FROM journal_entries e
	LEFT JOIN maintained_waters mw ON mw.id = e.maintained_water_id AND e.maintained_water_id <> ''
	LEFT JOIN watercourses w ON w.code = mw.watercourse_code AND mw.watercourse_code <> ''
	LEFT JOIN structures st ON st.id = mw.structure_id AND mw.structure_id <> ''
	LEFT JOIN work_items wi ON wi.id = e.work_item_id AND e.work_item_id <> ''`

func scanEntry(row rowScanner) (models.JournalEntry, error) {
	var e models.JournalEntry
	var date string
	var due sql.NullString
	var voided int
	err := row.Scan(&e.ID, &e.JournalID, &e.SheetID, &e.Number, &date, &e.Kind, &e.Side, &e.MaintainedWaterID, &e.SectionCode, &e.Place,
		&e.WorkItemID, &e.Text, &e.Hours, &due, &e.Status, &e.ParentID, &voided, &e.VoidReason, &e.VoidedBy,
		&e.UserID, &e.UserName, &e.CreatedAt, &e.UpdatedAt, &e.LocationName, &e.WorkItemText, &e.WorkItemNo)
	e.Date = parseDay(date)
	e.Voided = voided != 0
	if due.Valid && due.String != "" {
		t := parseDay(due.String)
		e.DueDate = &t
	}
	return e, err
}

const entryUpsert = `INSERT INTO journal_entries (id, journal_id, sheet_id, number, date, kind, side, maintained_water_id, section_code, place,
		work_item_id, text, hours, due_date, status, parent_id, voided, void_reason, voided_by, user_id, user_name, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		sheet_id = excluded.sheet_id, number = excluded.number, date = excluded.date, kind = excluded.kind, side = excluded.side,
		maintained_water_id = excluded.maintained_water_id, section_code = excluded.section_code, place = excluded.place,
		work_item_id = excluded.work_item_id, text = excluded.text, hours = excluded.hours, due_date = excluded.due_date,
		status = excluded.status, parent_id = excluded.parent_id, voided = excluded.voided, void_reason = excluded.void_reason,
		voided_by = excluded.voided_by, user_name = excluded.user_name, updated_at = excluded.updated_at`

func entryArgs(e *models.JournalEntry) []any {
	return []any{e.ID, e.JournalID, e.SheetID, e.Number, dayKey(e.Date), e.Kind, e.Side, e.MaintainedWaterID, e.SectionCode, e.Place,
		e.WorkItemID, e.Text, e.Hours, nullDay(e.DueDate), e.Status, e.ParentID, boolInt(e.Voided), e.VoidReason, e.VoidedBy,
		e.UserID, e.UserName, e.CreatedAt, e.UpdatedAt}
}

func (r *JournalRepository) queryEntries(ctx context.Context, where string, args ...any) ([]models.JournalEntry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+entryColumns+entryFrom+` WHERE `+where+` ORDER BY e.date, e.number, e.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.JournalEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EntriesForSheet vraća upise jednog lista
func (r *JournalRepository) EntriesForSheet(ctx context.Context, sheetID string) ([]models.JournalEntry, error) {
	return r.queryEntries(ctx, `e.sheet_id = ?`, sheetID)
}

// OpenTasks vraća otvorene naloge dnevnika
func (r *JournalRepository) OpenTasks(ctx context.Context, journalID string) ([]models.JournalEntry, error) {
	return r.queryEntries(ctx, `e.journal_id = ? AND e.kind = ? AND e.status = ? AND e.voided = 0`, journalID, models.EntryKindTask, models.TaskOpen)
}

// GetEntry vraća upis
func (r *JournalRepository) GetEntry(ctx context.Context, id string) (*models.JournalEntry, error) {
	e, err := scanEntry(r.db.QueryRowContext(ctx, `SELECT `+entryColumns+entryFrom+` WHERE e.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// SaveEntry upisuje upis; novi dobiva sljedeći redni broj u dnevniku. Upisi se
// ne brišu — storniranje je izmjena s razlogom.
func (r *JournalRepository) SaveEntry(ctx context.Context, e *models.JournalEntry) error {
	now := time.Now().UTC()
	if e.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		e.ID = id.String()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if e.Number == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM journal_entries WHERE journal_id = ?`, e.JournalID).Scan(&e.Number); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, entryUpsert, entryArgs(e)...); err != nil {
		return fmt.Errorf("greška pri upisu u dnevnik: %w", err)
	}
	if _, err := r.rec.RecordIn(ctx, tx, channelOfJournal(ctx, tx, e.JournalID), EntityJournalEntries, e.ID, e); err != nil {
		return err
	}
	return tx.Commit()
}

// NumberGaps vraća redne brojeve koji u dnevniku nedostaju — kontrola da
// nijedan upis nije nestao
func (r *JournalRepository) NumberGaps(ctx context.Context, journalID string) ([]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT number FROM journal_entries WHERE journal_id = ? ORDER BY number`, journalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var gaps []int
	expect := 1
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		for expect < n {
			gaps = append(gaps, expect)
			expect++
		}
		if n >= expect {
			expect = n + 1
		}
	}
	return gaps, rows.Err()
}
