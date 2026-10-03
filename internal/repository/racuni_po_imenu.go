package repository

import (
	"fmt"

	"gocop/internal/models"
)

// RacuniPoImenu vraća sve račune čije je korisničko ime jednako zadanom bez
// obzira na velika i mala slova, poredane po imenu. Stupac username je
// jedinstven uz razlikovanje slova, pa ih može biti više (tkraljevic i
// TKraljevic); GetUserByUsername tada vraća samo prvi koji nađe. Dužnosti
// se ne učitavaju.
func (r *UserRepository) RacuniPoImenu(username string) ([]models.User, error) {
	rows, err := r.db.Query("SELECT "+userColumns+" FROM users WHERE username = ? COLLATE NOCASE ORDER BY username", username)
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatu korisnika: %w", err)
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("greška pri dohvatu korisnika: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
