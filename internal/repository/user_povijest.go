package repository

import (
	"encoding/json"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// PovijestZaduzenja su sva zaduženja osobe, i opozvana i istekla, kako su
// stajala kroz vrijeme (models.SloziPovijestZaduzenja): ranije verzije iz
// knjige verzija, a zadnje stanje sa zapisa na površini. Samo čita.
func (r *UserRepository) PovijestZaduzenja(userID uuid.UUID) ([]models.VerzijaZaduzenja, error) {
	sva, err := r.dutiesForUser(userID, `1 = 1`, `created_at ASC`)
	if err != nil {
		return nil, err
	}
	knjiga, err := r.verzijeZaduzenja(userID)
	if err != nil {
		return nil, err
	}
	return models.SloziPovijestZaduzenja(sva, knjiga), nil
}

// verzijeZaduzenja su verzije zaduženja osobe iz knjige verzija, po
// zaduženju redom nastanka. Verzija koja se ne da pročitati je greška:
// povijest bez nje pogrešno bi produljila prethodnu verziju.
func (r *UserRepository) verzijeZaduzenja(userID uuid.UUID) ([]models.VerzijaZaduzenja, error) {
	rows, err := r.db.Query(`
		SELECT v.payload, v.created_at FROM record_versions v
		JOIN duties d ON d.id = v.entity_id
		WHERE v.entity = ? AND d.user_id = ?
		ORDER BY v.entity_id, v.version_id`, EntityDuties, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.VerzijaZaduzenja
	for rows.Next() {
		var payload string
		var v models.VerzijaZaduzenja
		if err := rows.Scan(&payload, &v.Od); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &v.Duty); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
