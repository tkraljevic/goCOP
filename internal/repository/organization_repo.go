package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Organizacija obrane: sektori (VGO s centrom obrane) i branjena područja
// (mali slivovi s VGI). Program ih ne nosi u sebi; upisuje ih globalni
// administrator, a svaki upis ostavlja verziju u knjizi pa stiže na ostale
// čvorove. Na njih se vežu ovlasti, dionice i zaduženja, pa se brišu samo
// dok ih ništa ne koristi.

// EntitySectors, EntityAreas i EntityOrgTerms su nazivi entiteta u knjizi verzija
const (
	EntitySectors  = "sectors"
	EntityAreas    = "areas"
	EntityOrgTerms = "org_terms"
)

const termsColumns = `id, sector, sectors, area, areas, area_short, sector_office, area_office, center, subcenter, updated_at,
	org_name, level1_unit, level1_center, level1_center_short,
	sector_office_short, center_short, area_office_short, logo_mime, logo, login_info, role_labels,
	org_legal_form, org_registry_no, org_tax_id`

const termsUpsert = `INSERT INTO org_terms (` + termsColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET sector = excluded.sector, sectors = excluded.sectors, area = excluded.area, areas = excluded.areas,
		area_short = excluded.area_short, sector_office = excluded.sector_office, area_office = excluded.area_office,
		center = excluded.center, subcenter = excluded.subcenter, updated_at = excluded.updated_at,
		org_name = excluded.org_name, level1_unit = excluded.level1_unit,
		level1_center = excluded.level1_center, level1_center_short = excluded.level1_center_short,
		sector_office_short = excluded.sector_office_short, center_short = excluded.center_short, area_office_short = excluded.area_office_short,
		logo_mime = excluded.logo_mime, logo = excluded.logo, login_info = excluded.login_info, role_labels = excluded.role_labels,
		org_legal_form = excluded.org_legal_form, org_registry_no = excluded.org_registry_no, org_tax_id = excluded.org_tax_id`

func termsArgs(t models.OrgTerms) []any {
	return []any{models.TermsID, t.Sector, t.Sectors, t.Area, t.Areas, t.AreaShort, t.SectorOffice, t.AreaOffice, t.Center, t.Subcenter, t.UpdatedAt,
		t.OrgName, t.Level1Unit, t.Level1Center, t.Level1CenterShort,
		t.SectorOfficeShort, t.CenterShort, t.AreaOfficeShort, t.LogoMime, t.Logo, t.LoginInfo, labelsJSON(t.RoleLabels),
		t.OrgLegalForm, t.OrgRegistryNo, t.OrgTaxID}
}

// labelsJSON sprema nazive uloga u jedan stupac; prazno ostaje prazno
func labelsJSON(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// GetTerms čita nazive razina; bez zapisa vraća zadane
func (r *OrgRepository) GetTerms(ctx context.Context) (models.OrgTerms, error) {
	var t models.OrgTerms
	var labels string
	err := r.db.QueryRowContext(ctx, `SELECT `+termsColumns+` FROM org_terms WHERE id = ?`, models.TermsID).Scan(
		&t.ID, &t.Sector, &t.Sectors, &t.Area, &t.Areas, &t.AreaShort, &t.SectorOffice, &t.AreaOffice, &t.Center, &t.Subcenter, &t.UpdatedAt,
		&t.OrgName, &t.Level1Unit, &t.Level1Center, &t.Level1CenterShort,
		&t.SectorOfficeShort, &t.CenterShort, &t.AreaOfficeShort, &t.LogoMime, &t.Logo, &t.LoginInfo, &labels,
		&t.OrgLegalForm, &t.OrgRegistryNo, &t.OrgTaxID)
	if err == sql.ErrNoRows {
		return models.DefaultTerms(), nil
	}
	if err != nil {
		return t, err
	}
	if labels != "" {
		_ = json.Unmarshal([]byte(labels), &t.RoleLabels)
	}
	return t.Filled(), nil
}

// SaveTerms upisuje nazive, bilježi verziju i primjenjuje ih odmah
func (r *OrgRepository) SaveTerms(ctx context.Context, t models.OrgTerms) error {
	t = t.Filled()
	t.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, termsUpsert, termsArgs(t)...); err != nil {
		return fmt.Errorf("upis naziva razina: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityOrgTerms, models.TermsID, t); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	models.SetTerms(t)
	return nil
}

type OrgRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewOrgRepository(db *sql.DB, rec *ledger.Recorder) *OrgRepository {
	return &OrgRepository{db: db, rec: rec}
}

type rowScannerOrg interface {
	Scan(dest ...any) error
}

func scanSector(row rowScannerOrg) (models.Sector, error) {
	var s models.Sector
	var address, phone, email sql.NullString
	err := row.Scan(&s.ID, &s.Name, &s.VgoName, &s.CenterCop, &address, &phone, &email, &s.Level)
	s.Address, s.Phone, s.Email = address.String, phone.String, email.String
	if s.Level == 0 {
		s.Level = 2 // zapisi otprije razina: jedinica je sektor
	}
	return s, err
}

func scanArea(row rowScannerOrg) (models.Area, error) {
	var a models.Area
	var sub, contractor sql.NullString
	var direct int
	err := row.Scan(&a.ID, &a.SectorID, &a.Name, &a.VgiName, &sub, &contractor, &direct)
	a.Subcenter, a.ContractorName, a.DirectToSector = sub.String, contractor.String, direct != 0
	return a, err
}

const sectorSelect = `SELECT id, name, vgo_name, center_cop, address, phone, email, level FROM sectors`
const areaSelect = `SELECT id, sector_id, name, vgi_name, subcenter, contractor_name, direct_to_sector FROM areas`

// ListSectors vraća sektore; Direkcija prva, ostali po oznaci
func (r *OrgRepository) ListSectors(ctx context.Context) ([]models.Sector, error) {
	rows, err := r.db.QueryContext(ctx, sectorSelect+` ORDER BY CASE WHEN id = 'DIREKCIJA' THEN 0 ELSE 1 END, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Sector
	for rows.Next() {
		s, err := scanSector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSector vraća sektor ili nil kad ga nema
func (r *OrgRepository) GetSector(ctx context.Context, id string) (*models.Sector, error) {
	s, err := scanSector(r.db.QueryRowContext(ctx, sectorSelect+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveSector upisuje novi ili izmijenjeni sektor i bilježi verziju
func (r *OrgRepository) SaveSector(ctx context.Context, s *models.Sector) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email, level)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, vgo_name = excluded.vgo_name,
			center_cop = excluded.center_cop, address = excluded.address, phone = excluded.phone,
			email = excluded.email, level = excluded.level`,
		s.ID, s.Name, s.VgoName, s.CenterCop, s.Address, s.Phone, s.Email, s.Level); err != nil {
		return fmt.Errorf("upis sektora %s: %w", s.ID, err)
	}
	if _, err := r.rec.Record(ctx, tx, EntitySectors, s.ID, s); err != nil {
		return err
	}
	return tx.Commit()
}

// SectorInUse javlja koliko se branjenih područja, dionica i zaduženja veže na sektor
func (r *OrgRepository) SectorInUse(ctx context.Context, id string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM areas WHERE sector_id = ?)
		     + (SELECT COUNT(*) FROM sections WHERE sector_id = ?)
		     + (SELECT COUNT(*) FROM duties WHERE sector_id = ?)`, id, id, id).Scan(&n)
	return n, err
}

// DeleteSector uklanja sektor s površine; u knjizi ostaje arhiviran
func (r *OrgRepository) DeleteSector(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanSector(tx.QueryRowContext(ctx, sectorSelect+` WHERE id = ?`, id))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sectors WHERE id = ?`, id); err != nil {
		return fmt.Errorf("brisanje sektora %s: %w", id, err)
	}
	if _, err := r.rec.Archive(ctx, tx, EntitySectors, id, current); err != nil {
		return err
	}
	return tx.Commit()
}

// ListAreas vraća branjena područja, po želji jednog sektora, po broju
func (r *OrgRepository) ListAreas(ctx context.Context, sectorID string) ([]models.Area, error) {
	query, args := areaSelect, []any{}
	if sectorID != "" {
		query += ` WHERE sector_id = ?`
		args = append(args, sectorID)
	}
	rows, err := r.db.QueryContext(ctx, query+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Area
	for rows.Next() {
		a, err := scanArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetArea vraća branjeno područje ili nil kad ga nema
func (r *OrgRepository) GetArea(ctx context.Context, id int) (*models.Area, error) {
	a, err := scanArea(r.db.QueryRowContext(ctx, areaSelect+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// SaveArea upisuje novo ili izmijenjeno branjeno područje i bilježi verziju
func (r *OrgRepository) SaveArea(ctx context.Context, a *models.Area) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, contractor_name, direct_to_sector)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET sector_id = excluded.sector_id, name = excluded.name,
			vgi_name = excluded.vgi_name, subcenter = excluded.subcenter, contractor_name = excluded.contractor_name,
			direct_to_sector = excluded.direct_to_sector`,
		a.ID, a.SectorID, a.Name, a.VgiName, a.Subcenter, a.ContractorName, boolToInt(a.DirectToSector)); err != nil {
		return fmt.Errorf("upis branjenog područja %d: %w", a.ID, err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityAreas, strconv.Itoa(a.ID), a); err != nil {
		return err
	}
	return tx.Commit()
}

// AreaInUse javlja koliko se dionica i zaduženja veže na branjeno područje
func (r *OrgRepository) AreaInUse(ctx context.Context, id int) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM sections WHERE area_id = ?)
		     + (SELECT COUNT(*) FROM duties WHERE area_id = ?)`, id, id).Scan(&n)
	return n, err
}

// DeleteArea uklanja branjeno područje s površine; u knjizi ostaje arhivirano
func (r *OrgRepository) DeleteArea(ctx context.Context, id int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanArea(tx.QueryRowContext(ctx, areaSelect+` WHERE id = ?`, id))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM areas WHERE id = ?`, id); err != nil {
		return fmt.Errorf("brisanje branjenog područja %d: %w", id, err)
	}
	if _, err := r.rec.Archive(ctx, tx, EntityAreas, strconv.Itoa(id), current); err != nil {
		return err
	}
	return tx.Commit()
}
