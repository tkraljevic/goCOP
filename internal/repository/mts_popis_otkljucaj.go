package repository

import (
	"context"

	"gocop/internal/models"
)

// uskladjenjaPopisaSQL traži retke prometa koje je upisalo zaključenje popisa
const uskladjenjaPopisaSQL = `SELECT ` + prometColumns + prometFrom + ` WHERE p.popis_id = ? AND p.vrsta = ? ORDER BY p.created_at, p.id`

// UskladjenjaPopisa vraća usklađenja koja je zaključenje popisa upisalo u
// promet; prazno kad ih nema
func (r *MtsRepository) UskladjenjaPopisa(ctx context.Context, popisID string) ([]models.Promet, error) {
	rows, err := r.db.QueryContext(ctx, uskladjenjaPopisaSQL, popisID, models.PrometPopis)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Promet
	for rows.Next() {
		p, err := scanPromet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OtkljucajPopis vraća zaključeni popis u nacrt: usklađenja koja je
// zaključenje upisalo miču se iz prometa (u knjizi ostaju arhivirana), a
// popis se upisuje bez zaključenja. Sve ide u jednoj transakciji. Nacrt koji
// bi zadržao usklađenja ponovnim bi ih zaključenjem udvostručio, a
// zaključeni popis bez usklađenja pokazivao bi stanje koje knjiga nema.
func (r *MtsRepository) OtkljucajPopis(ctx context.Context, p *models.Popis, uskladjenja []models.Promet) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.arhivirajRetke(ctx, tx, uskladjenja); err != nil {
		return err
	}
	p.ZakljucenoAt = nil
	if err := r.upisiPopis(ctx, tx, p); err != nil {
		return err
	}
	return tx.Commit()
}
