package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Računi čvora za zatvorene sustave koji ne idu po letvi, nego vrijede za sve
// postaje na sustavu — npr. mletva.voda.hr. Drže se samo na ovom čvoru, s
// lozinkom šifriranom ključem izvedenim iz ključa čvora, i ne putuju
// razmjenom.

// RacunSustava je jedan upisan račun.
type RacunSustava struct {
	Sustav    string
	Korisnik  string
	Lozinka   []byte // šifrirana
	UpdatedAt time.Time
}

// RacuniSustavaRepository čita i piše račune.
type RacuniSustavaRepository struct{ db *sql.DB }

// NewRacuniSustavaRepository sastavlja repozitorij nad bazom čvora.
func NewRacuniSustavaRepository(db *sql.DB) *RacuniSustavaRepository {
	return &RacuniSustavaRepository{db: db}
}

// Spremi upisuje ili mijenja račun.
func (r *RacuniSustavaRepository) Spremi(ctx context.Context, x *RacunSustava) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO racuni_sustava (sustav, korisnik, lozinka, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(sustav) DO UPDATE SET korisnik = excluded.korisnik,
			lozinka = excluded.lozinka, updated_at = excluded.updated_at`,
		strings.TrimSpace(x.Sustav), strings.TrimSpace(x.Korisnik), x.Lozinka, time.Now())
	return err
}

// Racun vraća račun za sustav; nil bez greške kad nije upisan.
func (r *RacuniSustavaRepository) Racun(ctx context.Context, sustav string) (*RacunSustava, error) {
	var x RacunSustava
	err := r.db.QueryRowContext(ctx,
		`SELECT sustav, korisnik, lozinka, updated_at FROM racuni_sustava WHERE sustav = ?`,
		strings.TrimSpace(sustav)).Scan(&x.Sustav, &x.Korisnik, &x.Lozinka, &x.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// Obrisi miče račun s ovog čvora.
func (r *RacuniSustavaRepository) Obrisi(ctx context.Context, sustav string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM racuni_sustava WHERE sustav = ?`, strings.TrimSpace(sustav))
	return err
}
