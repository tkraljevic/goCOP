package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// EntityDezurstva je naziv entiteta plana dežurstava u knjizi verzija
const EntityDezurstva = "dezurstva"

// Dežurstvo putuje kanalom svog dnevnika: tko prati dnevnik, prati i plan.
const dezurstvoUpsert = `INSERT INTO dezurstva (id, journal_id, user_id, user_name, od, do_, opis, mjesto, napomena, created_by, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET journal_id = excluded.journal_id, user_id = excluded.user_id, user_name = excluded.user_name,
		od = excluded.od, do_ = excluded.do_, opis = excluded.opis, mjesto = excluded.mjesto, napomena = excluded.napomena,
		created_by = excluded.created_by, created_at = excluded.created_at, updated_at = excluded.updated_at`

func dezurstvoArgs(d *models.Dezurstvo) []any {
	return []any{d.ID, d.JournalID, d.UserID, d.UserName, d.Od.UTC(), d.Do.UTC(), d.Opis, d.Mjesto, d.Napomena, d.CreatedBy, d.CreatedAt, d.UpdatedAt}
}

const dezurstvoColumns = `id, journal_id, user_id, user_name, od, do_, opis, mjesto, napomena, created_by, created_at, updated_at`

func scanDezurstvo(row interface{ Scan(...any) error }) (*models.Dezurstvo, error) {
	var d models.Dezurstvo
	if err := row.Scan(&d.ID, &d.JournalID, &d.UserID, &d.UserName, &d.Od, &d.Do, &d.Opis, &d.Mjesto, &d.Napomena, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	d.Od, d.Do = d.Od.In(models.Zagreb), d.Do.In(models.Zagreb)
	return &d, nil
}

// SaveDezurstvo upisuje ili mijenja dežurstvo i bilježi verziju u kanal dnevnika
func (r *JournalRepository) SaveDezurstvo(ctx context.Context, d *models.Dezurstvo) error {
	now := time.Now().UTC()
	if d.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		d.ID = id.String()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, dezurstvoUpsert, dezurstvoArgs(d)...); err != nil {
		return fmt.Errorf("greška pri upisu dežurstva: %w", err)
	}
	if _, err := r.rec.RecordIn(ctx, tx, channelOfJournal(ctx, tx, d.JournalID), EntityDezurstva, d.ID, d); err != nil {
		return err
	}
	return tx.Commit()
}

// GetDezurstvo vraća dežurstvo; nil kad ga nema
func (r *JournalRepository) GetDezurstvo(ctx context.Context, id string) (*models.Dezurstvo, error) {
	d, err := scanDezurstvo(r.db.QueryRowContext(ctx, `SELECT `+dezurstvoColumns+` FROM dezurstva WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

// ListDezurstva vraća dežurstva dnevnika redom početka
func (r *JournalRepository) ListDezurstva(ctx context.Context, journalID string) ([]models.Dezurstvo, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+dezurstvoColumns+` FROM dezurstva WHERE journal_id = ? ORDER BY od, user_name`, journalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Dezurstvo
	for rows.Next() {
		d, err := scanDezurstvo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ArhivirajDezurstvo miče dežurstvo s površine; u knjizi ostaje kao
// arhivirano, pa i drugi čvorovi znaju da ga više nema
func (r *JournalRepository) ArhivirajDezurstvo(ctx context.Context, d *models.Dezurstvo) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM dezurstva WHERE id = ?`, d.ID); err != nil {
		return err
	}
	if _, err := r.rec.ArchiveIn(ctx, tx, channelOfJournal(ctx, tx, d.JournalID), EntityDezurstva, d.ID, d); err != nil {
		return err
	}
	return tx.Commit()
}
