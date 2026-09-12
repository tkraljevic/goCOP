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
const dezurstvoUpsert = `INSERT INTO dezurstva (id, journal_id, user_id, user_name, od, do_, podrucje, opis, mjesto, napomena, potvrdio, potvrdeno_at, created_by, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET journal_id = excluded.journal_id, user_id = excluded.user_id, user_name = excluded.user_name,
		od = excluded.od, do_ = excluded.do_, podrucje = excluded.podrucje, opis = excluded.opis, mjesto = excluded.mjesto, napomena = excluded.napomena,
		potvrdio = excluded.potvrdio, potvrdeno_at = excluded.potvrdeno_at,
		created_by = excluded.created_by, created_at = excluded.created_at, updated_at = excluded.updated_at`

func dezurstvoArgs(d *models.Dezurstvo) []any {
	var potvrdeno any
	if d.PotvrdenoAt != nil {
		potvrdeno = d.PotvrdenoAt.UTC()
	}
	var podrucje any
	if d.ZaPodrucje() {
		podrucje = *d.Podrucje
	}
	return []any{d.ID, d.JournalID, d.UserID, d.UserName, d.Od.UTC(), d.Do.UTC(), podrucje, d.Opis, d.Mjesto, d.Napomena, d.Potvrdio, potvrdeno, d.CreatedBy, d.CreatedAt, d.UpdatedAt}
}

const dezurstvoColumns = `id, journal_id, user_id, user_name, od, do_, podrucje, opis, mjesto, napomena, potvrdio, potvrdeno_at, created_by, created_at, updated_at`

func scanDezurstvo(row interface{ Scan(...any) error }) (*models.Dezurstvo, error) {
	var d models.Dezurstvo
	var potvrdeno sql.NullTime
	var podrucje sql.NullInt64
	if err := row.Scan(&d.ID, &d.JournalID, &d.UserID, &d.UserName, &d.Od, &d.Do, &podrucje, &d.Opis, &d.Mjesto, &d.Napomena, &d.Potvrdio, &potvrdeno, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	if podrucje.Valid && podrucje.Int64 > 0 {
		n := int(podrucje.Int64)
		d.Podrucje = &n
	}
	d.Od, d.Do = d.Od.In(models.Zagreb), d.Do.In(models.Zagreb)
	if potvrdeno.Valid {
		t := potvrdeno.Time.In(models.Zagreb)
		d.PotvrdenoAt = &t
	}
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

// BrojDezurstava broji sva dežurstva, za razdjelnicu na /dnevnici
func (r *JournalRepository) BrojDezurstava(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM dezurstva`).Scan(&n)
	return n, err
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

// PlanoviOsobe vraća planove u kojima osoba ima dežurstva, najnoviji prvi.
// Zbraja se u Gou: datumi su pohranjeni u Go-ovu zapisu koji SQLite-ov
// strftime ne čita.
func (r *JournalRepository) PlanoviOsobe(ctx context.Context, userID string) ([]models.PlanOsobe, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT j.id, j.title, COALESCE(s.center_cop, ''), j.centar_sektor, j.ended_at IS NOT NULL, d.od, d.do_, d.potvrdeno_at IS NULL
		FROM dezurstva d JOIN journals j ON j.id = d.journal_id LEFT JOIN sectors s ON s.id = j.centar_sektor
		WHERE d.user_id = ? ORDER BY d.od`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	po := map[string]*models.PlanOsobe{}
	var redom []string
	for rows.Next() {
		var p models.PlanOsobe
		var od, do time.Time
		var ceka bool
		if err := rows.Scan(&p.JournalID, &p.Naslov, &p.Centar, &p.Sektor, &p.Zakljucen, &od, &do, &ceka); err != nil {
			return nil, err
		}
		od, do = od.In(models.Zagreb), do.In(models.Zagreb)
		g := po[p.JournalID]
		if g == nil {
			p.Prvo, p.Zadnje = od, do
			g = &p
			po[p.JournalID] = g
			redom = append(redom, p.JournalID)
		}
		g.Dezurstava++
		g.Sati += do.Sub(od)
		if ceka {
			g.CekaPotvrdu++
		}
		if od.Before(g.Prvo) {
			g.Prvo = od
		}
		if do.After(g.Zadnje) {
			g.Zadnje = do
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]models.PlanOsobe, 0, len(redom))
	for _, id := range redom {
		out = append(out, *po[id])
	}
	// najnoviji plan prvi
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
