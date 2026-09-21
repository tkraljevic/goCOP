package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Računi za telemetriju na Geolux HydroViewu. Drže se samo na ovom čvoru,
// s lozinkom šifriranom ključem izvedenim iz ključa čvora, i ne putuju
// razmjenom. Ključ zapisa je šifra letve; prazna šifra je račun čvora, koji
// vrijedi za svaku letvu koja nema svoj — tako sektor može dodati svoj račun
// za svoje postaje, a da ostale i dalje rade.

// RacunHidroView je jedan upisan račun.
type RacunHidroView struct {
	Letva     string // prazno = račun čvora
	Adresa    string // prazno = zadana adresa sustava
	Korisnik  string
	Lozinka   []byte // šifrirana
	UpdatedAt time.Time
}

// HidroViewRepository čita i piše račune.
type HidroViewRepository struct{ db *sql.DB }

// NewHidroViewRepository sastavlja repozitorij nad bazom čvora.
func NewHidroViewRepository(db *sql.DB) *HidroViewRepository {
	return &HidroViewRepository{db: db}
}

// Spremi upisuje ili mijenja račun.
func (r *HidroViewRepository) Spremi(ctx context.Context, x *RacunHidroView) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO hidroview_racuni (letva, adresa, korisnik, lozinka, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(letva) DO UPDATE SET adresa = excluded.adresa, korisnik = excluded.korisnik,
			lozinka = excluded.lozinka, updated_at = excluded.updated_at`,
		strings.TrimSpace(x.Letva), strings.TrimSpace(x.Adresa), strings.TrimSpace(x.Korisnik),
		x.Lozinka, time.Now())
	return err
}

// Racun vraća račun za zadanu letvu, a kad ga ona nema — račun čvora.
// Vraća nil bez greške kad nije upisan nijedan.
func (r *HidroViewRepository) Racun(ctx context.Context, letva string) (*RacunHidroView, error) {
	for _, kljuc := range []string{strings.TrimSpace(letva), ""} {
		var x RacunHidroView
		err := r.db.QueryRowContext(ctx,
			`SELECT letva, adresa, korisnik, lozinka, updated_at FROM hidroview_racuni WHERE letva = ?`,
			kljuc).Scan(&x.Letva, &x.Adresa, &x.Korisnik, &x.Lozinka, &x.UpdatedAt)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &x, nil
	}
	return nil, nil
}

// Racuni vraća sve upisane račune, račun čvora prvi.
func (r *HidroViewRepository) Racuni(ctx context.Context) ([]RacunHidroView, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT letva, adresa, korisnik, lozinka, updated_at FROM hidroview_racuni ORDER BY letva`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RacunHidroView
	for rows.Next() {
		var x RacunHidroView
		if err := rows.Scan(&x.Letva, &x.Adresa, &x.Korisnik, &x.Lozinka, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Obrisi miče račun s ovog čvora.
func (r *HidroViewRepository) Obrisi(ctx context.Context, letva string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM hidroview_racuni WHERE letva = ?`, strings.TrimSpace(letva))
	return err
}
