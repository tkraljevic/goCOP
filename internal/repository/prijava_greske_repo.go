package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Prijave grešaka i prijedlozi s ikone kukca idu zajedničkim kanalom:
// administrator ih čita, a osoba vidi odgovor, na bilo kojem računalu u mreži.
// Odgovori su zaseban entitet, da zadnja verzija prijave, koja nosi stanje, ne
// može pregaziti odgovor napisan na računalu s kojim se još nije razmijenila.
const (
	EntityPrijaveGresaka         = "prijave_gresaka"
	EntityPrijaveGresakaOdgovori = "prijave_gresaka_odgovori"
)

const prijavaGreskeColumns = `id, user_id, ime, uloga, vrsta, opis, stranica, naziv_stranice, izdanje, cvor, sustav, preglednik, zaslon, greske, stanje, created_at, updated_at`

const prijavaGreskeUpsert = `INSERT INTO prijave_gresaka (` + prijavaGreskeColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id, ime = excluded.ime, uloga = excluded.uloga, vrsta = excluded.vrsta, opis = excluded.opis,
	stranica = excluded.stranica, naziv_stranice = excluded.naziv_stranice, izdanje = excluded.izdanje, cvor = excluded.cvor, sustav = excluded.sustav,
	preglednik = excluded.preglednik, zaslon = excluded.zaslon, greske = excluded.greske, stanje = excluded.stanje,
	created_at = excluded.created_at, updated_at = excluded.updated_at`

const odgovorPrijaveColumns = `id, prijava_id, tekst, odgovorio, created_at`

const odgovorPrijaveUpsert = `INSERT INTO prijave_gresaka_odgovori (` + odgovorPrijaveColumns + `) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET prijava_id = excluded.prijava_id, tekst = excluded.tekst, odgovorio = excluded.odgovorio, created_at = excluded.created_at`

func odgovorPrijaveRedak(o *models.OdgovorNaPrijavu) []any {
	return []any{o.ID, o.PrijavaID, o.Tekst, o.Odgovorio, o.CreatedAt.UTC()}
}

func odgovorPrijaveArgs(o *models.OdgovorNaPrijavu) ([]any, error) {
	return odgovorPrijaveRedak(o), nil
}

func prijavaGreskeArgs(p *models.PrijavaGreske) ([]any, error) {
	greske, _ := json.Marshal(p.Greske) // popis teksta uvijek se da zapisati
	return []any{p.ID, p.UserID, p.Ime, p.Uloga, p.Vrsta, p.Opis, p.Stranica, p.NazivStranice, p.Izdanje, p.Cvor, p.Sustav, p.Preglednik, p.Zaslon,
		string(greske), p.Stanje, p.CreatedAt.UTC(), p.UpdatedAt.UTC()}, nil
}

func scanPrijavaGreske(row rowScanner) (*models.PrijavaGreske, error) {
	var p models.PrijavaGreske
	var greske string
	if err := row.Scan(&p.ID, &p.UserID, &p.Ime, &p.Uloga, &p.Vrsta, &p.Opis, &p.Stranica, &p.NazivStranice, &p.Izdanje, &p.Cvor, &p.Sustav, &p.Preglednik, &p.Zaslon,
		&greske, &p.Stanje, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(greske), &p.Greske) // pokvaren popis grešaka ne smije sakriti samu prijavu
	return &p, nil
}

// PrijavaGreskeRepository čuva prijave grešaka i prijedloge
type PrijavaGreskeRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

// NewPrijavaGreskeRepository otvara repozitorij prijava grešaka
func NewPrijavaGreskeRepository(db *sql.DB, rec *ledger.Recorder) *PrijavaGreskeRepository {
	return &PrijavaGreskeRepository{db: db, rec: rec}
}

// Save upisuje prijavu s verzijom u knjizi. Nova prijava dobije oznaku.
func (r *PrijavaGreskeRepository) Save(ctx context.Context, p *models.PrijavaGreske) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.upisiPrijavu(ctx, tx, p); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PrijavaGreskeRepository) upisiPrijavu(ctx context.Context, tx *sql.Tx, p *models.PrijavaGreske) error {
	now := time.Now().UTC()
	if p.ID == "" {
		p.ID, p.CreatedAt = uuid.Must(uuid.NewV7()).String(), now
	}
	p.UpdatedAt = now
	args, err := prijavaGreskeArgs(p)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, prijavaGreskeUpsert, args...); err != nil {
		return fmt.Errorf("upis prijave greške: %w", err)
	}
	_, err = r.rec.Record(ctx, tx, EntityPrijaveGresaka, p.ID, p)
	return err
}

// Odgovori upisuje stanje, kao novu verziju prijave, i odgovor, kao novi zapis.
// Prazan odgovor znači da se mijenja samo stanje, a isti tekst kao zadnji
// odgovor (ponovljeno slanje obrasca) ne upisuje se dvaput. Prijave koje nema
// vraća nil.
func (r *PrijavaGreskeRepository) Odgovori(ctx context.Context, id, stanje, odgovor, odgovorio string) (*models.PrijavaGreske, error) {
	p, err := r.Get(ctx, id)
	if err != nil || p == nil {
		return nil, err
	}
	o := noviOdgovor(p, odgovor, odgovorio)
	if p.Stanje == stanje && o == nil {
		return p, nil
	}
	if err := r.upisiStanjeIOdgovor(ctx, p, stanje, o); err != nil {
		return nil, err
	}
	return p, nil
}

// noviOdgovor sastavlja zapis odgovora; nil kad nema novog teksta
func noviOdgovor(p *models.PrijavaGreske, tekst, odgovorio string) *models.OdgovorNaPrijavu {
	if tekst == "" || (len(p.Odgovori) > 0 && p.Odgovori[len(p.Odgovori)-1].Tekst == tekst) {
		return nil
	}
	return &models.OdgovorNaPrijavu{ID: uuid.Must(uuid.NewV7()).String(), PrijavaID: p.ID, Tekst: tekst, Odgovorio: odgovorio, CreatedAt: time.Now().UTC()}
}

// upisiStanjeIOdgovor upisuje promjenu u jednoj transakciji i dopuni prijavu
func (r *PrijavaGreskeRepository) upisiStanjeIOdgovor(ctx context.Context, p *models.PrijavaGreske, stanje string, o *models.OdgovorNaPrijavu) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if p.Stanje != stanje {
		p.Stanje = stanje
		if err := r.upisiPrijavu(ctx, tx, p); err != nil {
			return err
		}
	}
	if o != nil {
		if err := r.upisiOdgovor(ctx, tx, o); err != nil {
			return err
		}
		p.Odgovori = append(p.Odgovori, *o)
	}
	return tx.Commit()
}

func (r *PrijavaGreskeRepository) upisiOdgovor(ctx context.Context, tx *sql.Tx, o *models.OdgovorNaPrijavu) error {
	if _, err := tx.ExecContext(ctx, odgovorPrijaveUpsert, odgovorPrijaveRedak(o)...); err != nil {
		return fmt.Errorf("upis odgovora na prijavu: %w", err)
	}
	_, err := r.rec.Record(ctx, tx, EntityPrijaveGresakaOdgovori, o.ID, o)
	return err
}

// Get čita jednu prijavu s odgovorima; prijave koje nema vraća nil
func (r *PrijavaGreskeRepository) Get(ctx context.Context, id string) (*models.PrijavaGreske, error) {
	p, err := scanPrijavaGreske(r.db.QueryRowContext(ctx, `SELECT `+prijavaGreskeColumns+` FROM prijave_gresaka WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	odgovori, err := r.citajOdgovore(ctx, `SELECT `+odgovorPrijaveColumns+` FROM prijave_gresaka_odgovori WHERE prijava_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	p.Odgovori = odgovori
	return p, nil
}

func (r *PrijavaGreskeRepository) citajOdgovore(ctx context.Context, upit string, args ...any) ([]models.OdgovorNaPrijavu, error) {
	rows, err := r.db.QueryContext(ctx, upit, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var odgovori []models.OdgovorNaPrijavu
	for rows.Next() {
		var o models.OdgovorNaPrijavu
		if err := rows.Scan(&o.ID, &o.PrijavaID, &o.Tekst, &o.Odgovorio, &o.CreatedAt); err != nil {
			return nil, err
		}
		odgovori = append(odgovori, o)
	}
	return odgovori, rows.Err()
}

// IstaNedavna traži istu prijavu iste osobe poslanu nakon zadanog trenutka:
// kad se veza prekine, osoba ne zna je li prijava stigla pa je šalje ponovno.
func (r *PrijavaGreskeRepository) IstaNedavna(ctx context.Context, userID, vrsta, opis, stranica string, od time.Time) (*models.PrijavaGreske, error) {
	prijave, err := r.citaj(ctx, `SELECT `+prijavaGreskeColumns+` FROM prijave_gresaka WHERE user_id = ? AND vrsta = ? AND opis = ? AND stranica = ? ORDER BY id DESC LIMIT 1`, userID, vrsta, opis, stranica)
	if err != nil || len(prijave) == 0 || prijave[0].CreatedAt.Before(od) {
		return nil, err
	}
	return &prijave[0], nil
}

// Popis daje prijave, najnovije prve; prazno stanje znači sve, a uz nove idu i
// prijave u stanju koje ovaj program ne zna
func (r *PrijavaGreskeRepository) Popis(ctx context.Context, stanje string) ([]models.PrijavaGreske, error) {
	switch stanje {
	case "":
		return r.citaj(ctx, `SELECT `+prijavaGreskeColumns+` FROM prijave_gresaka ORDER BY id DESC`)
	case models.StanjePrijaveNova:
		return r.citaj(ctx, `SELECT `+prijavaGreskeColumns+` FROM prijave_gresaka WHERE stanje NOT IN (?, ?) ORDER BY id DESC`, models.StanjePrijaveURadu, models.StanjePrijaveRijeseno)
	}
	return r.citaj(ctx, `SELECT `+prijavaGreskeColumns+` FROM prijave_gresaka WHERE stanje = ? ORDER BY id DESC`, stanje)
}

// PoKorisniku daje prijave jedne osobe s odgovorima, najnovije prve
func (r *PrijavaGreskeRepository) PoKorisniku(ctx context.Context, userID string, najvise int) ([]models.PrijavaGreske, error) {
	prijave, err := r.citaj(ctx, `SELECT `+prijavaGreskeColumns+` FROM prijave_gresaka WHERE user_id = ? ORDER BY id DESC LIMIT ?`, userID, najvise)
	if err != nil || len(prijave) == 0 {
		return prijave, err
	}
	odgovori, err := r.citajOdgovore(ctx, `SELECT o.id, o.prijava_id, o.tekst, o.odgovorio, o.created_at FROM prijave_gresaka_odgovori o
		JOIN prijave_gresaka p ON p.id = o.prijava_id WHERE p.user_id = ? ORDER BY o.id`, userID)
	if err != nil {
		return nil, err
	}
	poPrijavi := make(map[string][]models.OdgovorNaPrijavu, len(odgovori))
	for _, o := range odgovori {
		poPrijavi[o.PrijavaID] = append(poPrijavi[o.PrijavaID], o)
	}
	for i := range prijave {
		prijave[i].Odgovori = poPrijavi[prijave[i].ID]
	}
	return prijave, nil
}

func (r *PrijavaGreskeRepository) citaj(ctx context.Context, upit string, args ...any) ([]models.PrijavaGreske, error) {
	rows, err := r.db.QueryContext(ctx, upit, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var prijave []models.PrijavaGreske
	for rows.Next() {
		p, err := scanPrijavaGreske(rows)
		if err != nil {
			return nil, err
		}
		prijave = append(prijave, *p)
	}
	return prijave, rows.Err()
}

// BrojPoStanju broji prijave po stanju; nepoznato stanje broji se kao nova
func (r *PrijavaGreskeRepository) BrojPoStanju(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT stanje, COUNT(*) FROM prijave_gresaka GROUP BY stanje`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	broj := map[string]int{}
	for rows.Next() {
		var stanje string
		var n int
		if err := rows.Scan(&stanje, &n); err != nil {
			return nil, err
		}
		broj[models.StanjeZaPrikaz(stanje)] += n
	}
	return broj, rows.Err()
}

// RedniBroj je redni broj prijave među prijavama koje ovo računalo ima, po
// vremenu nastanka
func (r *PrijavaGreskeRepository) RedniBroj(ctx context.Context, id string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM prijave_gresaka WHERE id <= ?`, id).Scan(&n)
	return n, err
}

// ZadnjaVremena daje vremena zadnjih n prijava osobe, za ogradu broja prijava
func (r *PrijavaGreskeRepository) ZadnjaVremena(ctx context.Context, userID string, n int) ([]time.Time, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT created_at FROM prijave_gresaka WHERE user_id = ? ORDER BY id DESC LIMIT ?`, userID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vremena []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		vremena = append(vremena, t)
	}
	return vremena, rows.Err()
}
