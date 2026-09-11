package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntityBiljeske je naziv bilježaka uz arhivske vrijednosti u knjizi verzija
const EntityBiljeske = "arhiva_biljeske"

// BiljeskaRepository čuva ono što je čovjek rekao o pojedinoj arhivskoj
// vrijednosti. Arhiva ostaje netaknuta; bilješka stoji uz nju i ide kroz knjigu
// verzija, pa preživljava i ponovnu gradnju arhive i ulaganje očitanja u nju.
type BiljeskaRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewBiljeskaRepository(db *sql.DB, rec *ledger.Recorder) *BiljeskaRepository {
	return &BiljeskaRepository{db: db, rec: rec}
}

const biljeskaColumns = `id, letva, velicina, korak, vrijeme, tekst, tko, created_at, updated_at`

func scanBiljeska(sc interface{ Scan(...any) error }) (models.ArhivaBiljeska, error) {
	var b models.ArhivaBiljeska
	var id string
	if err := sc.Scan(&id, &b.Letva, &b.Velicina, &b.Korak, &b.Vrijeme, &b.Tekst,
		&b.Tko, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return b, err
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return b, fmt.Errorf("neispravan identifikator bilješke %q: %w", id, err)
	}
	b.ID = parsed
	return b, nil
}

// ZaNiz vraća bilješke jednog niza u razdoblju, ključem po trenutku — isto kao
// ispravci, jer se u listanju pokazuju uz isti redak.
func (r *BiljeskaRepository) ZaNiz(ctx context.Context, letva, velicina, korak string,
	od, do time.Time) (map[int64]models.ArhivaBiljeska, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+biljeskaColumns+` FROM arhiva_biljeske
		WHERE letva=? AND velicina=? AND korak=? AND vrijeme BETWEEN ? AND ?`,
		letva, velicina, korak, od.UTC(), do.UTC())
	if err != nil {
		return nil, fmt.Errorf("bilješke niza: %w", err)
	}
	defer rows.Close()
	out := map[int64]models.ArhivaBiljeska{}
	for rows.Next() {
		b, err := scanBiljeska(rows)
		if err != nil {
			return nil, err
		}
		out[b.Vrijeme.UTC().Unix()] = b
	}
	return out, rows.Err()
}

// Broj javlja koliko niz ima bilježaka.
func (r *BiljeskaRepository) Broj(ctx context.Context, letva, velicina, korak string) (int, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM arhiva_biljeske
		WHERE letva=? AND velicina=? AND korak=?`, letva, velicina, korak).Scan(&n)
	return n, err
}

// Vrhovi vraća bilješke kojima je čovjek rekao da je to bila kulminacija.
// One imaju jaču riječ od svakog nagađanja iz razilaženja dojava.
func (r *BiljeskaRepository) Vrhovi(ctx context.Context, letva, velicina string) ([]models.ArhivaBiljeska, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+biljeskaColumns+` FROM arhiva_biljeske
		WHERE letva=? AND velicina=? ORDER BY vrijeme`, letva, velicina)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ArhivaBiljeska
	for rows.Next() {
		b, err := scanBiljeska(rows)
		if err != nil {
			return nil, err
		}
		if b.JeVrh() {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

// Spremi upisuje bilješke i bilježi ih u knjigu verzija. Prazan tekst briše
// bilješku: bilješka bez teksta ne govori ništa, a ostavljena bi u listanju
// stajala kao prazan znak.
func (r *BiljeskaRepository) Spremi(ctx context.Context, biljeske []models.ArhivaBiljeska) (int, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("baza nije dostupna")
	}
	if len(biljeske) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	n := 0
	for idx := range biljeske {
		b := &biljeske[idx]
		var postojeci string
		err := tx.QueryRowContext(ctx, `SELECT id FROM arhiva_biljeske
			WHERE letva=? AND velicina=? AND korak=? AND vrijeme=?`,
			b.Letva, b.Velicina, b.Korak, b.Vrijeme.UTC()).Scan(&postojeci)
		if err == nil {
			if id, e := uuid.Parse(postojeci); e == nil {
				b.ID = id
			}
		} else if err != sql.ErrNoRows {
			return n, err
		}
		if b.Tekst == "" {
			if b.ID == uuid.Nil {
				continue // nema što obrisati
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM arhiva_biljeske WHERE id=?`, b.ID.String()); err != nil {
				return n, err
			}
			if _, err := r.rec.Archive(ctx, tx, EntityBiljeske, b.ID.String(), b); err != nil {
				return n, err
			}
			n++
			continue
		}
		if b.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return n, err
			}
			b.ID = id
		}
		now := time.Now().UTC()
		if b.CreatedAt.IsZero() {
			b.CreatedAt = now
		}
		b.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO arhiva_biljeske (`+biljeskaColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET tekst=excluded.tekst, tko=excluded.tko,
				updated_at=excluded.updated_at`,
			b.ID.String(), b.Letva, b.Velicina, b.Korak, b.Vrijeme.UTC(), b.Tekst,
			b.Tko, b.CreatedAt, b.UpdatedAt); err != nil {
			return n, fmt.Errorf("upis bilješke: %w", err)
		}
		if _, err := r.rec.Record(ctx, tx, EntityBiljeske, b.ID.String(), b); err != nil {
			return n, err
		}
		n++
	}
	return n, tx.Commit()
}
