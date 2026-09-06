package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntityWatercourses je naziv entiteta u knjizi verzija
const EntityWatercourses = "watercourses"

type WatercourseRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewWatercourseRepository(db *sql.DB, rec *ledger.Recorder) *WatercourseRepository {
	return &WatercourseRepository{db: db, rec: rec}
}

const watercourseColumns = `
	w.code, w.official_name, w.name, w.kind, w.category, w.subcategory, w.wiki_slug, w.origin,
	w.length_km, w.basin_km2, w.avg_flow_m3s, w.source, w.mouth, w.flows_into, w.notes,
	(SELECT COUNT(*) FROM sections s WHERE s.watercourse_code = w.code),
	(SELECT COUNT(*) FROM stations st WHERE st.watercourse_code = w.code)
`

func scanWatercourse(scanner interface{ Scan(...any) error }) (models.Watercourse, error) {
	var (
		w      models.Watercourse
		length sql.NullFloat64
		basin  sql.NullFloat64
		flow   sql.NullFloat64
	)

	err := scanner.Scan(
		&w.Code, &w.OfficialName, &w.Name, &w.Kind, &w.Category, &w.Subcategory, &w.WikiSlug, &w.Origin,
		&length, &basin, &flow, &w.Source, &w.Mouth, &w.FlowsInto, &w.Notes,
		&w.SectionCount, &w.StationCount,
	)
	if err != nil {
		return w, err
	}

	w.LengthKm = nullFloatPtr(length)
	w.BasinKm2 = nullFloatPtr(basin)
	w.AvgFlowM3S = nullFloatPtr(flow)

	return w, nil
}

// ListWatercourses vraća registar vodnih tijela uz filtre.
// onlyUsed ograničava popis na vode koje imaju dionicu ili postaju.
func (r *WatercourseRepository) ListWatercourses(ctx context.Context, search, category string, onlyUsed bool) ([]models.Watercourse, error) {
	query := `SELECT ` + watercourseColumns + ` FROM watercourses w WHERE 1=1`
	var args []any

	if term := strings.TrimSpace(search); term != "" {
		query += ` AND (w.name LIKE ? OR w.official_name LIKE ?)`
		like := "%" + term + "%"
		args = append(args, like, like)
	}
	if cat := strings.TrimSpace(category); cat != "" {
		if cat == "NIJE_I_REDA" {
			query += ` AND w.category = ''`
		} else {
			query += ` AND w.category = ?`
			args = append(args, cat)
		}
	}
	if onlyUsed {
		query += ` AND (
			EXISTS (SELECT 1 FROM sections s WHERE s.watercourse_code = w.code)
			OR EXISTS (SELECT 1 FROM stations st WHERE st.watercourse_code = w.code)
		)`
	}
	query += ` ORDER BY w.name COLLATE NOCASE`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju vodnih tijela: %w", err)
	}
	defer rows.Close()

	var waters []models.Watercourse
	for rows.Next() {
		w, err := scanWatercourse(rows)
		if err != nil {
			return nil, err
		}
		waters = append(waters, w)
	}
	return waters, rows.Err()
}

// GetWatercourse dohvaća jedno vodno tijelo
func (r *WatercourseRepository) GetWatercourse(ctx context.Context, code string) (*models.Watercourse, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+watercourseColumns+` FROM watercourses w WHERE w.code = ?`, code)

	w, err := scanWatercourse(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju vodnog tijela: %w", err)
	}
	return &w, nil
}

// ListCategories vraća kategorije zastupljene u registru
func (r *WatercourseRepository) ListCategories(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT category FROM watercourses WHERE category <> '' ORDER BY category
	`)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju kategorija voda: %w", err)
	}
	defer rows.Close()

	var list []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// CountWatercourses vraća ukupan broj voda, broj onih u upotrebi te broj
// dionica i postaja koje još nisu vezane na registar
func (r *WatercourseRepository) CountWatercourses(ctx context.Context) (total, used, unlinkedSections, unlinkedStations int, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM watercourses),
			(SELECT COUNT(*) FROM watercourses w WHERE
				EXISTS (SELECT 1 FROM sections s WHERE s.watercourse_code = w.code)
				OR EXISTS (SELECT 1 FROM stations st WHERE st.watercourse_code = w.code)),
			(SELECT COUNT(*) FROM sections WHERE watercourse_code = ''),
			(SELECT COUNT(*) FROM stations WHERE watercourse_code = '')
	`).Scan(&total, &used, &unlinkedSections, &unlinkedStations)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("greška pri brojanju vodnih tijela: %w", err)
	}
	return total, used, unlinkedSections, unlinkedStations, nil
}

// CreateWatercourse upisuje novo vodno tijelo u registar
func (r *WatercourseRepository) CreateWatercourse(ctx context.Context, w *models.Watercourse) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO watercourses (
			code, official_name, name, kind, category, subcategory, wiki_slug, origin,
			length_km, basin_km2, avg_flow_m3s, source, mouth, flows_into, notes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		w.Code, w.OfficialName, w.Name, w.Kind, w.Category, w.Subcategory, w.WikiSlug, w.Origin,
		w.LengthKm, w.BasinKm2, w.AvgFlowM3S, w.Source, w.Mouth, w.FlowsInto, w.Notes,
	)
	if err != nil {
		return fmt.Errorf("greška pri unosu vodnog tijela %q: %w", w.OfficialName, err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityWatercourses, w.Code, w); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateWatercourse mijenja podatke vodnog tijela. Šifra se ne mijenja jer na
// nju pokazuju dionice i postaje.
func (r *WatercourseRepository) UpdateWatercourse(ctx context.Context, w *models.Watercourse) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE watercourses SET
			official_name = ?, name = ?, kind = ?, category = ?, subcategory = ?, wiki_slug = ?,
			length_km = ?, basin_km2 = ?, avg_flow_m3s = ?, source = ?, mouth = ?, flows_into = ?, notes = ?
		WHERE code = ?
	`,
		w.OfficialName, w.Name, w.Kind, w.Category, w.Subcategory, w.WikiSlug,
		w.LengthKm, w.BasinKm2, w.AvgFlowM3S, w.Source, w.Mouth, w.FlowsInto, w.Notes, w.Code,
	)
	if err != nil {
		return fmt.Errorf("greška pri izmjeni vodnog tijela %q: %w", w.OfficialName, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("vodno tijelo nije pronađeno")
	}
	if _, err := r.rec.Record(ctx, tx, EntityWatercourses, w.Code, w); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteWatercourse uklanja vodno tijelo s površine; u knjizi ostaje arhivirano
func (r *WatercourseRepository) DeleteWatercourse(ctx context.Context, code string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := scanWatercourse(tx.QueryRowContext(ctx,
		`SELECT `+watercourseColumns+` FROM watercourses w WHERE w.code = ?`, code))
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM watercourses WHERE code = ?`, code); err != nil {
		return fmt.Errorf("greška pri brisanju vodnog tijela: %w", err)
	}
	if _, err := r.rec.Archive(ctx, tx, EntityWatercourses, code, current); err != nil {
		return err
	}
	return tx.Commit()
}

// CountUsage vraća broj dionica i postaja vezanih na vodno tijelo
func (r *WatercourseRepository) CountUsage(ctx context.Context, code string) (sections, stations int, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM sections WHERE watercourse_code = ?),
			(SELECT COUNT(*) FROM stations WHERE watercourse_code = ?)
	`, code, code).Scan(&sections, &stations)
	if err != nil {
		return 0, 0, fmt.Errorf("greška pri provjeri upotrebe vodnog tijela: %w", err)
	}
	return sections, stations, nil
}

// SetStationWatercourse pridružuje vodno tijelo postaji i upisuje naziv vode
// te naznaku da je vodotok potvrđen ručno
func (r *WatercourseRepository) SetStationWatercourse(ctx context.Context, stationID, watercourseCode, waterName, sourceLabel string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE stations SET watercourse_code = ?, watercourse = ?, watercourse_source = ?, updated_at = ?
		WHERE id = ?
	`, watercourseCode, waterName, sourceLabel, time.Now().UTC(), stationID)
	if err != nil {
		return fmt.Errorf("greška pri pridruživanju vodotoka postaji: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("vodomjerna postaja nije pronađena")
	}

	saved, err := getStationTx(ctx, tx, stationID)
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityStations, stationID, saved); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSectionsForWatercourse vraća šifre dionica na vodnom tijelu
func (r *WatercourseRepository) ListSectionsForWatercourse(ctx context.Context, code string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT code FROM sections WHERE watercourse_code = ? ORDER BY code`, code)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju dionica vodnog tijela: %w", err)
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}
