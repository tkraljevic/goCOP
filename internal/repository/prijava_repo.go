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

// EntityPrijave su prijave i obavijesti s terena u knjizi verzija
const EntityPrijave = "prijave"

// EntityPrijaveIzvornici su potpisani PDF-ovi prijava; nose slike, pa putuju
// umjesto izvornih fotografija
const EntityPrijaveIzvornici = "prijave_izvornici"

const prijavaColumns = `id, user_id, ime, sektor, area_id, broj, godina, vrsta, naslov, opis, datum, status, podaci, objavljeno_at, created_at, updated_at`

const prijavaUpsert = `INSERT INTO prijave (` + prijavaColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id, ime = excluded.ime, sektor = excluded.sektor, area_id = excluded.area_id,
	broj = excluded.broj, godina = excluded.godina, vrsta = excluded.vrsta, naslov = excluded.naslov, opis = excluded.opis, datum = excluded.datum,
	status = excluded.status, podaci = excluded.podaci, objavljeno_at = excluded.objavljeno_at, updated_at = excluded.updated_at`

const prijavaIzvornikUpsert = `INSERT INTO prijave_izvornici (prijava_id, pdf, sazetak, updated_at) VALUES (?, ?, ?, ?)
	ON CONFLICT(prijava_id) DO UPDATE SET pdf = excluded.pdf, sazetak = excluded.sazetak, updated_at = excluded.updated_at`

// podaciPrijave su polja koja se ne pretražuju, spremljena kao JSON
type podaciPrijave struct {
	VodotokCode  string                `json:"vodotok_code,omitempty"`
	Vodotok      string                `json:"vodotok,omitempty"`
	DionicaCode  string                `json:"dionica_code,omitempty"`
	ObjektID     string                `json:"objekt_id,omitempty"`
	Objekt       string                `json:"objekt,omitempty"`
	Latitude     *float64              `json:"latitude,omitempty"`
	Longitude    *float64              `json:"longitude,omitempty"`
	Stacionaza   string                `json:"stacionaza,omitempty"`
	Slike        []models.SlikaPrijave `json:"slike,omitempty"`
	ListID       string                `json:"list_id,omitempty"`
	ListBroj     int                   `json:"list_broj,omitempty"`
	ArhiviraoID  string                `json:"arhivirao_id,omitempty"`
	Arhivirao    string                `json:"arhivirao,omitempty"`
	ArhiviranoAt *time.Time            `json:"arhivirano_at,omitempty"`
	Cvor         string                `json:"cvor,omitempty"`
}

func prijavaArgs(p *models.PrijavaSTerena) []any {
	pod, _ := json.Marshal(podaciPrijave{VodotokCode: p.VodotokCode, Vodotok: p.Vodotok, DionicaCode: p.DionicaCode, ObjektID: p.ObjektID, Objekt: p.Objekt,
		Latitude: p.Latitude, Longitude: p.Longitude, Stacionaza: p.Stacionaza, Slike: p.Slike, ListID: p.ListID, ListBroj: p.ListBroj,
		ArhiviraoID: p.ArhiviraoID, Arhivirao: p.Arhivirao, ArhiviranoAt: p.ArhiviranoAt, Cvor: p.Cvor})
	var objavljeno any
	if p.ObjavljenoAt != nil {
		objavljeno = p.ObjavljenoAt.UTC()
	}
	return []any{p.ID, p.UserID, p.Ime, p.Sektor, p.AreaID, p.Broj, p.Godina, p.Vrsta, p.Naslov, p.Opis, p.Datum.In(models.Zagreb).Format("2006-01-02"),
		p.Status, string(pod), objavljeno, p.CreatedAt.UTC(), p.UpdatedAt.UTC()}
}

func scanPrijava(row rowScanner) (*models.PrijavaSTerena, error) {
	var p models.PrijavaSTerena
	var datum, pod string
	var objavljeno sql.NullTime
	if err := row.Scan(&p.ID, &p.UserID, &p.Ime, &p.Sektor, &p.AreaID, &p.Broj, &p.Godina, &p.Vrsta, &p.Naslov, &p.Opis, &datum, &p.Status, &pod, &objavljeno, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.Datum, _ = time.ParseInLocation("2006-01-02", datum, models.Zagreb)
	if objavljeno.Valid {
		t := objavljeno.Time
		p.ObjavljenoAt = &t
	}
	var x podaciPrijave
	_ = json.Unmarshal([]byte(pod), &x)
	p.VodotokCode, p.Vodotok, p.DionicaCode, p.ObjektID, p.Objekt = x.VodotokCode, x.Vodotok, x.DionicaCode, x.ObjektID, x.Objekt
	p.Latitude, p.Longitude, p.Stacionaza, p.Slike = x.Latitude, x.Longitude, x.Stacionaza, x.Slike
	p.ListID, p.ListBroj, p.ArhiviraoID, p.Arhivirao, p.ArhiviranoAt, p.Cvor = x.ListID, x.ListBroj, x.ArhiviraoID, x.Arhivirao, x.ArhiviranoAt, x.Cvor
	return &p, nil
}

// PrijavaRepository čuva prijave s terena, njihove slike i izvornike
type PrijavaRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

// NewPrijavaRepository otvara repozitorij prijava
func NewPrijavaRepository(db *sql.DB, rec *ledger.Recorder) *PrijavaRepository {
	return &PrijavaRepository{db: db, rec: rec}
}

// Save sprema prijavu, s verzijom u knjizi
func (r *PrijavaRepository) Save(ctx context.Context, p *models.PrijavaSTerena) error {
	now := time.Now().UTC()
	if p.ID == "" {
		p.ID = uuid.Must(uuid.NewV7()).String()
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, prijavaUpsert, prijavaArgs(p)...); err != nil {
		return fmt.Errorf("upis prijave: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityPrijave, p.ID, p); err != nil {
		return err
	}
	return tx.Commit()
}

// Objavi sprema objavljenu prijavu i njezin potpisani izvornik u jednoj
// transakciji: ili oboje, ili ništa
func (r *PrijavaRepository) Objavi(ctx context.Context, p *models.PrijavaSTerena, pdf []byte, sazetak string) error {
	if len(pdf) < 8 || string(pdf[:4]) != "%PDF" {
		return fmt.Errorf("izvornik nije PDF")
	}
	now := time.Now().UTC()
	p.UpdatedAt = now
	iz := models.IzvornikLista{ListID: p.ID, PDF: pdf, Sazetak: sazetak, UpdatedAt: now}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE prijave SET broj = ?, godina = ?, status = ?, podaci = ?, objavljeno_at = ?, updated_at = ? WHERE id = ? AND objavljeno_at IS NULL`,
		p.Broj, p.Godina, p.Status, prijavaArgs(p)[12], p.ObjavljenoAt.UTC(), now, p.ID)
	if err != nil {
		return fmt.Errorf("objava prijave: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("prijava je već objavljena ili ne postoji")
	}
	if _, err := r.rec.Record(ctx, tx, EntityPrijave, p.ID, p); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, prijavaIzvornikUpsert, p.ID, pdf, sazetak, now); err != nil {
		return fmt.Errorf("upis izvornika: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityPrijaveIzvornici, p.ID, iz); err != nil {
		return err
	}
	return tx.Commit()
}

// Get čita prijavu; nil kad je nema
func (r *PrijavaRepository) Get(ctx context.Context, id string) (*models.PrijavaSTerena, error) {
	p, err := scanPrijava(r.db.QueryRowContext(ctx, `SELECT `+prijavaColumns+` FROM prijave WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// FiltarPrijava sužava popis
type FiltarPrijava struct {
	Sektor string
	AreaID int
	UserID string
	Status string
	Godina int
	Trazi  string
	Limit  int
}

// List vraća prijave, najnovije prvo
func (r *PrijavaRepository) List(ctx context.Context, f FiltarPrijava) ([]models.PrijavaSTerena, error) {
	q := `SELECT ` + prijavaColumns + ` FROM prijave WHERE 1 = 1`
	var args []any
	if f.Sektor != "" {
		q += ` AND sektor = ?`
		args = append(args, f.Sektor)
	}
	if f.AreaID > 0 {
		q += ` AND area_id = ?`
		args = append(args, f.AreaID)
	}
	if f.UserID != "" {
		q += ` AND user_id = ?`
		args = append(args, f.UserID)
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.Godina > 0 {
		q += ` AND substr(datum, 1, 4) = ?`
		args = append(args, fmt.Sprint(f.Godina))
	}
	if t := strings.TrimSpace(f.Trazi); t != "" {
		q += ` AND (naslov LIKE ? OR opis LIKE ? OR podaci LIKE ?)`
		like := "%" + t + "%"
		args = append(args, like, like, like)
	}
	q += ` ORDER BY datum DESC, created_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PrijavaSTerena
	for rows.Next() {
		p, err := scanPrijava(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// SljedeciBroj daje sljedeći redni broj prijave u godini po sektoru
func (r *PrijavaRepository) SljedeciBroj(ctx context.Context, sektor string, godina int) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(broj), 0) + 1 FROM prijave WHERE sektor = ? AND godina = ?`, sektor, godina).Scan(&n)
	return n, err
}

// Delete briše nacrt, sa spomenikom u knjizi; slike idu s njim
func (r *PrijavaRepository) Delete(ctx context.Context, id string) error {
	p, err := r.Get(ctx, id)
	if err != nil || p == nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{`DELETE FROM prijave_slike WHERE prijava_id = ?`, `DELETE FROM prijave WHERE id = ?`} {
		if _, err := tx.ExecContext(ctx, t, id); err != nil {
			return err
		}
	}
	if _, err := r.rec.Archive(ctx, tx, EntityPrijave, id, p); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- slike: lokalno, s rokom ----

// SaveSlika sprema smanjenu fotografiju uz prijavu, samo na ovom čvoru
func (r *PrijavaRepository) SaveSlika(ctx context.Context, id, prijavaID string, jpg []byte) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO prijave_slike (id, prijava_id, slika, created_at) VALUES (?, ?, ?, ?)`, id, prijavaID, jpg, time.Now().UTC())
	return err
}

// Slika čita jednu fotografiju; nil kad je nema (obrisana nakon roka ili na drugom čvoru)
func (r *PrijavaRepository) Slika(ctx context.Context, id string) ([]byte, error) {
	var b []byte
	err := r.db.QueryRowContext(ctx, `SELECT slika FROM prijave_slike WHERE id = ?`, id).Scan(&b)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

// DeleteSlika briše jednu fotografiju
func (r *PrijavaRepository) DeleteSlika(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM prijave_slike WHERE id = ?`, id)
	return err
}

// ObrisiStareSlike briše izvorne fotografije objavljenih prijava starijih
// od roka; PDF ih nosi dalje. Vraća koliko je obrisano.
func (r *PrijavaRepository) ObrisiStareSlike(ctx context.Context, prije time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM prijave_slike WHERE created_at < ? AND prijava_id IN (SELECT id FROM prijave WHERE objavljeno_at IS NOT NULL AND objavljeno_at < ?)`, prije.UTC(), prije.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Izvornik čita potpisani PDF prijave; nil kad ga nema
func (r *PrijavaRepository) Izvornik(ctx context.Context, id string) (*models.IzvornikLista, error) {
	iz := models.IzvornikLista{ListID: id}
	err := r.db.QueryRowContext(ctx, `SELECT pdf, sazetak, updated_at FROM prijave_izvornici WHERE prijava_id = ?`, id).Scan(&iz.PDF, &iz.Sazetak, &iz.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &iz, nil
}
