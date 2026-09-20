package repository

import (
	"context"
	"database/sql"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntityPotpisniKljucevi su osobni potpisni ključevi u knjizi verzija;
// privatni dio je šifriran lozinkom osobe pa smije putovati
const EntityPotpisniKljucevi = "potpisni_kljucevi"

// EntityPotpisniIzdavatelji su certifikati izdavatelja čvorova
const EntityPotpisniIzdavatelji = "potpisni_izdavatelji"

// EntityVodocuvarskiIzvornici su potpisani PDF-ovi dnevnih listova
const EntityVodocuvarskiIzvornici = "vodocuvarski_izvornici"

const potpisniKljucUpsert = `INSERT INTO potpisni_kljucevi (user_id, ime, cert, kljuc, sol, izdao, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(user_id) DO UPDATE SET ime = excluded.ime, cert = excluded.cert, kljuc = excluded.kljuc, sol = excluded.sol, izdao = excluded.izdao, updated_at = excluded.updated_at`

const izdavateljUpsert = `INSERT INTO potpisni_izdavatelji (cvor, cert, created_at) VALUES (?, ?, ?)
	ON CONFLICT(cvor) DO UPDATE SET cert = excluded.cert, created_at = excluded.created_at`

const izvornikListaUpsert = `INSERT INTO vodocuvarski_izvornici (list_id, otisak, bajtova, vrsta, sazetak, updated_at) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(list_id) DO UPDATE SET otisak = excluded.otisak, bajtova = excluded.bajtova, vrsta = excluded.vrsta,
		sazetak = excluded.sazetak, updated_at = excluded.updated_at`

// PotpisRepository čuva ključeve osoba i izdavatelje čvorova
type PotpisRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

// NewPotpisRepository otvara repozitorij potpisa
func NewPotpisRepository(db *sql.DB, rec *ledger.Recorder) *PotpisRepository {
	return &PotpisRepository{db: db, rec: rec}
}

// GetKljuc čita ključ osobe; nil kad ga nema
func (r *PotpisRepository) GetKljuc(ctx context.Context, userID string) (*models.PotpisniKljuc, error) {
	k := models.PotpisniKljuc{UserID: userID}
	err := r.db.QueryRowContext(ctx, `SELECT ime, cert, kljuc, sol, izdao, created_at, updated_at FROM potpisni_kljucevi WHERE user_id = ?`, userID).
		Scan(&k.Ime, &k.Cert, &k.Kljuc, &k.Sol, &k.Izdao, &k.CreatedAt, &k.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// ListKljucevi vraća sve izdane ključeve, za pregled u administraciji
func (r *PotpisRepository) ListKljucevi(ctx context.Context) ([]models.PotpisniKljuc, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_id, ime, cert, izdao, created_at, updated_at FROM potpisni_kljucevi ORDER BY ime`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PotpisniKljuc
	for rows.Next() {
		var k models.PotpisniKljuc
		if err := rows.Scan(&k.UserID, &k.Ime, &k.Cert, &k.Izdao, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SaveKljuc sprema ključ osobe, s verzijom u knjizi
func (r *PotpisRepository) SaveKljuc(ctx context.Context, k *models.PotpisniKljuc) error {
	now := time.Now().UTC()
	if k.CreatedAt.IsZero() {
		k.CreatedAt = now
	}
	k.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, potpisniKljucUpsert, k.UserID, k.Ime, k.Cert, k.Kljuc, k.Sol, k.Izdao, k.CreatedAt, k.UpdatedAt); err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityPotpisniKljucevi, k.UserID, k); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteKljuc briše ključ osobe
func (r *PotpisRepository) DeleteKljuc(ctx context.Context, userID string) error {
	k, err := r.GetKljuc(ctx, userID)
	if err != nil || k == nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM potpisni_kljucevi WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityPotpisniKljucevi, userID, k); err != nil {
		return err
	}
	return tx.Commit()
}

// GetIzdavatelj čita certifikat izdavatelja čvora; nil kad ga nema
func (r *PotpisRepository) GetIzdavatelj(ctx context.Context, cvor string) (*models.IzdavateljPotpisa, error) {
	i := models.IzdavateljPotpisa{Cvor: cvor}
	err := r.db.QueryRowContext(ctx, `SELECT cert, created_at FROM potpisni_izdavatelji WHERE cvor = ?`, cvor).Scan(&i.Cert, &i.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// ListIzdavatelji vraća izdavatelje svih čvorova
func (r *PotpisRepository) ListIzdavatelji(ctx context.Context) ([]models.IzdavateljPotpisa, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT cvor, cert, created_at FROM potpisni_izdavatelji ORDER BY cvor`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.IzdavateljPotpisa
	for rows.Next() {
		var i models.IzdavateljPotpisa
		if err := rows.Scan(&i.Cvor, &i.Cert, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// SaveIzdavatelj objavljuje certifikat izdavatelja čvora u knjizi
func (r *PotpisRepository) SaveIzdavatelj(ctx context.Context, i *models.IzdavateljPotpisa) error {
	if i.CreatedAt.IsZero() {
		i.CreatedAt = time.Now().UTC()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, izdavateljUpsert, i.Cvor, i.Cert, i.CreatedAt); err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityPotpisniIzdavatelji, i.Cvor, i); err != nil {
		return err
	}
	return tx.Commit()
}
