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

// EntityStructures i EntitySectionStructures su nazivi entiteta u knjizi verzija
const (
	EntityStructures        = "structures"
	EntitySectionStructures = "section_structures"
)

// StructureRepository čuva registar objekata; svaki upis ostavlja verziju
type StructureRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewStructureRepository(database *sql.DB, rec *ledger.Recorder) *StructureRepository {
	return &StructureRepository{db: database, rec: rec}
}

const structureColumns = `id, code, name, kind, sector_id, area_id, watercourse_code, station_id,
	zero_datum, zero_datum_system, capacity_text, start_cm, start_text, stop_cm, stop_text,
	notes, origin, latitude, longitude, created_at, updated_at`

func scanStructure(row rowScanner) (models.Structure, error) {
	var s models.Structure
	var idStr string
	var wc, st, zds, cap, stt, spt, notes, origin sql.NullString
	var zd, lat, lon sql.NullFloat64
	var startCm, stopCm sql.NullInt64
	err := row.Scan(&idStr, &s.Code, &s.Name, &s.Kind, &s.SectorID, &s.AreaID, &wc, &st,
		&zd, &zds, &cap, &startCm, &stt, &stopCm, &spt, &notes, &origin, &lat, &lon, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	s.ID, _ = uuid.Parse(idStr)
	s.WatercourseCode, s.StationID, s.ZeroDatumSystem = wc.String, st.String, zds.String
	s.CapacityText, s.StartText, s.StopText, s.Notes, s.Origin = cap.String, stt.String, spt.String, notes.String, origin.String
	if zd.Valid {
		v := zd.Float64
		s.ZeroDatum = &v
	}
	if startCm.Valid {
		v := int(startCm.Int64)
		s.StartCm = &v
	}
	if stopCm.Valid {
		v := int(stopCm.Int64)
		s.StopCm = &v
	}
	if lat.Valid && lon.Valid {
		a, b := lat.Float64, lon.Float64
		s.Latitude, s.Longitude = &a, &b
	}
	return s, nil
}

func getStructureTx(ctx context.Context, q rowQuerier, id string) (models.Structure, error) {
	return scanStructure(q.QueryRowContext(ctx, "SELECT "+structureColumns+" FROM structures WHERE id = ?", id))
}

// ListStructures vraća objekte, po želji sužene na sektor, područje, vrstu ili tekst
func (r *StructureRepository) ListStructures(ctx context.Context, sectorID string, areaID int, kind, search string) ([]models.Structure, error) {
	query := "SELECT " + structureColumns + " FROM structures WHERE 1=1"
	var args []any
	if sectorID != "" {
		query += " AND sector_id = ?"
		args = append(args, sectorID)
	}
	if areaID > 0 {
		query += " AND area_id = ?"
		args = append(args, areaID)
	}
	if kind != "" {
		query += " AND kind = ?"
		args = append(args, kind)
	}
	if term := strings.TrimSpace(search); term != "" {
		query += " AND (name LIKE ? OR code LIKE ? OR notes LIKE ?)"
		like := "%" + term + "%"
		args = append(args, like, like, like)
	}
	query += " ORDER BY area_id, kind, name COLLATE NOCASE"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju objekata: %w", err)
	}
	defer rows.Close()

	var out []models.Structure
	for rows.Next() {
		s, err := scanStructure(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		r.decorate(ctx, &out[i])
	}
	return out, nil
}

// decorate puni izvedena polja: dionice, naziv vodomjera, naziv područja
func (r *StructureRepository) decorate(ctx context.Context, s *models.Structure) {
	rows, err := r.db.QueryContext(ctx, `SELECT section_code FROM section_structures WHERE structure_id = ? ORDER BY section_code`, s.ID.String())
	if err == nil {
		for rows.Next() {
			var code string
			if rows.Scan(&code) == nil {
				s.SectionCodes = append(s.SectionCodes, code)
			}
		}
		rows.Close()
	}
	if s.StationID != "" {
		_ = r.db.QueryRowContext(ctx, `SELECT name FROM stations WHERE id = ?`, s.StationID).Scan(&s.StationName)
	}
	_ = r.db.QueryRowContext(ctx, `SELECT name FROM areas WHERE id = ?`, s.AreaID).Scan(&s.AreaName)
}

// GetStructure vraća jedan objekt ili nil
func (r *StructureRepository) GetStructure(ctx context.Context, id uuid.UUID) (*models.Structure, error) {
	s, err := getStructureTx(ctx, r.db, id.String())
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.decorate(ctx, &s)
	return &s, nil
}

// GetByCode vraća objekt po šifri ili nil
func (r *StructureRepository) GetByCode(ctx context.Context, code string) (*models.Structure, error) {
	s, err := scanStructure(r.db.QueryRowContext(ctx, "SELECT "+structureColumns+" FROM structures WHERE code = ?", code))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.decorate(ctx, &s)
	return &s, nil
}

// ListForSection vraća objekte vezane na dionicu
func (r *StructureRepository) ListForSection(ctx context.Context, sectionCode string) ([]models.Structure, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+structureColumns+` FROM structures
		WHERE id IN (SELECT structure_id FROM section_structures WHERE section_code = ?) ORDER BY kind, name`, sectionCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Structure
	for rows.Next() {
		s, err := scanStructure(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateStructure upisuje objekt i njegovu verziju
func (r *StructureRepository) CreateStructure(ctx context.Context, s *models.Structure) error {
	if s.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		s.ID = id
	}
	now := time.Now().UTC()
	s.CreatedAt, s.UpdatedAt = now, now

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO structures (`+structureColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID.String(), s.Code, s.Name, s.Kind, s.SectorID, s.AreaID, s.WatercourseCode, s.StationID,
		s.ZeroDatum, s.ZeroDatumSystem, s.CapacityText, s.StartCm, s.StartText, s.StopCm, s.StopText,
		s.Notes, s.Origin, s.Latitude, s.Longitude, s.CreatedAt, s.UpdatedAt); err != nil {
		return fmt.Errorf("greška pri upisu objekta: %w", err)
	}
	saved, err := getStructureTx(ctx, tx, s.ID.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityStructures, s.ID.String(), saved); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateStructure mijenja podatke objekta; veze na dionice se ne diraju
func (r *StructureRepository) UpdateStructure(ctx context.Context, s *models.Structure) error {
	s.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE structures SET code = ?, name = ?, kind = ?, sector_id = ?, area_id = ?,
		watercourse_code = ?, station_id = ?, zero_datum = ?, zero_datum_system = ?, capacity_text = ?,
		start_cm = ?, start_text = ?, stop_cm = ?, stop_text = ?, notes = ?, origin = ?, latitude = ?, longitude = ?,
		updated_at = ? WHERE id = ?`,
		s.Code, s.Name, s.Kind, s.SectorID, s.AreaID, s.WatercourseCode, s.StationID, s.ZeroDatum, s.ZeroDatumSystem,
		s.CapacityText, s.StartCm, s.StartText, s.StopCm, s.StopText, s.Notes, s.Origin, s.Latitude, s.Longitude,
		s.UpdatedAt, s.ID.String()); err != nil {
		return fmt.Errorf("greška pri izmjeni objekta: %w", err)
	}
	saved, err := getStructureTx(ctx, tx, s.ID.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityStructures, s.ID.String(), saved); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteStructure skida objekt s površine; u knjizi ostaje arhiviran
func (r *StructureRepository) DeleteStructure(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := getStructureTx(ctx, tx, id.String())
	if err != nil {
		return err
	}
	links, err := r.linksTx(ctx, tx, id.String())
	if err != nil {
		return err
	}
	for _, code := range links {
		if err := r.unlinkTx(ctx, tx, code, id.String()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM structures WHERE id = ?`, id.String()); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityStructures, id.String(), current); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *StructureRepository) linksTx(ctx context.Context, tx *sql.Tx, structureID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT section_code FROM section_structures WHERE structure_id = ?`, structureID)
	if err != nil {
		return nil, err
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

type sectionStructureLink struct {
	ID          string    `json:"id"`
	SectionCode string    `json:"section_code"`
	StructureID string    `json:"structure_id"`
	CreatedAt   time.Time `json:"created_at"`
}

func sectionStructureKey(sectionCode, structureID string) string {
	return sectionCode + "|" + structureID
}

// LinkSection veže objekt na dionicu (bez učinka ako veza već postoji)
func (r *StructureRepository) LinkSection(ctx context.Context, sectionCode string, structureID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.linkTx(ctx, tx, sectionCode, structureID.String()); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *StructureRepository) linkTx(ctx context.Context, tx *sql.Tx, sectionCode, structureID string) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM section_structures WHERE section_code = ? AND structure_id = ?`,
		sectionCode, structureID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// identitet veze slijedi iz para, pa je isti na svakom čvoru
	link := sectionStructureLink{ID: stableLinkID("section_structure", sectionStructureKey(sectionCode, structureID)),
		SectionCode: sectionCode, StructureID: structureID, CreatedAt: time.Now().UTC()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO section_structures (id, section_code, structure_id, created_at) VALUES (?, ?, ?, ?)`,
		link.ID, link.SectionCode, link.StructureID, link.CreatedAt); err != nil {
		return fmt.Errorf("greška pri vezanju objekta na dionicu: %w", err)
	}
	_, err := r.rec.Record(ctx, tx, EntitySectionStructures, link.ID, link)
	return err
}

// UnlinkSection skida objekt s dionice; objekt ostaje u registru
func (r *StructureRepository) UnlinkSection(ctx context.Context, sectionCode string, structureID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.unlinkTx(ctx, tx, sectionCode, structureID.String()); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *StructureRepository) unlinkTx(ctx context.Context, tx *sql.Tx, sectionCode, structureID string) error {
	var link sectionStructureLink
	err := tx.QueryRowContext(ctx, `SELECT id, section_code, structure_id, created_at FROM section_structures WHERE section_code = ? AND structure_id = ?`,
		sectionCode, structureID).Scan(&link.ID, &link.SectionCode, &link.StructureID, &link.CreatedAt)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM section_structures WHERE id = ?`, link.ID); err != nil {
		return err
	}
	_, err = r.rec.Archive(ctx, tx, EntitySectionStructures, link.ID, link)
	return err
}

// stableLinkID izvodi isti identifikator veze na svakom čvoru iz njezina para
func stableLinkID(kind, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://github.com/tkraljevic/goCOP/"+kind+"/"+key)).String()
}
