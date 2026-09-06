package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Nazivi entiteta teritorijalnih jedinica u knjizi verzija
const (
	EntityCounties           = "counties"
	EntityMunicipalities     = "municipalities"
	EntitySettlements        = "settlements"
	EntitySectionTerritories = "section_territories"
)

type TerritoryRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewTerritoryRepository(db *sql.DB, rec *ledger.Recorder) *TerritoryRepository {
	return &TerritoryRepository{db: db, rec: rec}
}

func getCountyTx(ctx context.Context, q rowQuerier, id int) (models.County, error) {
	var c models.County
	var code, seat, prefect, email, phone sql.NullString
	var area, pop sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id, code, name, seat, prefect, area_sqkm, population, email, phone FROM counties WHERE id = ?`, id).
		Scan(&c.ID, &code, &c.Name, &seat, &prefect, &area, &pop, &email, &phone)
	if err != nil {
		return c, err
	}
	c.Code, c.Seat, c.Prefect, c.Email, c.Phone = code.String, seat.String, prefect.String, email.String, phone.String
	c.AreaSqKm, c.Population = int(area.Int64), int(pop.Int64)
	return c, nil
}

func getMunicipalityTx(ctx context.Context, q rowQuerier, id int) (models.Municipality, error) {
	var m models.Municipality
	var headTitle, headName, postal sql.NullString
	var area sql.NullFloat64
	var pop sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id, county_id, name, type, head_title, head_name, postal_code, area_sqkm, population, email, phone, website FROM municipalities WHERE id = ?`, id).
		Scan(&m.ID, &m.CountyID, &m.Name, &m.Type, &headTitle, &headName, &postal, &area, &pop, &m.Email, &m.Phone, &m.Website)
	if err != nil {
		return m, err
	}
	m.HeadTitle, m.HeadName, m.PostalCode = headTitle.String, headName.String, postal.String
	m.AreaSqKm, m.Population = area.Float64, int(pop.Int64)
	return m, nil
}

func getSettlementTx(ctx context.Context, q rowQuerier, id int) (models.Settlement, error) {
	var st models.Settlement
	var postal sql.NullString
	var pop sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id, municipality_id, county_id, name, postal_code, population FROM settlements WHERE id = ?`, id).
		Scan(&st.ID, &st.MunicipalityID, &st.CountyID, &st.Name, &postal, &pop)
	if err != nil {
		return st, err
	}
	st.PostalCode, st.Population = postal.String, int(pop.Int64)
	return st, nil
}

// archiveSectionTerritories briše kazalo veza dionica s jedinicom koja
// nestaje; kazalo je izvedeno iz poddionica, pa se ne arhivira zasebno
func (r *TerritoryRepository) archiveSectionTerritories(ctx context.Context, tx *sql.Tx, where string, arg any) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM section_territories WHERE "+where, arg)
	return err
}

// ListCounties vraća sve županije sortirane po redoslijedu/nazivu
func (r *TerritoryRepository) ListCounties(ctx context.Context) ([]models.County, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, name, seat, prefect, area_sqkm, population, email, phone
		FROM counties
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu županija: %w", err)
	}
	defer rows.Close()

	var list []models.County
	for rows.Next() {
		var c models.County
		var code, prefect, email, phone sql.NullString
		var area, pop sql.NullInt64

		if err := rows.Scan(&c.ID, &code, &c.Name, &c.Seat, &prefect, &area, &pop, &email, &phone); err != nil {
			return nil, err
		}
		c.Code = code.String
		c.Prefect = prefect.String
		c.AreaSqKm = int(area.Int64)
		c.Population = int(pop.Int64)
		c.Email = email.String
		c.Phone = phone.String

		list = append(list, c)
	}
	return list, nil
}

// GetCountyByID vraća pojedinu županiju
func (r *TerritoryRepository) GetCountyByID(ctx context.Context, id int) (*models.County, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, code, name, seat, prefect, area_sqkm, population, email, phone
		FROM counties
		WHERE id = ?
	`, id)

	var c models.County
	var code, prefect, email, phone sql.NullString
	var area, pop sql.NullInt64

	if err := row.Scan(&c.ID, &code, &c.Name, &c.Seat, &prefect, &area, &pop, &email, &phone); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	c.Code = code.String
	c.Prefect = prefect.String
	c.AreaSqKm = int(area.Int64)
	c.Population = int(pop.Int64)
	c.Email = email.String
	c.Phone = phone.String

	return &c, nil
}

// ListMunicipalities dohvaća gradove i općine (uz opcionalno filtriranje po županiji, tipu i pretrazi)
func (r *TerritoryRepository) ListMunicipalities(ctx context.Context, countyID int, mType string, query string) ([]models.Municipality, error) {
	queryBuilder := `
		SELECT m.id, m.county_id, c.name, m.name, m.type, m.head_title, m.head_name, m.postal_code, m.area_sqkm, m.population,
		       m.email, m.phone, m.website
		FROM municipalities m
		JOIN counties c ON m.county_id = c.id
		WHERE 1=1
	`
	var args []interface{}

	if countyID > 0 {
		queryBuilder += " AND m.county_id = ?"
		args = append(args, countyID)
	}
	if mType != "" {
		queryBuilder += " AND m.type = ?"
		args = append(args, strings.ToUpper(mType))
	}
	if strings.TrimSpace(query) != "" {
		queryBuilder += " AND (m.name LIKE ? OR c.name LIKE ? OR m.postal_code LIKE ? OR m.head_name LIKE ?)"
		searchTerm := "%" + strings.TrimSpace(query) + "%"
		args = append(args, searchTerm, searchTerm, searchTerm, searchTerm)
	}

	queryBuilder += " ORDER BY m.name ASC"

	rows, err := r.db.QueryContext(ctx, queryBuilder, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu općina/gradova: %w", err)
	}
	defer rows.Close()

	var list []models.Municipality
	for rows.Next() {
		var m models.Municipality
		var headTitle, headName, postalCode sql.NullString
		var area sql.NullFloat64
		var pop sql.NullInt64

		if err := rows.Scan(&m.ID, &m.CountyID, &m.CountyName, &m.Name, &m.Type, &headTitle, &headName, &postalCode, &area, &pop,
			&m.Email, &m.Phone, &m.Website); err != nil {
			return nil, err
		}
		m.HeadTitle = headTitle.String
		m.HeadName = headName.String
		m.PostalCode = postalCode.String
		m.AreaSqKm = area.Float64
		m.Population = int(pop.Int64)

		list = append(list, m)
	}
	return list, nil
}

// ListSettlements dohvaća naselja za određeni grad/općinu ili županiju
func (r *TerritoryRepository) ListSettlements(ctx context.Context, municipalityID int, countyID int, query string) ([]models.Settlement, error) {
	queryBuilder := `
		SELECT id, municipality_id, county_id, name, postal_code, population
		FROM settlements
		WHERE 1=1
	`
	var args []interface{}

	if municipalityID > 0 {
		queryBuilder += " AND municipality_id = ?"
		args = append(args, municipalityID)
	}
	if countyID > 0 {
		queryBuilder += " AND county_id = ?"
		args = append(args, countyID)
	}
	if strings.TrimSpace(query) != "" {
		queryBuilder += " AND (name LIKE ? OR postal_code LIKE ?)"
		searchTerm := "%" + strings.TrimSpace(query) + "%"
		args = append(args, searchTerm, searchTerm)
	}

	queryBuilder += " ORDER BY name ASC"

	rows, err := r.db.QueryContext(ctx, queryBuilder, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu naselja: %w", err)
	}
	defer rows.Close()

	var list []models.Settlement
	for rows.Next() {
		var s models.Settlement
		var postalCode sql.NullString
		var pop sql.NullInt64

		if err := rows.Scan(&s.ID, &s.MunicipalityID, &s.CountyID, &s.Name, &postalCode, &pop); err != nil {
			return nil, err
		}
		s.PostalCode = postalCode.String
		s.Population = int(pop.Int64)

		list = append(list, s)
	}
	return list, nil
}

// GetSectionTerritories dohvaća sva povezana naselja/općine za dionicu
func (r *TerritoryRepository) GetSectionTerritories(ctx context.Context, sectionCode string) ([]models.SectionTerritory, error) {
	query := `
		SELECT st.id, st.section_code, st.county_id, st.municipality_id, st.settlement_id,
		       c.name AS county_name,
		       m.name AS municipality_name, m.type AS municipality_type,
		       COALESCE(s.name, '') AS settlement_name,
		       st.created_at
		FROM section_territories st
		JOIN counties c ON st.county_id = c.id
		JOIN municipalities m ON st.municipality_id = m.id
		LEFT JOIN settlements s ON st.settlement_id = s.id
		WHERE st.section_code = ?
		ORDER BY c.name ASC, m.name ASC, s.name ASC
	`
	rows, err := r.db.QueryContext(ctx, query, sectionCode)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu teritorija za dionicu %s: %w", sectionCode, err)
	}
	defer rows.Close()

	var list []models.SectionTerritory
	for rows.Next() {
		var item models.SectionTerritory
		var settID sql.NullInt64

		if err := rows.Scan(
			&item.ID, &item.SectionCode, &item.CountyID, &item.MunicipalityID, &settID,
			&item.CountyName, &item.MunicipalityName, &item.MunicipalityType, &item.SettlementName,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if settID.Valid {
			val := int(settID.Int64)
			item.SettlementID = &val
		}
		list = append(list, item)
	}
	return list, nil
}

// GetTerritoryCounts vraća ukupan broj županija, gradova/općina i naselja
func (r *TerritoryRepository) GetTerritoryCounts(ctx context.Context) (int, int, int, error) {
	var counties, munis, settlements int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM counties").Scan(&counties); err != nil {
		return 0, 0, 0, err
	}
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM municipalities").Scan(&munis); err != nil {
		return 0, 0, 0, err
	}
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM settlements").Scan(&settlements); err != nil {
		return 0, 0, 0, err
	}
	return counties, munis, settlements, nil
}

// CreateCounty dodaje novu županiju
func (r *TerritoryRepository) CreateCounty(ctx context.Context, c *models.County) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO counties (code, name, seat, prefect, area_sqkm, population, email, phone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, c.Code, c.Name, c.Seat, c.Prefect, c.AreaSqKm, c.Population, c.Email, c.Phone)
	if err != nil {
		return fmt.Errorf("greška pri dodavanju županije: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		c.ID = int(id)
	}
	if _, err := r.rec.Record(ctx, tx, EntityCounties, strconv.Itoa(c.ID), c); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateCounty ažurira postojeću županiju
func (r *TerritoryRepository) UpdateCounty(ctx context.Context, c *models.County) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE counties
		SET code = ?, name = ?, seat = ?, prefect = ?, area_sqkm = ?, population = ?, email = ?, phone = ?
		WHERE id = ?
	`, c.Code, c.Name, c.Seat, c.Prefect, c.AreaSqKm, c.Population, c.Email, c.Phone, c.ID); err != nil {
		return fmt.Errorf("greška pri ažuriranju županije: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityCounties, strconv.Itoa(c.ID), c); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteCounty uklanja županiju s površine; u knjizi ostaje arhivirana
func (r *TerritoryRepository) DeleteCounty(ctx context.Context, id int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := getCountyTx(ctx, tx, id)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM counties WHERE id = ?", id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityCounties, strconv.Itoa(id), current); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateMunicipality dodaje novi grad ili općinu
func (r *TerritoryRepository) CreateMunicipality(ctx context.Context, m *models.Municipality) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO municipalities (county_id, name, type, head_title, head_name, postal_code, area_sqkm, population, email, phone, website)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, m.CountyID, m.Name, m.Type, m.HeadTitle, m.HeadName, m.PostalCode, m.AreaSqKm, m.Population, m.Email, m.Phone, m.Website)
	if err != nil {
		return fmt.Errorf("greška pri dodavanju grada/općine: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		m.ID = int(id)
	}
	if _, err := r.rec.Record(ctx, tx, EntityMunicipalities, strconv.Itoa(m.ID), m); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateMunicipality ažurira grad ili općinu
func (r *TerritoryRepository) UpdateMunicipality(ctx context.Context, m *models.Municipality) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE municipalities
		SET county_id = ?, name = ?, type = ?, head_title = ?, head_name = ?, postal_code = ?, area_sqkm = ?, population = ?,
			email = ?, phone = ?, website = ?
		WHERE id = ?
	`, m.CountyID, m.Name, m.Type, m.HeadTitle, m.HeadName, m.PostalCode, m.AreaSqKm, m.Population, m.Email, m.Phone, m.Website, m.ID); err != nil {
		return fmt.Errorf("greška pri ažuriranju grada/općine: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityMunicipalities, strconv.Itoa(m.ID), m); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteMunicipality uklanja grad ili općinu, njena naselja i veze s dionicama;
// sve ostaje arhivirano u knjizi
func (r *TerritoryRepository) DeleteMunicipality(ctx context.Context, id int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := getMunicipalityTx(ctx, tx, id)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err := r.archiveSectionTerritories(ctx, tx, "municipality_id = ?", id); err != nil {
		return err
	}
	settlementIDs, err := idsOf(ctx, tx, "SELECT id FROM settlements WHERE municipality_id = ?", id)
	if err != nil {
		return err
	}
	for _, sid := range settlementIDs {
		n, _ := strconv.Atoi(sid)
		st, err := getSettlementTx(ctx, tx, n)
		if err != nil {
			return err
		}
		if _, err := r.rec.Archive(ctx, tx, EntitySettlements, sid, st); err != nil {
			return err
		}
	}

	_, _ = tx.ExecContext(ctx, "DELETE FROM section_territories WHERE municipality_id = ?", id)
	_, _ = tx.ExecContext(ctx, "DELETE FROM settlements WHERE municipality_id = ?", id)
	if _, err := tx.ExecContext(ctx, "DELETE FROM municipalities WHERE id = ?", id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityMunicipalities, strconv.Itoa(id), current); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateSettlement dodaje novo naselje
func (r *TerritoryRepository) CreateSettlement(ctx context.Context, s *models.Settlement) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO settlements (municipality_id, county_id, name, postal_code, population)
		VALUES (?, ?, ?, ?, ?)
	`, s.MunicipalityID, s.CountyID, s.Name, s.PostalCode, s.Population)
	if err != nil {
		return fmt.Errorf("greška pri dodavanju naselja: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		s.ID = int(id)
	}
	if _, err := r.rec.Record(ctx, tx, EntitySettlements, strconv.Itoa(s.ID), s); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateSettlement ažurira / preimenuje naselje
func (r *TerritoryRepository) UpdateSettlement(ctx context.Context, s *models.Settlement) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE settlements
		SET name = ?, postal_code = ?, population = ?
		WHERE id = ?
	`, s.Name, s.PostalCode, s.Population, s.ID); err != nil {
		return fmt.Errorf("greška pri ažuriranju naselja: %w", err)
	}
	saved, err := getSettlementTx(ctx, tx, s.ID)
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntitySettlements, strconv.Itoa(s.ID), saved); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSettlement uklanja naselje i njegove veze s dionicama; ostaju arhivirani
func (r *TerritoryRepository) DeleteSettlement(ctx context.Context, id int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := getSettlementTx(ctx, tx, id)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err := r.archiveSectionTerritories(ctx, tx, "settlement_id = ?", id); err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, "DELETE FROM section_territories WHERE settlement_id = ?", id)
	if _, err := tx.ExecContext(ctx, "DELETE FROM settlements WHERE id = ?", id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntitySettlements, strconv.Itoa(id), current); err != nil {
		return err
	}
	return tx.Commit()
}

// GetSectionsAffectedByTerritory vraća šifre svih dionica povezanih sa zadanom županijom, općinom ili naseljem
func (r *TerritoryRepository) GetSectionsAffectedByTerritory(ctx context.Context, countyID int, muniID int, settlementID int) ([]string, error) {
	query := `SELECT DISTINCT section_code FROM section_territories WHERE 1=1`
	var args []interface{}
	if settlementID > 0 {
		query += " AND settlement_id = ?"
		args = append(args, settlementID)
	} else if muniID > 0 {
		query += " AND municipality_id = ?"
		args = append(args, muniID)
	} else if countyID > 0 {
		query += " AND county_id = ?"
		args = append(args, countyID)
	} else {
		return nil, nil
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil {
			codes = append(codes, c)
		}
	}
	return codes, nil
}
