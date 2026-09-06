package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntityStations i EntitySectionStations su nazivi entiteta u knjizi verzija
const (
	EntityStations        = "stations"
	EntitySectionStations = "section_stations"
)

type StationRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

// NewStationRepository traži Recorder: svaki upis ostavlja verziju u knjizi,
// u istoj transakciji u kojoj mijenja površinu
func NewStationRepository(db *sql.DB, rec *ledger.Recorder) *StationRepository {
	return &StationRepository{db: db, rec: rec}
}

// getStationTx čita postaju unutar transakcije — za verziju nakon upisa
func getStationTx(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (models.Station, error) {
	row := q.QueryRowContext(ctx, `SELECT `+stationColumns+` FROM stations s WHERE s.id = ?`, id)
	return scanStation(row)
}

// sectionStationKey je identifikator veze u knjizi verzija
func sectionStationKey(sectionCode string, stationID uuid.UUID) string {
	return sectionCode + "|" + stationID.String()
}

const stationColumns = `
	s.id, s.code, s.name, s.watercourse, s.watercourse_code, s.watercourse_source, s.water_area, s.stationing,
	s.zero_datum, s.zero_datum_system, s.zero_datum_new, s.zero_datum_new_system,
	s.zero_datum_source, s.zero_datum_method, s.zero_datum_survey_date, s.zero_datum_document_date,
	s.zero_datum_history,
	s.prep_cm, s.prep_raw, s.regular_cm, s.regular_raw,
	s.emergency_cm, s.emergency_raw, s.state_cm, s.state_raw,
	s.record_cm, s.record_raw,
	s.notes, s.source_name, s.needs_review, s.review_note,
	s.latitude, s.longitude, s.created_at, s.updated_at
`

// scanStation čita jedan redak registra postaja
func scanStation(scanner interface{ Scan(...any) error }) (models.Station, error) {
	var (
		st        models.Station
		idStr     string
		zeroDatum sql.NullFloat64
		zeroNew   sql.NullFloat64
		prepCm    sql.NullInt64
		regCm     sql.NullInt64
		emgCm     sql.NullInt64
		stateCm   sql.NullInt64
		recordCm  sql.NullInt64
		lat       sql.NullFloat64
		lon       sql.NullFloat64
		needsRev  int
		history   string
	)

	err := scanner.Scan(
		&idStr, &st.Code, &st.Name, &st.Watercourse, &st.WatercourseCode, &st.WatercourseSource, &st.WaterArea, &st.Stationing,
		&zeroDatum, &st.ZeroDatumSystem, &zeroNew, &st.ZeroDatumNewSystem,
		&st.ZeroDatumSource, &st.ZeroDatumMethod, &st.ZeroDatumSurveyDate, &st.ZeroDatumDocumentDate,
		&history,
		&prepCm, &st.Prep.Raw, &regCm, &st.Regular.Raw,
		&emgCm, &st.Emergency.Raw, &stateCm, &st.State.Raw,
		&recordCm, &st.Record.Raw,
		&st.Notes, &st.SourceName, &needsRev, &st.ReviewNote,
		&lat, &lon, &st.CreatedAt, &st.UpdatedAt,
	)
	if err != nil {
		return st, err
	}

	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return st, fmt.Errorf("neispravan identifikator postaje %q: %w", idStr, err)
	}
	st.ID = parsedID
	st.NeedsReview = needsRev != 0

	st.ZeroDatum = nullFloatPtr(zeroDatum)
	st.ZeroDatumNew = nullFloatPtr(zeroNew)
	if history != "" && history != "[]" {
		_ = json.Unmarshal([]byte(history), &st.ZeroDatumHistory)
	}
	st.Latitude = nullFloatPtr(lat)
	st.Longitude = nullFloatPtr(lon)
	st.Prep.Cm = nullIntPtr(prepCm)
	st.Regular.Cm = nullIntPtr(regCm)
	st.Emergency.Cm = nullIntPtr(emgCm)
	st.State.Cm = nullIntPtr(stateCm)
	st.Record.Cm = nullIntPtr(recordCm)

	return st, nil
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	value := int(v.Int64)
	return &value
}

func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	value := v.Float64
	return &value
}

// ListStations vraća registar postaja uz opcionalne filtre
func (r *StationRepository) ListStations(ctx context.Context, search, watercourse string, onlyNeedsReview bool) ([]models.Station, error) {
	query := `SELECT ` + stationColumns + ` FROM stations s WHERE 1=1`
	var args []any

	if term := strings.TrimSpace(search); term != "" {
		query += ` AND (s.name LIKE ? OR s.code LIKE ? OR s.source_name LIKE ?)`
		like := "%" + term + "%"
		args = append(args, like, like, like)
	}
	if wc := strings.TrimSpace(watercourse); wc != "" {
		query += ` AND s.watercourse = ?`
		args = append(args, wc)
	}
	if onlyNeedsReview {
		query += ` AND s.needs_review = 1`
	}
	query += ` ORDER BY s.name COLLATE NOCASE`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju vodomjernih postaja: %w", err)
	}
	defer rows.Close()

	var stations []models.Station
	for rows.Next() {
		st, err := scanStation(rows)
		if err != nil {
			return nil, err
		}
		stations = append(stations, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.attachSectionCodes(ctx, stations); err != nil {
		return nil, err
	}

	return stations, nil
}

// attachSectionCodes popunjava dionice za koje je postaja mjerodavna
func (r *StationRepository) attachSectionCodes(ctx context.Context, stations []models.Station) error {
	if len(stations) == 0 {
		return nil
	}

	byID := make(map[string]int, len(stations))
	for i, st := range stations {
		byID[st.ID.String()] = i
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT station_id, section_code
		FROM section_stations
		ORDER BY section_code
	`)
	if err != nil {
		return fmt.Errorf("greška pri dohvaćanju veza postaja i dionica: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var stationID, sectionCode string
		if err := rows.Scan(&stationID, &sectionCode); err != nil {
			return err
		}
		if idx, ok := byID[stationID]; ok {
			stations[idx].SectionCodes = append(stations[idx].SectionCodes, sectionCode)
		}
	}

	return rows.Err()
}

// GetStationByCode vraća postaju po šifri ili nil
func (r *StationRepository) GetStationByCode(ctx context.Context, code string) (*models.Station, error) {
	st, err := scanStation(r.db.QueryRowContext(ctx, "SELECT "+stationColumns+" FROM stations s WHERE s.code = ?", code))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st.SectionCodes, _ = r.GetSectionCodesForStation(ctx, st.ID)
	return &st, nil
}

// GetStationByID dohvaća jednu postaju s pripadajućim dionicama
func (r *StationRepository) GetStationByID(ctx context.Context, id uuid.UUID) (*models.Station, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+stationColumns+` FROM stations s WHERE s.id = ?`, id.String())

	st, err := scanStation(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju postaje: %w", err)
	}

	codes, err := r.GetSectionCodesForStation(ctx, id)
	if err != nil {
		return nil, err
	}
	st.SectionCodes = codes

	return &st, nil
}

// GetSectionCodesForStation vraća šifre dionica za koje je postaja mjerodavna
func (r *StationRepository) GetSectionCodesForStation(ctx context.Context, stationID uuid.UUID) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT section_code FROM section_stations WHERE station_id = ? ORDER BY section_code
	`, stationID.String())
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju dionica postaje: %w", err)
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
}

// GetStationsForSection vraća mjerodavne vodomjere jedne dionice
func (r *StationRepository) GetStationsForSection(ctx context.Context, sectionCode string) ([]models.Station, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+stationColumns+`
		FROM stations s
		JOIN section_stations ss ON ss.station_id = s.id
		WHERE ss.section_code = ?
		ORDER BY s.name COLLATE NOCASE
	`, sectionCode)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju vodomjera dionice %s: %w", sectionCode, err)
	}
	defer rows.Close()

	var stations []models.Station
	for rows.Next() {
		st, err := scanStation(rows)
		if err != nil {
			return nil, err
		}
		stations = append(stations, st)
	}
	return stations, rows.Err()
}

// StationScopes vraća sektore i branjena područja kojima je postaja mjerodavna.
func (r *StationRepository) StationScopes(ctx context.Context) (map[string][]string, map[string][]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT ss.station_id, s.sector_id, s.area_id
		FROM section_stations ss JOIN sections s ON s.code = ss.section_code`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	sectors, areas := map[string][]string{}, map[string][]int{}
	for rows.Next() {
		var stationID, sectorID string
		var areaID int
		if err := rows.Scan(&stationID, &sectorID, &areaID); err != nil {
			return nil, nil, err
		}
		sectors[stationID] = append(sectors[stationID], sectorID)
		areas[stationID] = append(areas[stationID], areaID)
	}
	return sectors, areas, rows.Err()
}

// CreateStation upisuje novu vodomjernu postaju u registar
func (r *StationRepository) CreateStation(ctx context.Context, st *models.Station) error {
	if st.ID == uuid.Nil {
		newID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		st.ID = newID
	}

	now := time.Now().UTC()
	st.CreatedAt, st.UpdatedAt = now, now

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO stations (
			id, code, name, watercourse, watercourse_code, watercourse_source, water_area, stationing,
			zero_datum, zero_datum_system, zero_datum_new, zero_datum_new_system,
			zero_datum_source, zero_datum_method, zero_datum_survey_date, zero_datum_document_date,
			zero_datum_history,
			prep_cm, prep_raw, regular_cm, regular_raw,
			emergency_cm, emergency_raw, state_cm, state_raw,
			record_cm, record_raw,
			notes, source_name, needs_review, review_note,
			latitude, longitude, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		st.ID.String(), st.Code, st.Name, st.Watercourse, st.WatercourseCode, st.WatercourseSource, st.WaterArea, st.Stationing,
		st.ZeroDatum, defaultSystem(st.ZeroDatumSystem, models.ZeroDatumSystemOld),
		st.ZeroDatumNew, defaultSystem(st.ZeroDatumNewSystem, models.ZeroDatumSystemNew),
		st.ZeroDatumSource, st.ZeroDatumMethod, st.ZeroDatumSurveyDate, st.ZeroDatumDocumentDate,
		zeroDatumHistoryJSON(st),
		st.Prep.Cm, st.Prep.Raw, st.Regular.Cm, st.Regular.Raw,
		st.Emergency.Cm, st.Emergency.Raw, st.State.Cm, st.State.Raw,
		st.Record.Cm, st.Record.Raw,
		st.Notes, st.SourceName, boolToInt(st.NeedsReview), st.ReviewNote,
		st.Latitude, st.Longitude, st.CreatedAt, st.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("greška pri unosu vodomjerne postaje %q: %w", st.Name, err)
	}

	if _, err := r.rec.Record(ctx, tx, EntityStations, st.ID.String(), st); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateStation mijenja podatke postaje
func (r *StationRepository) UpdateStation(ctx context.Context, st *models.Station) error {
	st.UpdatedAt = time.Now().UTC()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE stations SET
			code = ?, name = ?, watercourse = ?, watercourse_source = ?,
			watercourse_code = CASE WHEN watercourse = ? THEN watercourse_code ELSE '' END,
			water_area = ?, stationing = ?,
			zero_datum = ?, zero_datum_system = ?, zero_datum_new = ?, zero_datum_new_system = ?,
			zero_datum_source = ?, zero_datum_method = ?, zero_datum_survey_date = ?, zero_datum_document_date = ?,
			zero_datum_history = ?,
			prep_cm = ?, prep_raw = ?, regular_cm = ?, regular_raw = ?,
			emergency_cm = ?, emergency_raw = ?, state_cm = ?, state_raw = ?,
			record_cm = ?, record_raw = ?,
			notes = ?, needs_review = ?, review_note = ?,
			latitude = ?, longitude = ?, updated_at = ?
		WHERE id = ?
	`,
		st.Code, st.Name, st.Watercourse, st.WatercourseSource, st.Watercourse, st.WaterArea, st.Stationing,
		st.ZeroDatum, defaultSystem(st.ZeroDatumSystem, models.ZeroDatumSystemOld),
		st.ZeroDatumNew, defaultSystem(st.ZeroDatumNewSystem, models.ZeroDatumSystemNew),
		st.ZeroDatumSource, st.ZeroDatumMethod, st.ZeroDatumSurveyDate, st.ZeroDatumDocumentDate,
		zeroDatumHistoryJSON(st),
		st.Prep.Cm, st.Prep.Raw, st.Regular.Cm, st.Regular.Raw,
		st.Emergency.Cm, st.Emergency.Raw, st.State.Cm, st.State.Raw,
		st.Record.Cm, st.Record.Raw,
		st.Notes, boolToInt(st.NeedsReview), st.ReviewNote,
		st.Latitude, st.Longitude, st.UpdatedAt, st.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("greška pri izmjeni vodomjerne postaje %q: %w", st.Name, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("vodomjerna postaja nije pronađena")
	}

	// Verzija nosi stanje kakvo je stvarno na površini (uključivo sačuvanu vezu)
	saved, err := getStationTx(ctx, tx, st.ID.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityStations, st.ID.String(), saved); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteStation uklanja postaju s površine i sve njezine veze s dionicama.
// U knjizi verzija ostaje arhivirana i može se vratiti.
func (r *StationRepository) DeleteStation(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := getStationTx(ctx, tx, id.String())
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	rows, err := tx.QueryContext(ctx, `SELECT section_code FROM section_stations WHERE station_id = ?`, id.String())
	if err != nil {
		return err
	}
	var links []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return err
		}
		links = append(links, code)
	}
	rows.Close()

	if _, err := tx.ExecContext(ctx, `DELETE FROM section_stations WHERE station_id = ?`, id.String()); err != nil {
		return fmt.Errorf("greška pri uklanjanju veza postaje: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM stations WHERE id = ?`, id.String()); err != nil {
		return fmt.Errorf("greška pri brisanju vodomjerne postaje: %w", err)
	}

	for _, code := range links {
		if _, err := r.rec.Archive(ctx, tx, EntitySectionStations, sectionStationKey(code, id),
			map[string]string{"section_code": code, "station_id": id.String()}); err != nil {
			return err
		}
	}
	if _, err := r.rec.Archive(ctx, tx, EntityStations, id.String(), current); err != nil {
		return err
	}
	return tx.Commit()
}

// CountStations vraća ukupan broj postaja, broj označenih za pregled i broj veza s dionicama
func (r *StationRepository) CountStations(ctx context.Context) (total, needsReview, links int, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM stations),
			(SELECT COUNT(*) FROM stations WHERE needs_review = 1),
			(SELECT COUNT(*) FROM section_stations)
	`).Scan(&total, &needsReview, &links)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("greška pri brojanju vodomjernih postaja: %w", err)
	}
	return total, needsReview, links, nil
}

// ListWatercourses vraća vodotoke zastupljene u registru, radi filtra
func (r *StationRepository) ListWatercourses(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT watercourse FROM stations
		WHERE TRIM(watercourse) <> ''
		ORDER BY watercourse COLLATE NOCASE
	`)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju vodotoka: %w", err)
	}
	defer rows.Close()

	var list []string
	for rows.Next() {
		var wc string
		if err := rows.Scan(&wc); err != nil {
			return nil, err
		}
		list = append(list, wc)
	}
	return list, rows.Err()
}

// defaultSystem osigurava da oznaka visinskog sustava nikad ne ostane prazna,
// jer kota bez naznake sustava nije upotrebljiva
func defaultSystem(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// zeroDatumHistoryJSON pakira povijest kote nule u stupac; prazna povijest je
// "[]", da stupac nikad nije NULL i da čitanje ne mora nagađati.
func zeroDatumHistoryJSON(st *models.Station) string {
	if len(st.ZeroDatumHistory) == 0 {
		return "[]"
	}
	b, err := json.Marshal(st.ZeroDatumHistory)
	if err != nil {
		return "[]"
	}
	return string(b)
}
