package repository

import (
	"context"
	"database/sql"
	"time"
)

// FollowRepository pamti letve čiju povijest ovaj čvor drži u cijelosti.
// Tablica je lokalna i ne ide u knjigu verzija: koliko će koje računalo
// držati na disku nije stvar mreže nego čovjeka za tim računalom.
type FollowRepository struct{ db *sql.DB }

func NewFollowRepository(db *sql.DB) *FollowRepository { return &FollowRepository{db: db} }

// Follow je jedna praćena letva
type Follow struct {
	GaugeKey  string
	Name      string
	CreatedAt time.Time
}

// List vraća praćene letve, najnovije prve
func (r *FollowRepository) List(ctx context.Context) ([]Follow, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT gauge_key, name, created_at FROM reading_follows ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Follow
	for rows.Next() {
		var f Follow
		if err := rows.Scan(&f.GaugeKey, &f.Name, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Keys vraća skup praćenih letvi za politiku povijesti
func (r *FollowRepository) Keys(ctx context.Context) (map[string]bool, error) {
	list, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, f := range list {
		out[f.GaugeKey] = true
	}
	return out, nil
}

// Add počinje pratiti letvu
func (r *FollowRepository) Add(ctx context.Context, key, name string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO reading_follows (gauge_key, name, created_at) VALUES (?, ?, ?)
		ON CONFLICT(gauge_key) DO UPDATE SET name = excluded.name`, key, name, time.Now().UTC())
	return err
}

// Remove prestaje pratiti letvu; već preuzeta očitanja ostaju
func (r *FollowRepository) Remove(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM reading_follows WHERE gauge_key = ?`, key)
	return err
}
