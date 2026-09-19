package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// EntityVodocuvarski su listovi vodočuvarskog dnevnika u knjizi verzija
const EntityVodocuvarski = "vodocuvarski_listovi"

const vodocuvarskiColumns = `id, user_id, ime, sektor, area_id, datum, broj, od, do_, prilike, naredbe, opis, zapazanja, ocitanja, zadaci, parafe,
	predano_at, potvrdio_id, potvrdio, potvrdeno_at, cvor, created_at, updated_at`

const vodocuvarskiUpsert = `INSERT INTO vodocuvarski_listovi (` + vodocuvarskiColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id, ime = excluded.ime, sektor = excluded.sektor, area_id = excluded.area_id,
	datum = excluded.datum, broj = excluded.broj, od = excluded.od, do_ = excluded.do_, prilike = excluded.prilike, naredbe = excluded.naredbe,
	opis = excluded.opis, zapazanja = excluded.zapazanja, ocitanja = excluded.ocitanja, zadaci = excluded.zadaci, parafe = excluded.parafe, predano_at = excluded.predano_at,
	potvrdio_id = excluded.potvrdio_id, potvrdio = excluded.potvrdio, potvrdeno_at = excluded.potvrdeno_at, cvor = excluded.cvor, updated_at = excluded.updated_at`

func vodocuvarskiArgs(l *models.VodocuvarskiList) []any {
	parafe, _ := json.Marshal(l.Parafe)
	zadaci, _ := json.Marshal(l.Zadaci)
	var predano, potvrdeno any
	if l.PredanoAt != nil {
		predano = l.PredanoAt.UTC()
	}
	if l.PotvrdenoAt != nil {
		potvrdeno = l.PotvrdenoAt.UTC()
	}
	return []any{l.ID, l.UserID, l.Ime, l.Sektor, l.AreaID, l.Datum.In(models.Zagreb).Format("2006-01-02"), l.Broj, l.Od, l.Do, l.Prilike, l.Naredbe, l.Opis, l.Zapazanja, l.Ocitanja, string(zadaci), string(parafe),
		predano, l.PotvrdioID, l.Potvrdio, potvrdeno, l.Cvor, l.CreatedAt.UTC(), l.UpdatedAt.UTC()}
}

func scanVodocuvarski(sc interface{ Scan(...any) error }) (models.VodocuvarskiList, error) {
	var l models.VodocuvarskiList
	var datum, parafe, zadaci string
	var predano, potvrdeno sql.NullTime
	err := sc.Scan(&l.ID, &l.UserID, &l.Ime, &l.Sektor, &l.AreaID, &datum, &l.Broj, &l.Od, &l.Do, &l.Prilike, &l.Naredbe, &l.Opis, &l.Zapazanja, &l.Ocitanja, &zadaci, &parafe,
		&predano, &l.PotvrdioID, &l.Potvrdio, &potvrdeno, &l.Cvor, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return l, err
	}
	l.Datum, _ = time.ParseInLocation("2006-01-02", datum[:10], models.Zagreb)
	_ = json.Unmarshal([]byte(parafe), &l.Parafe)
	_ = json.Unmarshal([]byte(zadaci), &l.Zadaci)
	if predano.Valid {
		t := predano.Time
		l.PredanoAt = &t
	}
	if potvrdeno.Valid {
		t := potvrdeno.Time
		l.PotvrdenoAt = &t
	}
	return l, nil
}

// VodocuvarRepository čuva listove vodočuvarskog dnevnika
type VodocuvarRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewVodocuvarRepository(db *sql.DB, rec *ledger.Recorder) *VodocuvarRepository {
	return &VodocuvarRepository{db: db, rec: rec}
}

// Save upisuje list, s verzijom u knjizi
func (r *VodocuvarRepository) Save(ctx context.Context, l *models.VodocuvarskiList) error {
	now := time.Now().UTC()
	if l.ID == "" {
		l.ID = uuid.Must(uuid.NewV7()).String()
		l.CreatedAt = now
	}
	l.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, vodocuvarskiUpsert, vodocuvarskiArgs(l)...); err != nil {
		return fmt.Errorf("upis lista: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityVodocuvarski, l.ID, l); err != nil {
		return err
	}
	return tx.Commit()
}

// Get čita list; nil kad ga nema
func (r *VodocuvarRepository) Get(ctx context.Context, id string) (*models.VodocuvarskiList, error) {
	l, err := scanVodocuvarski(r.db.QueryRowContext(ctx, `SELECT `+vodocuvarskiColumns+` FROM vodocuvarski_listovi WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// FiltarListova sužava popis
type FiltarListova struct {
	UserID      string
	Sektor      string
	AreaID      int
	Godina      int
	CekaPotvrdu bool
	Limit       int
}

// List vraća listove, najnoviji prvo
func (r *VodocuvarRepository) List(ctx context.Context, f FiltarListova) ([]models.VodocuvarskiList, error) {
	where, args := []string{"1=1"}, []any{}
	if f.UserID != "" {
		where, args = append(where, "user_id = ?"), append(args, f.UserID)
	}
	if f.Sektor != "" {
		where, args = append(where, "sektor = ?"), append(args, f.Sektor)
	}
	if f.AreaID > 0 {
		where, args = append(where, "area_id = ?"), append(args, f.AreaID)
	}
	if f.Godina > 0 {
		where, args = append(where, "substr(datum, 1, 4) = ?"), append(args, fmt.Sprint(f.Godina))
	}
	if f.CekaPotvrdu {
		where = append(where, "predano_at IS NOT NULL AND potvrdeno_at IS NULL")
	}
	q := `SELECT ` + vodocuvarskiColumns + ` FROM vodocuvarski_listovi WHERE ` + strings.Join(where, " AND ") + ` ORDER BY datum DESC, broj DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.VodocuvarskiList
	for rows.Next() {
		l, err := scanVodocuvarski(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ZaDan vraća list vodočuvara za zadani dan; nil kad ga nema
func (r *VodocuvarRepository) ZaDan(ctx context.Context, userID string, dan time.Time) (*models.VodocuvarskiList, error) {
	l, err := scanVodocuvarski(r.db.QueryRowContext(ctx, `SELECT `+vodocuvarskiColumns+` FROM vodocuvarski_listovi WHERE user_id = ? AND datum = ?`, userID, dan.In(models.Zagreb).Format("2006-01-02")))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// SljedeciBroj daje sljedeći redni broj lista vodočuvara u godini
func (r *VodocuvarRepository) SljedeciBroj(ctx context.Context, userID string, godina int) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(broj), 0) + 1 FROM vodocuvarski_listovi WHERE user_id = ? AND substr(datum, 1, 4) = ?`, userID, fmt.Sprint(godina)).Scan(&n)
	return n, err
}

// Delete briše list (nacrt), sa spomenikom u knjizi
func (r *VodocuvarRepository) Delete(ctx context.Context, id string) error {
	l, err := r.Get(ctx, id)
	if err != nil || l == nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM vodocuvarski_listovi WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityVodocuvarski, id, l); err != nil {
		return err
	}
	return tx.Commit()
}

// OcitanjeDana je vodostaj koji je vodočuvar očitao tog dana
type OcitanjeDana struct {
	Postaja  string
	Kad      time.Time
	Vodostaj *int
}

// OcitanjaDana vraća vodostaje koje je korisnik upisao tog dana, po postajama
func (r *VodocuvarRepository) OcitanjaDana(ctx context.Context, userID string, dan time.Time) ([]OcitanjeDana, error) {
	od := time.Date(dan.In(models.Zagreb).Year(), dan.In(models.Zagreb).Month(), dan.In(models.Zagreb).Day(), 0, 0, 0, 0, models.Zagreb)
	rows, err := r.db.QueryContext(ctx, `SELECT COALESCE(s.name, ''), r.measured_at, r.level_cm FROM readings r LEFT JOIN stations s ON s.id = r.station_id
		WHERE r.user_id = ? AND r.measured_at >= ? AND r.measured_at < ? AND r.level_cm IS NOT NULL ORDER BY r.measured_at`, userID, od.UTC(), od.Add(24*time.Hour).UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OcitanjeDana
	for rows.Next() {
		var o OcitanjeDana
		var v sql.NullInt64
		if err := rows.Scan(&o.Postaja, &o.Kad, &v); err != nil {
			return nil, err
		}
		if v.Valid {
			n := int(v.Int64)
			o.Vodostaj = &n
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---- zadaci ----

// EntityZadaci su zadaci vodočuvarima u knjizi verzija
const EntityZadaci = "vodocuvarski_zadaci"

const zadatakUpsert = `INSERT INTO vodocuvarski_zadaci (id, user_id, sektor, area_id, tekst, zadao_id, zadao, zadano_at, za, status, obavljeno, obavljeno_at, list_id, cvor, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id, sektor = excluded.sektor, area_id = excluded.area_id, tekst = excluded.tekst,
	zadao_id = excluded.zadao_id, zadao = excluded.zadao, zadano_at = excluded.zadano_at, za = excluded.za, status = excluded.status, obavljeno = excluded.obavljeno,
	obavljeno_at = excluded.obavljeno_at, list_id = excluded.list_id, cvor = excluded.cvor, updated_at = excluded.updated_at`

func zadatakArgs(z *models.Zadatak) []any {
	var ob any
	if z.ObavljenoAt != nil {
		ob = z.ObavljenoAt.UTC()
	}
	status := z.Status
	if status == "" {
		status = models.ZadatakOtvoren
	}
	za := ""
	if !z.Za.IsZero() {
		za = z.Za.In(models.Zagreb).Format("2006-01-02")
	}
	return []any{z.ID, z.UserID, z.Sektor, z.AreaID, z.Tekst, z.ZadaoID, z.Zadao, z.ZadanoAt.UTC(), za, status, z.Obavljeno, ob, z.ListID, z.Cvor, z.UpdatedAt.UTC()}
}

const zadatakColumns = `id, user_id, sektor, area_id, tekst, zadao_id, zadao, zadano_at, za, status, obavljeno, obavljeno_at, list_id, cvor, updated_at`

func scanZadatak(sc interface{ Scan(...any) error }) (models.Zadatak, error) {
	var z models.Zadatak
	var ob sql.NullTime
	var za string
	err := sc.Scan(&z.ID, &z.UserID, &z.Sektor, &z.AreaID, &z.Tekst, &z.ZadaoID, &z.Zadao, &z.ZadanoAt, &za, &z.Status, &z.Obavljeno, &ob, &z.ListID, &z.Cvor, &z.UpdatedAt)
	if za != "" {
		z.Za, _ = time.ParseInLocation("2006-01-02", za, models.Zagreb)
	}
	if ob.Valid {
		t := ob.Time
		z.ObavljenoAt = &t
	}
	return z, err
}

// SaveZadatak upisuje zadatak, s verzijom u knjizi
func (r *VodocuvarRepository) SaveZadatak(ctx context.Context, z *models.Zadatak) error {
	if z.ID == "" {
		z.ID = uuid.Must(uuid.NewV7()).String()
	}
	if z.ZadanoAt.IsZero() {
		z.ZadanoAt = time.Now()
	}
	z.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, zadatakUpsert, zadatakArgs(z)...); err != nil {
		return fmt.Errorf("upis zadatka: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityZadaci, z.ID, z); err != nil {
		return err
	}
	return tx.Commit()
}

// GetZadatak čita zadatak; nil kad ga nema
func (r *VodocuvarRepository) GetZadatak(ctx context.Context, id string) (*models.Zadatak, error) {
	z, err := scanZadatak(r.db.QueryRowContext(ctx, `SELECT `+zadatakColumns+` FROM vodocuvarski_zadaci WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &z, nil
}

// OtvoreniZadaci vraća otvorene zadatke vodočuvara zadane do kraja dana
func (r *VodocuvarRepository) OtvoreniZadaci(ctx context.Context, userID string, doDana time.Time) ([]models.Zadatak, error) {
	d := doDana.In(models.Zagreb)
	dan := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, models.Zagreb)
	// planirani zadatak stoji na listu od planiranog dana; bez plana od dana zadavanja
	rows, err := r.db.QueryContext(ctx, `SELECT `+zadatakColumns+` FROM vodocuvarski_zadaci WHERE user_id = ? AND status = ?
		AND ((za <> '' AND za <= ?) OR (za = '' AND zadano_at < ?)) ORDER BY za, zadano_at`, userID, models.ZadatakOtvoren, dan.Format("2006-01-02"), dan.Add(24*time.Hour).UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Zadatak
	for rows.Next() {
		z, err := scanZadatak(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// ZadaciVodocuvara vraća sve zadatke vodočuvara, najnoviji prvo
func (r *VodocuvarRepository) ZadaciVodocuvara(ctx context.Context, userID string, limit int) ([]models.Zadatak, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+zadatakColumns+` FROM vodocuvarski_zadaci WHERE user_id = ? ORDER BY zadano_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Zadatak
	for rows.Next() {
		z, err := scanZadatak(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// ZadaciUMjesecu vraća zadatke vodočuvara čiji dan pada u razdoblje, sve statuse
func (r *VodocuvarRepository) ZadaciURazdoblju(ctx context.Context, userID string, od, do time.Time) ([]models.Zadatak, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+zadatakColumns+` FROM vodocuvarski_zadaci WHERE user_id = ? ORDER BY za, zadano_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Zadatak
	for rows.Next() {
		z, err := scanZadatak(rows)
		if err != nil {
			return nil, err
		}
		if dan := z.Dan(); !dan.Before(od) && dan.Before(do) {
			out = append(out, z)
		}
	}
	return out, rows.Err()
}
