package repository

import (
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
)

// ImaVazeceZaduzenjeNaPodrucju javlja ima li na branjenom području itko s
// uključenim računom i važećom dužnošću (aktivnom, neisteklom) u jednoj od
// uloga. Bez područja ili bez uloga nema nikoga. Samo čita.
func (r *UserRepository) ImaVazeceZaduzenjeNaPodrucju(podrucje int, uloge []models.Role) (bool, error) {
	if podrucje <= 0 || len(uloge) == 0 {
		return false, nil
	}
	istekovi, err := r.istekoviZaduzenjaNaPodrucju(podrucje, uloge)
	if err != nil {
		return false, err
	}
	sad := time.Now()
	return slices.ContainsFunc(istekovi, func(istek *time.Time) bool {
		return len(models.VazecaZaduzenja([]models.Duty{{IsActive: true, ExpiresAt: istek}}, sad)) > 0
	}), nil
}

// istekoviZaduzenjaNaPodrucju su rokovi aktivnih dužnosti u ulogama na
// području, osoba s uključenim računom; nil je dužnost bez roka
func (r *UserRepository) istekoviZaduzenjaNaPodrucju(podrucje int, uloge []models.Role) ([]*time.Time, error) {
	args := []any{podrucje}
	for _, u := range uloge {
		args = append(args, string(u))
	}
	mjesta := strings.TrimSuffix(strings.Repeat("?, ", len(uloge)), ", ")
	rows, err := r.db.Query(`SELECT d.expires_at FROM duties d JOIN users u ON u.id = d.user_id
		WHERE d.area_id = ? AND d.role IN (`+mjesta+`) AND d.is_active = 1 AND u.is_active = 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*time.Time
	for rows.Next() {
		var istek *time.Time
		if err := rows.Scan(&istek); err != nil {
			return nil, err
		}
		out = append(out, istek)
	}
	return out, rows.Err()
}
