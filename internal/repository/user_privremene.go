package repository

// Privremene dužnosti (privremeno imenovanje): zadani datum, istek s
// prestankom redovne i izvanredne obrane i dužnost privremene uprave iz koje
// je zaduženje dodijeljeno. Stvarni istek (expires_at) računa servis; ovdje se
// samo čita i upisuje, uz verziju u knjizi.

import (
	"context"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// ovisiOZapis je dužnost iz koje je zaduženje dodijeljeno, za stupac ovisi_o
func ovisiOZapis(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// ovisiOIzZapisa čita stupac ovisi_o; prazno ili neispravno je nil
func ovisiOIzZapisa(s string) *uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

// PrivremeneSIstekom su aktivne privremene dužnosti kojima istek ovisi o
// obrani ili o drugoj dužnosti, i one kojima je istek već prošao: poništen
// akt o prekidu obrane vraća ih
func (r *UserRepository) PrivremeneSIstekom() ([]models.Duty, error) {
	ctx := context.Background()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids, err := idsOf(ctx, tx, `SELECT id FROM duties
		WHERE is_active = 1 AND is_temporary = 1 AND (istece_s_obranom = 1 OR ovisi_o <> '')
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	out := make([]models.Duty, 0, len(ids))
	for _, id := range ids {
		d, err := getDutyTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// PostaviIstek upisuje stvarni istek dužnosti i bilježi verziju u knjigu, da
// ga razmjenom dobiju i drugi čvorovi (i oni starije inačice, koji gledaju
// samo expires_at)
func (r *UserRepository) PostaviIstek(id uuid.UUID, istek *time.Time) error {
	ctx := context.Background()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE duties SET expires_at = ? WHERE id = ?`, nullTime(istek), id.String()); err != nil {
		return err
	}
	d, err := getDutyTx(ctx, tx, id.String())
	if err != nil {
		return err
	}
	if _, err := r.rec.Record(ctx, tx, EntityDuties, id.String(), d); err != nil {
		return err
	}
	return tx.Commit()
}
