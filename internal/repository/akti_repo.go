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

// Entiteti akata u knjizi verzija
const (
	EntityAkti       = "akti"
	EntityPrimatelji = "primatelji"
	EntitySprance    = "akti_sprance"
)

// AktiRepository čuva akte i registar primatelja. Sve ide knjigom verzija:
// akt ovjeren u jednom uredu moraju vidjeti svi, a registar primatelja se
// uređuje jednom, ne na svakom čvoru.
type AktiRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewAktiRepository(db *sql.DB, rec *ledger.Recorder) *AktiRepository {
	return &AktiRepository{db: db, rec: rec}
}

const aktUpsert = `INSERT INTO akti (id, sektor, area_id, broj, godina, radnja, stupanj, station_id, station_name, watercourse,
	vodostaj_cm, vodostaj_kad, tendencija, prognoza, uvod, zavrsno, poveznice, prekida_akt_id, izvan_snage, za_potpis, kvalificirani, rucno, dionice, vrijedi, napomena, potpisnik, primatelji,
	status, izradio_id, izradio, izradeno_at, ovjerio_id, ovjerio, ovjereno_at, ovjera_kod, u_zamjeni, potpis, kljuc_cvora, cvor, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET sektor = excluded.sektor, area_id = excluded.area_id, broj = excluded.broj, godina = excluded.godina,
		radnja = excluded.radnja, stupanj = excluded.stupanj, station_id = excluded.station_id, station_name = excluded.station_name,
		watercourse = excluded.watercourse, vodostaj_cm = excluded.vodostaj_cm, vodostaj_kad = excluded.vodostaj_kad,
		tendencija = excluded.tendencija, prognoza = excluded.prognoza, uvod = excluded.uvod, zavrsno = excluded.zavrsno, poveznice = excluded.poveznice, prekida_akt_id = excluded.prekida_akt_id, izvan_snage = excluded.izvan_snage, za_potpis = excluded.za_potpis, kvalificirani = excluded.kvalificirani, rucno = excluded.rucno, dionice = excluded.dionice, vrijedi = excluded.vrijedi,
		napomena = excluded.napomena, potpisnik = excluded.potpisnik, primatelji = excluded.primatelji, status = excluded.status,
		izradio_id = excluded.izradio_id, izradio = excluded.izradio, izradeno_at = excluded.izradeno_at,
		ovjerio_id = excluded.ovjerio_id, ovjerio = excluded.ovjerio, ovjereno_at = excluded.ovjereno_at,
		ovjera_kod = excluded.ovjera_kod, u_zamjeni = excluded.u_zamjeni, potpis = excluded.potpis, kljuc_cvora = excluded.kljuc_cvora, cvor = excluded.cvor, created_at = excluded.created_at, updated_at = excluded.updated_at`

const aktColumns = `id, sektor, area_id, broj, godina, radnja, stupanj, station_id, station_name, watercourse,
	vodostaj_cm, vodostaj_kad, tendencija, prognoza, uvod, zavrsno, poveznice, prekida_akt_id, izvan_snage, za_potpis, kvalificirani, rucno, dionice, vrijedi, napomena, potpisnik, primatelji,
	status, izradio_id, izradio, izradeno_at, ovjerio_id, ovjerio, ovjereno_at, ovjera_kod, u_zamjeni, potpis, kljuc_cvora, cvor, created_at, updated_at`

func aktArgs(a *models.Akt) []any {
	dionice, _ := json.Marshal(a.Dionice)
	primatelji, _ := json.Marshal(a.Primatelji)
	zaPotpis, _ := json.Marshal(a.ZaPotpis)
	kval, rucno := "", ""
	if a.Kvalificirani != nil {
		b, _ := json.Marshal(a.Kvalificirani)
		kval = string(b)
	}
	if a.Rucno != nil {
		b, _ := json.Marshal(a.Rucno)
		rucno = string(b)
	}
	var vodostajKad, ovjerenoAt any
	if !a.VodostajKad.IsZero() {
		vodostajKad = a.VodostajKad.UTC()
	}
	if a.OvjerenoAt != nil {
		ovjerenoAt = a.OvjerenoAt.UTC()
	}
	return []any{a.ID, a.Sektor, a.AreaID, a.Broj, a.Godina, a.Radnja, string(a.Stupanj), a.StationID, a.StationName, a.Watercourse,
		a.VodostajCm, vodostajKad, a.Tendencija, a.Prognoza, a.Uvod, a.Zavrsno, a.Poveznice, a.PrekidaAktID, a.IzvanSnage, string(zaPotpis), kval, rucno, string(dionice), a.Vrijedi.UTC(), a.Napomena, a.Potpisnik, string(primatelji),
		a.Status, a.IzradioID, a.Izradio, a.IzradenoAt.UTC(), a.OvjerioID, a.Ovjerio, ovjerenoAt, a.OvjeraKod, boolInt(a.UZamjeni), a.Potpis, a.KljucCvora, a.Cvor, a.CreatedAt.UTC(), a.UpdatedAt.UTC()}
}

func scanAkt(sc interface{ Scan(...any) error }) (models.Akt, error) {
	var a models.Akt
	var stupanj, dionice, primatelji, zaPotpis, kval, rucno string
	var vodostaj sql.NullInt64
	var vodostajKad, ovjerenoAt sql.NullTime
	var uZamjeni int
	err := sc.Scan(&a.ID, &a.Sektor, &a.AreaID, &a.Broj, &a.Godina, &a.Radnja, &stupanj, &a.StationID, &a.StationName, &a.Watercourse,
		&vodostaj, &vodostajKad, &a.Tendencija, &a.Prognoza, &a.Uvod, &a.Zavrsno, &a.Poveznice, &a.PrekidaAktID, &a.IzvanSnage, &zaPotpis, &kval, &rucno, &dionice, &a.Vrijedi, &a.Napomena, &a.Potpisnik, &primatelji,
		&a.Status, &a.IzradioID, &a.Izradio, &a.IzradenoAt, &a.OvjerioID, &a.Ovjerio, &ovjerenoAt, &a.OvjeraKod, &uZamjeni, &a.Potpis, &a.KljucCvora, &a.Cvor, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return a, err
	}
	a.UZamjeni = uZamjeni != 0
	a.Stupanj = models.DefensePhase(stupanj)
	if vodostaj.Valid {
		v := int(vodostaj.Int64)
		a.VodostajCm = &v
	}
	if vodostajKad.Valid {
		a.VodostajKad = vodostajKad.Time
	}
	if ovjerenoAt.Valid {
		t := ovjerenoAt.Time
		a.OvjerenoAt = &t
	}
	_ = json.Unmarshal([]byte(zaPotpis), &a.ZaPotpis)
	if kval != "" {
		a.Kvalificirani = &models.KvalificiraniPotpis{}
		_ = json.Unmarshal([]byte(kval), a.Kvalificirani)
	}
	if rucno != "" {
		a.Rucno = &models.RucniPotpis{}
		_ = json.Unmarshal([]byte(rucno), a.Rucno)
	}
	_ = json.Unmarshal([]byte(dionice), &a.Dionice)
	_ = json.Unmarshal([]byte(primatelji), &a.Primatelji)
	return a, nil
}

// SaveAkt upisuje akt, nov ili izmijenjen, s verzijom u knjizi
func (r *AktiRepository) SaveAkt(ctx context.Context, a *models.Akt) error {
	now := time.Now().UTC()
	if a.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		a.ID = id.String()
		a.CreatedAt = now
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, aktUpsert, aktArgs(a)...); err != nil {
		return fmt.Errorf("upis akta: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityAkti, a.ID, a); err != nil {
		return err
	}
	return tx.Commit()
}

// SljedeciBroj daje sljedeći redni broj akta u godini po sektoru
func (r *AktiRepository) SljedeciBroj(ctx context.Context, sektor string, godina int) (int, error) {
	var n sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT MAX(broj) FROM akti WHERE sektor = ? AND godina = ?`, sektor, godina).Scan(&n)
	if err != nil {
		return 0, err
	}
	return int(n.Int64) + 1, nil
}

// GetAkt čita jedan akt
func (r *AktiRepository) GetAkt(ctx context.Context, id string) (*models.Akt, error) {
	a, err := scanAkt(r.db.QueryRowContext(ctx, `SELECT `+aktColumns+` FROM akti WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// FiltarAkata sužava popis; prazno polje ne filtrira
type FiltarAkata struct {
	Sektor    string
	AreaID    int
	StationID string
	Stupanj   models.DefensePhase
	Radnja    string
	Status    string
	Godina    int
	Trazi     string // dio naziva postaje, šifre dionice ili teksta
	Limit     int
}

// ListAkti vraća akte po filtru, najnoviji prvo
func (r *AktiRepository) ListAkti(ctx context.Context, f FiltarAkata) ([]models.Akt, error) {
	where := []string{"1=1"}
	var args []any
	if f.Sektor != "" {
		where, args = append(where, "sektor = ?"), append(args, f.Sektor)
	}
	if f.AreaID > 0 {
		where, args = append(where, "area_id = ?"), append(args, f.AreaID)
	}
	if f.StationID != "" {
		where, args = append(where, "station_id = ?"), append(args, f.StationID)
	}
	if f.Stupanj != "" {
		where, args = append(where, "stupanj = ?"), append(args, string(f.Stupanj))
	}
	if f.Radnja != "" {
		where, args = append(where, "radnja = ?"), append(args, f.Radnja)
	}
	if f.Status != "" {
		where, args = append(where, "status = ?"), append(args, f.Status)
	}
	if f.Godina > 0 {
		where, args = append(where, "godina = ?"), append(args, f.Godina)
	}
	if t := strings.TrimSpace(f.Trazi); t != "" {
		like := "%" + t + "%"
		where = append(where, "(station_name LIKE ? OR dionice LIKE ? OR napomena LIKE ? OR ovjerio LIKE ? OR izradio LIKE ? OR ovjera_kod LIKE ?)")
		args = append(args, like, like, like, like, like, like)
	}
	q := `SELECT ` + aktColumns + ` FROM akti WHERE ` + strings.Join(where, " AND ") + ` ORDER BY vrijedi DESC, created_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Akt
	for rows.Next() {
		a, err := scanAkt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAkt briše nacrt; ovjeren akt se ne briše
func (r *AktiRepository) DeleteAkt(ctx context.Context, id string) error {
	a, err := r.GetAkt(ctx, id)
	if err != nil {
		return err
	}
	if a == nil {
		return nil
	}
	if a.Ovjeren() {
		return fmt.Errorf("ovjeren akt se ne briše")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM akti WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityAkti, id, a); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- registar primatelja ----

const primateljUpsert = `INSERT INTO primatelji (id, sektor, area_id, naziv, email, skupina, od_stupnja, redoslijed, aktivan, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET sektor = excluded.sektor, area_id = excluded.area_id, naziv = excluded.naziv, email = excluded.email,
		skupina = excluded.skupina, od_stupnja = excluded.od_stupnja, redoslijed = excluded.redoslijed, aktivan = excluded.aktivan,
		updated_at = excluded.updated_at`

func primateljArgs(p *models.Primatelj) []any {
	return []any{p.ID, p.Sektor, p.AreaID, p.Naziv, p.Email, p.Skupina, string(p.OdStupnja), p.Redoslijed, boolInt(p.Aktivan), p.UpdatedAt.UTC()}
}

// SavePrimatelj upisuje primatelja u registar
func (r *AktiRepository) SavePrimatelj(ctx context.Context, p *models.Primatelj) error {
	if p.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		p.ID = id.String()
	}
	p.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, primateljUpsert, primateljArgs(p)...); err != nil {
		return fmt.Errorf("upis primatelja: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityPrimatelji, p.ID, p); err != nil {
		return err
	}
	return tx.Commit()
}

// ListPrimatelji vraća primatelje sektora, po skupini i redoslijedu
func (r *AktiRepository) ListPrimatelji(ctx context.Context, sektor string) ([]models.Primatelj, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, sektor, area_id, naziv, email, skupina, od_stupnja, redoslijed, aktivan, updated_at
		FROM primatelji WHERE sektor = ? ORDER BY area_id, skupina, redoslijed, naziv`, sektor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Primatelj
	for rows.Next() {
		var p models.Primatelj
		var od string
		var aktivan int
		if err := rows.Scan(&p.ID, &p.Sektor, &p.AreaID, &p.Naziv, &p.Email, &p.Skupina, &od, &p.Redoslijed, &aktivan, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.OdStupnja, p.Aktivan = models.DefensePhase(od), aktivan != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPrimatelj čita jednog primatelja
func (r *AktiRepository) GetPrimatelj(ctx context.Context, id string) (*models.Primatelj, error) {
	var p models.Primatelj
	var od string
	var aktivan int
	err := r.db.QueryRowContext(ctx, `SELECT id, sektor, area_id, naziv, email, skupina, od_stupnja, redoslijed, aktivan, updated_at
		FROM primatelji WHERE id = ?`, id).Scan(&p.ID, &p.Sektor, &p.AreaID, &p.Naziv, &p.Email, &p.Skupina, &od, &p.Redoslijed, &aktivan, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.OdStupnja, p.Aktivan = models.DefensePhase(od), aktivan != 0
	return &p, nil
}

// DeletePrimatelj briše primatelja iz registra
func (r *AktiRepository) DeletePrimatelj(ctx context.Context, id string) error {
	p, err := r.GetPrimatelj(ctx, id)
	if err != nil || p == nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM primatelji WHERE id = ?`, id); err != nil {
		return err
	}
	p.Archived = true
	if _, err := r.rec.Archive(ctx, tx, EntityPrimatelji, id, p); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- špranca ----

const sprancaUpsert = `INSERT INTO akti_sprance (sektor, podaci, updated_at) VALUES (?, ?, ?)
	ON CONFLICT(sektor) DO UPDATE SET podaci = excluded.podaci, updated_at = excluded.updated_at`

func sprancaArgs(sp *models.Spranca) []any {
	b, _ := json.Marshal(sp)
	return []any{sp.Sektor, string(b), sp.UpdatedAt.UTC()}
}

// GetSpranca čita šprancu sektora; zadana kad je nitko nije uredio
func (r *AktiRepository) GetSpranca(ctx context.Context, sektor string) (models.Spranca, error) {
	var podaci string
	err := r.db.QueryRowContext(ctx, `SELECT podaci FROM akti_sprance WHERE sektor = ?`, sektor).Scan(&podaci)
	if err == sql.ErrNoRows {
		return models.ZadanaSpranca(sektor), nil
	}
	if err != nil {
		return models.ZadanaSpranca(sektor), err
	}
	sp := models.ZadanaSpranca(sektor)
	if err := json.Unmarshal([]byte(podaci), &sp); err != nil {
		return models.ZadanaSpranca(sektor), err
	}
	return sp, nil
}

// SaveSpranca upisuje šprancu sektora s verzijom u knjizi
func (r *AktiRepository) SaveSpranca(ctx context.Context, sp *models.Spranca) error {
	sp.UpdatedAt = time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, sprancaUpsert, sprancaArgs(sp)...); err != nil {
		return fmt.Errorf("upis špranče: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntitySprance, sp.Sektor, sp); err != nil {
		return err
	}
	return tx.Commit()
}

// UgovorneFirme su licencirane pravne osobe za obranu na branjenom
// području, iz registra firmi, s e-poštom
func (r *AktiRepository) UgovorneFirme(ctx context.Context, areaID int) ([]models.AktPrimatelj, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT c.name, c.email FROM contractors c
		JOIN contractor_assignments a ON a.contractor_id = c.id
		WHERE a.area_id = ? AND c.active = 1 ORDER BY c.name`, areaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AktPrimatelj
	for rows.Next() {
		var p models.AktPrimatelj
		if err := rows.Scan(&p.Naziv, &p.Email); err != nil {
			return nil, err
		}
		p.Skupina = models.SkupinaIspostava
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- izvornici ----

// EntityIzvornici su potpisani izvornici akata u knjizi verzija
const EntityIzvornici = "akti_izvornici"

// Izvornik je potpisani PDF akta
type Izvornik struct {
	AktID     string    `json:"akt_id"`
	PDF       []byte    `json:"pdf"`
	Sazetak   string    `json:"sazetak"`
	CreatedAt time.Time `json:"created_at"`
}

const izvornikUpsert = `INSERT INTO akti_izvornici (akt_id, pdf, sazetak, created_at) VALUES (?, ?, ?, ?)
	ON CONFLICT(akt_id) DO UPDATE SET pdf = excluded.pdf, sazetak = excluded.sazetak, created_at = excluded.created_at`

// SaveIzvornik sprema potpisani PDF uz akt, s verzijom u knjizi
func (r *AktiRepository) SaveIzvornik(ctx context.Context, iz *Izvornik) error {
	if iz.CreatedAt.IsZero() {
		iz.CreatedAt = time.Now().UTC()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, izvornikUpsert, iz.AktID, iz.PDF, iz.Sazetak, iz.CreatedAt.UTC()); err != nil {
		return fmt.Errorf("upis izvornika: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityIzvornici, iz.AktID, iz); err != nil {
		return err
	}
	return tx.Commit()
}

// GetIzvornik čita potpisani PDF akta; nil kad ga nema
func (r *AktiRepository) GetIzvornik(ctx context.Context, aktID string) (*Izvornik, error) {
	iz := Izvornik{AktID: aktID}
	err := r.db.QueryRowContext(ctx, `SELECT pdf, sazetak, created_at FROM akti_izvornici WHERE akt_id = ?`, aktID).Scan(&iz.PDF, &iz.Sazetak, &iz.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &iz, nil
}
