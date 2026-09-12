package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/obracun"
)

// Postavke obračuna sati — blagdani i koeficijenti — su podatak organizacije
// i putuju zajedničkim kanalom kao registri: svaki čvor računa isto.
const (
	EntityBlagdani     = "blagdani"
	EntityKoeficijenti = "koeficijenti"
)

// Koeficijent je jedan množitelj: mjesto rada i razred sata
type Koeficijent struct {
	ID     string  `json:"id"` // "URED/DRD"
	Mjesto string  `json:"mjesto"`
	Razred string  `json:"razred"`
	K      float64 `json:"k"`
}

// KoeficijentID slaže ključ iz mjesta i razreda
func KoeficijentID(mjesto obracun.Mjesto, r obracun.Razred) string {
	return string(mjesto) + "/" + string(r)
}

const blagdanUpsert = `INSERT INTO blagdani (id, naziv, vrsta, mjesec, dan, pomak, datum, od_godine, do_godine, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET naziv = excluded.naziv, vrsta = excluded.vrsta, mjesec = excluded.mjesec, dan = excluded.dan,
		pomak = excluded.pomak, datum = excluded.datum, od_godine = excluded.od_godine, do_godine = excluded.do_godine, updated_at = excluded.updated_at`

func blagdanArgs(p obracun.Pravilo, kad time.Time) []any {
	return []any{p.ID, p.Naziv, string(p.Vrsta), p.Mjesec, p.Dan, p.Pomak, p.Datum, p.OdGodine, p.DoGodine, kad}
}

const koeficijentUpsert = `INSERT INTO koeficijenti (id, mjesto, razred, k, updated_at) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET mjesto = excluded.mjesto, razred = excluded.razred, k = excluded.k, updated_at = excluded.updated_at`

type ObracunRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewObracunRepository(db *sql.DB, rec *ledger.Recorder) *ObracunRepository {
	return &ObracunRepository{db: db, rec: rec}
}

// Osiguraj puni prazne tablice zadanim: hrvatskim zakonom i koeficijentima
// obrasca IORS 2026. Radi se pri pokretanju prvog čvora; svaki sljedeći
// popis dobiva sinkronizacijom, pa se ovdje ništa ne prepisuje.
func (r *ObracunRepository) Osiguraj(ctx context.Context) error {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM blagdani`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		for _, p := range obracun.ZakonskiBlagdani() {
			if err := r.SaveBlagdan(ctx, p); err != nil {
				return err
			}
		}
	}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM koeficijenti`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		for mjesto, po := range obracun.IORS2026 {
			for razred, k := range po {
				if err := r.SaveKoeficijent(ctx, Koeficijent{ID: KoeficijentID(mjesto, razred), Mjesto: string(mjesto), Razred: string(razred), K: k}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Blagdani vraća pravila redom kojim padaju u godini: stalni po datumu,
// pomični po pomaku, pa jednokratni
func (r *ObracunRepository) Blagdani(ctx context.Context) (obracun.Pravila, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, naziv, vrsta, mjesec, dan, pomak, datum, od_godine, do_godine FROM blagdani
		ORDER BY CASE vrsta WHEN 'STALNI' THEN 0 WHEN 'USKRS' THEN 1 ELSE 2 END, mjesec, dan, pomak, datum`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out obracun.Pravila
	for rows.Next() {
		var p obracun.Pravilo
		var vrsta string
		if err := rows.Scan(&p.ID, &p.Naziv, &vrsta, &p.Mjesec, &p.Dan, &p.Pomak, &p.Datum, &p.OdGodine, &p.DoGodine); err != nil {
			return nil, err
		}
		p.Vrsta = obracun.VrstaPravila(vrsta)
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveBlagdan upisuje ili mijenja pravilo i bilježi verziju
func (r *ObracunRepository) SaveBlagdan(ctx context.Context, p obracun.Pravilo) error {
	if p.ID == "" || p.Naziv == "" {
		return fmt.Errorf("blagdan mora imati oznaku i naziv")
	}
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, blagdanUpsert, blagdanArgs(p, now)...); err != nil {
		return fmt.Errorf("upis blagdana: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityBlagdani, p.ID, p); err != nil {
		return err
	}
	return tx.Commit()
}

// MakniBlagdan miče pravilo; u knjizi ostaje arhivirano
func (r *ObracunRepository) MakniBlagdan(ctx context.Context, id string) error {
	ps, err := r.Blagdani(ctx)
	if err != nil {
		return err
	}
	var p *obracun.Pravilo
	for i := range ps {
		if ps[i].ID == id {
			p = &ps[i]
		}
	}
	if p == nil {
		return fmt.Errorf("blagdan nije pronađen")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM blagdani WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityBlagdani, id, p); err != nil {
		return err
	}
	return tx.Commit()
}

// Koeficijenti vraća množitelje kao tablicu; što u bazi nedostaje dopunjuje
// se iz IORS 2026, da obračun nikad ne množi nulom zato što redak fali
func (r *ObracunRepository) Koeficijenti(ctx context.Context) (obracun.Koeficijenti, error) {
	out := obracun.Koeficijenti{}
	for mjesto, po := range obracun.IORS2026 {
		out[mjesto] = map[obracun.Razred]float64{}
		for razred, k := range po {
			out[mjesto][razred] = k
		}
	}
	rows, err := r.db.QueryContext(ctx, `SELECT mjesto, razred, k FROM koeficijenti`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mjesto, razred string
		var k float64
		if err := rows.Scan(&mjesto, &razred, &k); err != nil {
			return nil, err
		}
		if out[obracun.Mjesto(mjesto)] == nil {
			out[obracun.Mjesto(mjesto)] = map[obracun.Razred]float64{}
		}
		out[obracun.Mjesto(mjesto)][obracun.Razred(razred)] = k
	}
	return out, rows.Err()
}

// SaveKoeficijent upisuje jedan množitelj i bilježi verziju
func (r *ObracunRepository) SaveKoeficijent(ctx context.Context, k Koeficijent) error {
	if k.K < 0 {
		return fmt.Errorf("koeficijent ne može biti negativan")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, koeficijentUpsert, k.ID, k.Mjesto, k.Razred, k.K, time.Now().UTC()); err != nil {
		return fmt.Errorf("upis koeficijenta: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityKoeficijenti, k.ID, k); err != nil {
		return err
	}
	return tx.Commit()
}
