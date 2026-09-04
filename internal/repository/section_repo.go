package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntitySections je naziv entiteta u knjizi verzija
const EntitySections = "sections"

type SectionRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewSectionRepository(db *sql.DB, rec *ledger.Recorder) *SectionRepository {
	return &SectionRepository{db: db, rec: rec}
}

const sectionSelect = `
	SELECT s.code, s.area_id, s.sector_id, s.description, s.protected_area,
	       s.embankments, s.structures, s.gauges, s.notes, s.created_at, s.updated_at,
	       COALESCE(a.name, '') as area_name, COALESCE(sec.name, '') as sector_name,
	       s.watercourse_code, COALESCE(w.name, '') as watercourse_name,
	       s.bank, s.rkm_from, s.rkm_to
	FROM sections s
	LEFT JOIN areas a ON s.area_id = a.id
	LEFT JOIN sectors sec ON s.sector_id = sec.id
	LEFT JOIN watercourses w ON w.code = s.watercourse_code
`

// scanSection čita jedan redak dionice s pripadajućim JSON blokovima
func scanSection(scanner interface{ Scan(...any) error }) (models.Section, error) {
	var sec models.Section
	var embJSON, strJSON, gagJSON sql.NullString
	var protArea, notes sql.NullString
	var rkmFrom, rkmTo sql.NullFloat64

	err := scanner.Scan(
		&sec.Code, &sec.AreaID, &sec.SectorID, &sec.Description, &protArea,
		&embJSON, &strJSON, &gagJSON, &notes, &sec.CreatedAt, &sec.UpdatedAt,
		&sec.AreaName, &sec.SectorName,
		&sec.WatercourseCode, &sec.WatercourseName, &sec.Bank, &rkmFrom, &rkmTo,
	)
	if err != nil {
		return sec, err
	}

	if protArea.Valid {
		sec.ProtectedArea = protArea.String
	}
	if notes.Valid {
		sec.Notes = notes.String
	}
	sec.RkmFrom = nullFloatPtr(rkmFrom)
	sec.RkmTo = nullFloatPtr(rkmTo)

	if embJSON.Valid && embJSON.String != "" {
		_ = json.Unmarshal([]byte(embJSON.String), &sec.Embankments)
	}
	if strJSON.Valid && strJSON.String != "" {
		_ = json.Unmarshal([]byte(strJSON.String), &sec.Structures)
	}
	if gagJSON.Valid && gagJSON.String != "" {
		_ = json.Unmarshal([]byte(gagJSON.String), &sec.Gauges)
	}
	return sec, nil
}

// getSectionTx čita dionicu unutar transakcije — za verziju nakon upisa
func getSectionTx(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, code string) (models.Section, error) {
	return scanSection(q.QueryRowContext(ctx, sectionSelect+` WHERE s.code = ?`, code))
}

// ListSections vraća dionice uz opcionalne filtre po sektoru, području i ključnoj riječi
func (r *SectionRepository) ListSections(sectorID string, areaID int, search string) ([]models.Section, error) {
	query := sectionSelect + ` WHERE 1=1`
	var args []any

	if sectorID != "" {
		query += " AND s.sector_id = ?"
		args = append(args, sectorID)
	}
	if areaID > 0 {
		query += " AND s.area_id = ?"
		args = append(args, areaID)
	}
	if s := strings.TrimSpace(search); s != "" {
		like := "%" + s + "%"
		query += ` AND (
			s.code LIKE ? OR
			s.description LIKE ? OR
			w.name LIKE ? OR
			s.protected_area LIKE ? OR
			s.structures LIKE ? OR
			s.gauges LIKE ? OR
			s.notes LIKE ?
		)`
		args = append(args, like, like, like, like, like, like, like)
	}

	query += " ORDER BY s.sector_id ASC, s.area_id ASC, s.code ASC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu dionica: %w", err)
	}
	defer rows.Close()

	var sections []models.Section
	for rows.Next() {
		sec, err := scanSection(rows)
		if err != nil {
			return nil, fmt.Errorf("greška pri skeniranju dionice: %w", err)
		}
		sections = append(sections, sec)
	}

	return sections, nil
}

// GetSectionByCode dohvaća pojedinačnu dionicu sa svim detaljima
func (r *SectionRepository) GetSectionByCode(code string) (*models.Section, error) {
	sec, err := scanSection(r.db.QueryRow(sectionSelect+` WHERE s.code = ?`, code))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu dionice %s: %w", code, err)
	}
	return &sec, nil
}

// CreateSection stvara novu dionicu u branjenom području
func (r *SectionRepository) CreateSection(s *models.Section) error {
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	s.CreatedAt = now
	s.UpdatedAt = now

	embJSON, _ := json.Marshal(s.Embankments)
	strJSON, _ := json.Marshal(s.Structures)
	gagJSON, _ := json.Marshal(s.Gauges)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sections (
			code, area_id, sector_id, description, watercourse_code, bank, rkm_from, rkm_to,
			protected_area, embankments, structures, gauges, notes, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.Code, s.AreaID, s.SectorID, s.Description, s.WatercourseCode, s.Bank, s.RkmFrom, s.RkmTo,
		s.ProtectedArea, string(embJSON), string(strJSON), string(gagJSON), s.Notes, now, now,
	)
	if err != nil {
		return fmt.Errorf("greška pri spremanju nove dionice %s: %w", s.Code, err)
	}

	saved, err := getSectionTx(ctx, tx, s.Code)
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntitySections, s.Code, saved); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateSection ažurira postojeću dionicu
func (r *SectionRepository) UpdateSection(s *models.Section) error {
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	s.UpdatedAt = now

	embJSON, _ := json.Marshal(s.Embankments)
	strJSON, _ := json.Marshal(s.Structures)
	gagJSON, _ := json.Marshal(s.Gauges)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE sections
		SET description = ?, bank = ?, rkm_from = ?, rkm_to = ?, protected_area = ?,
		    embankments = ?, structures = ?, gauges = ?, notes = ?, updated_at = ?
		WHERE code = ?
	`, s.Description, s.Bank, s.RkmFrom, s.RkmTo, s.ProtectedArea,
		string(embJSON), string(strJSON), string(gagJSON), s.Notes, now, s.Code,
	)
	if err != nil {
		return fmt.Errorf("greška pri ažuriranju dionice %s: %w", s.Code, err)
	}

	saved, err := getSectionTx(ctx, tx, s.Code)
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntitySections, s.Code, saved); err != nil {
		return err
	}
	return tx.Commit()
}

// GetSectionPersonnel pronalazi sve djelatnike vezane uz dionicu i njezino branjeno područje
func (r *SectionRepository) GetSectionPersonnel(code string, areaID int, sectorID string) ([]models.SectionOfficer, error) {
	query := `
		SELECT DISTINCT u.id, u.full_name, u.title, d.title as duty_title, d.role,
		       u.phone, u.mobile_phone, u.email, u.org_name
		FROM duties d
		JOIN users u ON d.user_id = u.id
		WHERE d.is_active = 1
		  AND (
		      d.section_codes LIKE ? OR
		      (d.area_id = ? AND d.role IN ('WATER_GUARD', 'MACHINIST', 'AREA_LEADER', 'AREA_DEPUTY', 'CONTRACT_OFFICER_A2', 'CONTRACT_OFFICER_A3', 'SERVICE_LEADER_FOREMAN')) OR
		      (d.sector_id = ? AND d.role IN ('SECTOR_LEADER', 'SECTOR_DEPUTY', 'COP_LEADER', 'COP_DEPUTY'))
		  )
		ORDER BY 
			CASE 
				WHEN d.section_codes LIKE ? THEN 1
				WHEN d.role IN ('AREA_LEADER', 'AREA_DEPUTY') THEN 2
				WHEN d.role IN ('WATER_GUARD', 'MACHINIST') THEN 3
				WHEN d.role IN ('SECTOR_LEADER', 'SECTOR_DEPUTY', 'COP_LEADER', 'COP_DEPUTY') THEN 4
				ELSE 5
			END,
			u.full_name ASC
	`
	codeLike := "%" + code + "%"
	rows, err := r.db.Query(query, codeLike, areaID, sectorID, codeLike)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu osoblja za dionicu: %w", err)
	}
	defer rows.Close()

	var officers []models.SectionOfficer
	for rows.Next() {
		var o models.SectionOfficer
		var roleStr string
		var title, phone, mob, email, org sql.NullString

		err := rows.Scan(
			&o.UserID, &o.FullName, &title, &o.DutyTitle, &roleStr,
			&phone, &mob, &email, &org,
		)
		if err != nil {
			return nil, err
		}

		if title.Valid {
			o.Title = title.String
		}
		if phone.Valid {
			o.Phone = phone.String
		}
		if mob.Valid {
			o.MobilePhone = mob.String
		}
		if email.Valid {
			o.Email = email.String
		}
		if org.Valid {
			o.OrgName = org.String
		}

		o.Role = roleStr
		o.RoleLabel = models.Role(roleStr).Label()

		officers = append(officers, o)
	}

	return officers, nil
}

// UpdateProtectedArea ažurira tekst ugroženog područja dionice
func (r *SectionRepository) UpdateProtectedArea(code string, text string) error {
	ctx := context.Background()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE sections SET protected_area = ?, updated_at = ? WHERE code = ?`,
		text, time.Now().UTC().Format(time.RFC3339), code); err != nil {
		return fmt.Errorf("greška pri ažuriranju ugroženog područja za dionicu %s: %w", code, err)
	}

	saved, err := getSectionTx(ctx, tx, code)
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntitySections, code, saved); err != nil {
		return err
	}
	return tx.Commit()
}
