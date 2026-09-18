package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// EntitySluzbe su službe uz županije i gradove u knjizi verzija
const EntitySluzbe = "sluzbe"

const sluzbaUpsert = `INSERT INTO sluzbe (id, county_id, municipality_id, vrsta, naziv, email, phone, napomena, redoslijed, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET county_id = excluded.county_id, municipality_id = excluded.municipality_id, vrsta = excluded.vrsta,
		naziv = excluded.naziv, email = excluded.email, phone = excluded.phone, napomena = excluded.napomena,
		redoslijed = excluded.redoslijed, updated_at = excluded.updated_at`

func sluzbaArgs(s *models.Sluzba) []any {
	return []any{s.ID, s.CountyID, s.MunicipalityID, s.Vrsta, s.Naziv, s.Email, s.Phone, s.Napomena, s.Redoslijed, s.UpdatedAt.UTC()}
}

// SaveSluzba upisuje službu s verzijom u knjizi
func (r *TerritoryRepository) SaveSluzba(ctx context.Context, s *models.Sluzba) error {
	if s.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		s.ID = id.String()
	}
	s.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, sluzbaUpsert, sluzbaArgs(s)...); err != nil {
		return fmt.Errorf("upis službe: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntitySluzbe, s.ID, s); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSluzbe vraća službe županije (countyID > 0) ili sve, s nazivom grada,
// poredane po vrsti i redoslijedu
func (r *TerritoryRepository) ListSluzbe(ctx context.Context, countyID int) ([]models.Sluzba, error) {
	q := `SELECT s.id, s.county_id, s.municipality_id, s.vrsta, s.naziv, s.email, s.phone, s.napomena, s.redoslijed, s.updated_at,
		COALESCE(m.name, '') FROM sluzbe s LEFT JOIN municipalities m ON m.id = s.municipality_id`
	var args []any
	if countyID > 0 {
		q += ` WHERE s.county_id = ?`
		args = append(args, countyID)
	}
	rows, err := r.db.QueryContext(ctx, q+` ORDER BY s.county_id, s.municipality_id, s.redoslijed, s.naziv`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Sluzba
	for rows.Next() {
		var s models.Sluzba
		if err := rows.Scan(&s.ID, &s.CountyID, &s.MunicipalityID, &s.Vrsta, &s.Naziv, &s.Email, &s.Phone, &s.Napomena, &s.Redoslijed, &s.UpdatedAt, &s.MunicipalityName); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSluzba čita jednu službu
func (r *TerritoryRepository) GetSluzba(ctx context.Context, id string) (*models.Sluzba, error) {
	var s models.Sluzba
	err := r.db.QueryRowContext(ctx, `SELECT id, county_id, municipality_id, vrsta, naziv, email, phone, napomena, redoslijed, updated_at
		FROM sluzbe WHERE id = ?`, id).Scan(&s.ID, &s.CountyID, &s.MunicipalityID, &s.Vrsta, &s.Naziv, &s.Email, &s.Phone, &s.Napomena, &s.Redoslijed, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteSluzba briše službu i ostavlja arhivsku verziju
func (r *TerritoryRepository) DeleteSluzba(ctx context.Context, id string) error {
	s, err := r.GetSluzba(ctx, id)
	if err != nil || s == nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sluzbe WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntitySluzbe, id, s); err != nil {
		return err
	}
	return tx.Commit()
}

// BrojSluzbiPoZupaniji vraća koliko službi ima svaka županija, za karticu
func (r *TerritoryRepository) BrojSluzbiPoZupaniji(ctx context.Context) (map[int]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT county_id, COUNT(*) FROM sluzbe GROUP BY county_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]int{}
	for rows.Next() {
		var id, n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

var _ = strconv.Itoa
