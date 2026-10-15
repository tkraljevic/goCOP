package repository

import (
	"database/sql"
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

// ImaoUlogu javlja je li osoba ikad imala zaduženje s ulogom: na zapisu
// zaduženja (i opozvanom i isteklom) ili u ranijoj verziji iz knjige
// verzija, kad je zaduženje poslije izmijenjeno u drugu ulogu. Samo čita.
func (r *UserRepository) ImaoUlogu(userID uuid.UUID, uloga models.Role) (bool, error) {
	var imao bool
	err := r.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM duties WHERE user_id = ? AND role = ?)
		OR EXISTS (SELECT 1 FROM record_versions v JOIN duties d ON d.id = v.entity_id
			WHERE v.entity = ? AND d.user_id = ? AND json_extract(v.payload, '$.role') = ?)`,
		userID.String(), string(uloga), EntityDuties, userID.String(), string(uloga)).Scan(&imao)
	return imao, err
}

// MjestaVodocuvara su, po osobi (oznaka računa), mjesta svih zaduženja
// vodočuvara koja je ikad imala: uloga, sektor i branjeno područje sa
// zapisa zaduženja (i opozvanog i isteklog) i iz ranijih verzija u knjizi
// verzija (zaduženje poslije premješteno izmjenom u mjestu). Svako mjesto
// jednom. Samo čita.
func (r *UserRepository) MjestaVodocuvara() (map[string][]models.Duty, error) {
	rows, err := r.db.Query(`SELECT user_id, sector_id, area_id FROM duties WHERE role = ?
		UNION
		SELECT d.user_id, json_extract(v.payload, '$.sector_id'), json_extract(v.payload, '$.area_id')
		FROM record_versions v JOIN duties d ON d.id = v.entity_id
		WHERE v.entity = ? AND json_extract(v.payload, '$.role') = ?`,
		string(models.RoleWaterGuard), EntityDuties, string(models.RoleWaterGuard))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]models.Duty{}
	for rows.Next() {
		var korisnik string
		var sektor sql.NullString
		var podrucje sql.NullInt64
		if err := rows.Scan(&korisnik, &sektor, &podrucje); err != nil {
			return nil, err
		}
		out[korisnik] = append(out[korisnik], mjestoVodocuvara(sektor, podrucje))
	}
	return out, rows.Err()
}

// mjestoVodocuvara je zaduženje vodočuvara koje nosi samo mjesto: sektor i
// branjeno područje, kad su upisani
func mjestoVodocuvara(sektor sql.NullString, podrucje sql.NullInt64) models.Duty {
	d := models.Duty{Role: models.RoleWaterGuard}
	if sektor.Valid {
		d.SectorID = &sektor.String
	}
	if podrucje.Valid {
		a := int(podrucje.Int64)
		d.AreaID = &a
	}
	return d
}
