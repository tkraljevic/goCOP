package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Nazivi entiteta u knjizi verzija
const (
	EntityKisomjeri = "kisomjeri"
	EntitySlivovi   = "slivovi"
)

// KisomjerRepository čuva registar točaka kvazi-kišomjera i slivova.
// Oboje ide kroz knjigu verzija kao i ostali registri, pa se dijeli među
// čvorovima; same oborine ne idu ovuda — njih je previše i uvijek se mogu
// ponovno preuzeti.
type KisomjerRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewKisomjerRepository(db *sql.DB, rec *ledger.Recorder) *KisomjerRepository {
	return &KisomjerRepository{db: db, rec: rec}
}

const kisomjerColumns = `code, naziv, sliv, pojas, latitude, longitude, visina, srednja_visina, km2, tezina,
	aktivan, napomena, created_at, updated_at`

func scanKisomjer(scanner interface{ Scan(...any) error }) (models.Kisomjer, error) {
	var (
		k                    models.Kisomjer
		visina, srednja, km2 sql.NullFloat64
		tezina               sql.NullFloat64
		aktivan              int
	)
	err := scanner.Scan(&k.Code, &k.Naziv, &k.Sliv, &k.Pojas, &k.Latitude, &k.Longitude,
		&visina, &srednja, &km2, &tezina, &aktivan, &k.Napomena, &k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return k, err
	}
	k.Visina = nullFloatPtr(visina)
	k.SrednjaVisina = nullFloatPtr(srednja)
	k.Km2 = nullFloatPtr(km2)
	k.Tezina = nullFloatPtr(tezina)
	k.Aktivan = aktivan != 0
	return k, nil
}

// ListKisomjeri vraća sve točke, po slivu pa po šifri
func (r *KisomjerRepository) ListKisomjeri(ctx context.Context) ([]models.Kisomjer, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+kisomjerColumns+` FROM kisomjeri ORDER BY sliv, code`)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju kišomjera: %w", err)
	}
	defer rows.Close()
	var out []models.Kisomjer
	for rows.Next() {
		k, err := scanKisomjer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// GetKisomjer dohvaća jednu točku; nil kad je nema
func (r *KisomjerRepository) GetKisomjer(ctx context.Context, code string) (*models.Kisomjer, error) {
	k, err := scanKisomjer(r.db.QueryRowContext(ctx, `SELECT `+kisomjerColumns+` FROM kisomjeri WHERE code = ?`, code))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju kišomjera: %w", err)
	}
	return &k, nil
}

// CreateKisomjer upisuje novu točku
func (r *KisomjerRepository) CreateKisomjer(ctx context.Context, k *models.Kisomjer) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	k.CreatedAt, k.UpdatedAt = now, now
	_, err = tx.ExecContext(ctx, `
		INSERT INTO kisomjeri (`+kisomjerColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.Code, k.Naziv, k.Sliv, k.Pojas, k.Latitude, k.Longitude, k.Visina, k.SrednjaVisina, k.Km2, k.Tezina,
		boolToInt(k.Aktivan), k.Napomena, k.CreatedAt, k.UpdatedAt)
	if err != nil {
		return fmt.Errorf("greška pri unosu kišomjera %q: %w", k.Code, err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityKisomjeri, k.Code, k); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateKisomjer mijenja točku; šifra ostaje, na nju se vežu oborine
func (r *KisomjerRepository) UpdateKisomjer(ctx context.Context, k *models.Kisomjer) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	k.UpdatedAt = time.Now().UTC()
	res, err := tx.ExecContext(ctx, `
		UPDATE kisomjeri SET naziv = ?, sliv = ?, pojas = ?, latitude = ?, longitude = ?, visina = ?,
			srednja_visina = ?, km2 = ?, tezina = ?, aktivan = ?, napomena = ?, updated_at = ?
		WHERE code = ?`,
		k.Naziv, k.Sliv, k.Pojas, k.Latitude, k.Longitude, k.Visina, k.SrednjaVisina, k.Km2, k.Tezina,
		boolToInt(k.Aktivan), k.Napomena, k.UpdatedAt, k.Code)
	if err != nil {
		return fmt.Errorf("greška pri izmjeni kišomjera %q: %w", k.Code, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("kišomjer %q nije pronađen", k.Code)
	}
	if _, err := r.rec.Record(ctx, tx, EntityKisomjeri, k.Code, k); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteKisomjer uklanja točku s površine; u knjizi ostaje arhivirana
func (r *KisomjerRepository) DeleteKisomjer(ctx context.Context, code string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := scanKisomjer(tx.QueryRowContext(ctx, `SELECT `+kisomjerColumns+` FROM kisomjeri WHERE code = ?`, code))
	if err == sql.ErrNoRows {
		return fmt.Errorf("kišomjer %q nije pronađen", code)
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kisomjeri WHERE code = ?`, code); err != nil {
		return fmt.Errorf("greška pri brisanju kišomjera: %w", err)
	}
	if _, err := r.rec.Archive(ctx, tx, EntityKisomjeri, code, current); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSlivovi vraća slivove po oznaci
func (r *KisomjerRepository) ListSlivovi(ctx context.Context) ([]models.Sliv, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT oznaka, naziv, km2, geometry, napomena FROM slivovi ORDER BY oznaka`)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvaćanju slivova: %w", err)
	}
	defer rows.Close()
	var out []models.Sliv
	for rows.Next() {
		var m models.Sliv
		var km2 sql.NullFloat64
		if err := rows.Scan(&m.Oznaka, &m.Naziv, &km2, &m.Geometry, &m.Napomena); err != nil {
			return nil, err
		}
		m.Km2 = nullFloatPtr(km2)
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpsertSliv upisuje ili mijenja sliv
func (r *KisomjerRepository) UpsertSliv(ctx context.Context, m *models.Sliv) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, upsertSlivSQL, m.Oznaka, m.Naziv, m.Km2, m.Geometry, m.Napomena); err != nil {
		return fmt.Errorf("greška pri upisu sliva %q: %w", m.Oznaka, err)
	}
	if _, err := r.rec.Record(ctx, tx, EntitySlivovi, m.Oznaka, m); err != nil {
		return err
	}
	return tx.Commit()
}

const upsertSlivSQL = `
	INSERT INTO slivovi (oznaka, naziv, km2, geometry, napomena) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(oznaka) DO UPDATE SET naziv = excluded.naziv, km2 = excluded.km2,
		geometry = excluded.geometry, napomena = excluded.napomena`

// DeleteSliv uklanja sliv; točke koje se na njega pozivaju ostaju
func (r *KisomjerRepository) DeleteSliv(ctx context.Context, oznaka string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var m models.Sliv
	var km2 sql.NullFloat64
	err = tx.QueryRowContext(ctx, `SELECT oznaka, naziv, km2, geometry, napomena FROM slivovi WHERE oznaka = ?`, oznaka).
		Scan(&m.Oznaka, &m.Naziv, &km2, &m.Geometry, &m.Napomena)
	if err == sql.ErrNoRows {
		return fmt.Errorf("sliv %q nije pronađen", oznaka)
	}
	if err != nil {
		return err
	}
	m.Km2 = nullFloatPtr(km2)
	if _, err := tx.ExecContext(ctx, `DELETE FROM slivovi WHERE oznaka = ?`, oznaka); err != nil {
		return fmt.Errorf("greška pri brisanju sliva: %w", err)
	}
	if _, err := r.rec.Archive(ctx, tx, EntitySlivovi, oznaka, m); err != nil {
		return err
	}
	return tx.Commit()
}

// upsertKisomjer primjenjuje verziju iz knjige na površinu
func upsertKisomjer(ctx context.Context, tx *sql.Tx, k models.Kisomjer) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO kisomjeri (`+kisomjerColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET naziv = excluded.naziv, sliv = excluded.sliv, pojas = excluded.pojas,
			latitude = excluded.latitude, longitude = excluded.longitude, visina = excluded.visina,
			srednja_visina = excluded.srednja_visina, km2 = excluded.km2, tezina = excluded.tezina,
			aktivan = excluded.aktivan, napomena = excluded.napomena, updated_at = excluded.updated_at`,
		k.Code, k.Naziv, k.Sliv, k.Pojas, k.Latitude, k.Longitude, k.Visina, k.SrednjaVisina, k.Km2, k.Tezina,
		boolToInt(k.Aktivan), k.Napomena, k.CreatedAt.UTC(), k.UpdatedAt.UTC())
	return err
}
