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
// razmjenom. Isto vrijedi za račun koji šalje PIN prijave izvana
// (sustav "posta-pin"), uz adresu pošiljatelja.

// RacunSustava je jedan upisan račun.
type RacunSustava struct {
	Sustav   string
	Korisnik string
	Lozinka  []byte // šifrirana
	// Adresa pošiljatelja, za račun koji šalje e-poštu; ime za prijavu
	// (tkraljevic) nije adresa, a Exchange ne šalje s tuđe adrese
	Adresa string
	// NeispravanOd je trenutak kad je poslužitelj odbio lozinku; nil dok
	// je račun ispravan. Spremi ga upisuje kako je zadan (nova lozinka ga
	// briše).
	NeispravanOd *time.Time
	UpdatedAt    time.Time
}

// RacuniSustavaRepository čita i piše račune.
type RacuniSustavaRepository struct{ db *sql.DB }

// NewRacuniSustavaRepository sastavlja repozitorij nad bazom čvora.
func NewRacuniSustavaRepository(db *sql.DB) *RacuniSustavaRepository {
	return &RacuniSustavaRepository{db: db}
}

// Spremi upisuje ili mijenja račun.
func (r *RacuniSustavaRepository) Spremi(ctx context.Context, x *RacunSustava) error {
	var neispravan any
	if x.NeispravanOd != nil {
		neispravan = x.NeispravanOd.UTC()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO racuni_sustava (sustav, korisnik, lozinka, adresa, neispravan_od, updated_at) VALUES (?,?,?,?,?,?)
		ON CONFLICT(sustav) DO UPDATE SET korisnik = excluded.korisnik,
			lozinka = excluded.lozinka, adresa = excluded.adresa,
			neispravan_od = excluded.neispravan_od, updated_at = excluded.updated_at`,
		strings.TrimSpace(x.Sustav), strings.TrimSpace(x.Korisnik), x.Lozinka, strings.TrimSpace(x.Adresa), neispravan, time.Now())
	return err
}

// Racun vraća račun za sustav; nil bez greške kad nije upisan.
func (r *RacuniSustavaRepository) Racun(ctx context.Context, sustav string) (*RacunSustava, error) {
	var x RacunSustava
	var neispravan sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT sustav, korisnik, lozinka, adresa, neispravan_od, updated_at FROM racuni_sustava WHERE sustav = ?`,
		strings.TrimSpace(sustav)).Scan(&x.Sustav, &x.Korisnik, &x.Lozinka, &x.Adresa, &neispravan, &x.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if neispravan.Valid {
		t := neispravan.Time
		x.NeispravanOd = &t
	}
	return &x, nil
}

// OznaciNeispravan bilježi da je poslužitelj odbio lozinku računa; lozinka
// ostaje, ali se njome više ne pokušava do novog upisa. Već označen račun
// zadržava prvi trenutak.
func (r *RacuniSustavaRepository) OznaciNeispravan(ctx context.Context, sustav string, kad time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE racuni_sustava SET neispravan_od = ? WHERE sustav = ? AND neispravan_od IS NULL`,
		kad.UTC(), strings.TrimSpace(sustav))
	return err
}

// Obrisi miče račun s ovog čvora.
func (r *RacuniSustavaRepository) Obrisi(ctx context.Context, sustav string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM racuni_sustava WHERE sustav = ?`, strings.TrimSpace(sustav))
	return err
}
