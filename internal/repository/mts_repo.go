package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Materijalno-tehnička sredstva u knjizi verzija. Vrste i skladišta su
// registar i putuju svima; promet i popisi su podatak sektora, ali idu
// istim putem, da se stanje vidi i na čvoru koji ga nije upisao.
const (
	EntityMtsVrste     = "mts_vrste"
	EntityMtsSkladista = "mts_skladista"
	EntityMtsPromet    = "mts_promet"
	EntityMtsPopisi    = "mts_popisi"
)

const vrstaUpsert = `INSERT INTO mts_vrste (id, grupa, redoslijed, naziv, jedinica, oblici, aktivna, napomena, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET grupa = excluded.grupa, redoslijed = excluded.redoslijed, naziv = excluded.naziv,
		jedinica = excluded.jedinica, oblici = excluded.oblici, aktivna = excluded.aktivna, napomena = excluded.napomena,
		updated_at = excluded.updated_at`

func vrstaArgs(v *models.VrstaSredstva) ([]any, error) {
	oblici, err := json.Marshal(v.Oblici)
	if err != nil {
		return nil, err
	}
	aktivna := 0
	if v.Aktivna {
		aktivna = 1
	}
	return []any{v.ID, v.Grupa, v.Redoslijed, v.Naziv, v.Jedinica, string(oblici), aktivna, v.Napomena, v.UpdatedAt.UTC()}, nil
}

const vrstaSelect = `SELECT id, grupa, redoslijed, naziv, jedinica, oblici, aktivna, napomena, updated_at FROM mts_vrste`

func scanVrsta(row interface{ Scan(...any) error }) (models.VrstaSredstva, error) {
	var v models.VrstaSredstva
	var oblici string
	var aktivna int
	if err := row.Scan(&v.ID, &v.Grupa, &v.Redoslijed, &v.Naziv, &v.Jedinica, &oblici, &aktivna, &v.Napomena, &v.UpdatedAt); err != nil {
		return v, err
	}
	v.Aktivna = aktivna != 0
	if oblici != "" {
		_ = json.Unmarshal([]byte(oblici), &v.Oblici)
	}
	return v, nil
}

const skladisteUpsert = `INSERT INTO mts_skladista (id, sektor, area_id, naziv, adresa, structure_id, contractor_id, centralno, aktivno, napomena, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET sektor = excluded.sektor, area_id = excluded.area_id, naziv = excluded.naziv, adresa = excluded.adresa,
		structure_id = excluded.structure_id, contractor_id = excluded.contractor_id, centralno = excluded.centralno,
		aktivno = excluded.aktivno, napomena = excluded.napomena, created_at = excluded.created_at, updated_at = excluded.updated_at`

func skladisteArgs(s *models.Skladiste) []any {
	c, a := 0, 0
	if s.Centralno {
		c = 1
	}
	if s.Aktivno {
		a = 1
	}
	return []any{s.ID, s.Sektor, s.AreaID, s.Naziv, s.Adresa, s.StructureID, s.ContractorID, c, a, s.Napomena, s.CreatedAt.UTC(), s.UpdatedAt.UTC()}
}

const skladisteColumns = `s.id, s.sektor, s.area_id, s.naziv, s.adresa, s.structure_id, s.contractor_id, s.centralno, s.aktivno, s.napomena,
	s.created_at, s.updated_at, COALESCE(a.name, ''), COALESCE(st.name, ''), COALESCE(c.name, '')`
const skladisteFrom = ` FROM mts_skladista s
	LEFT JOIN areas a ON a.id = s.area_id
	LEFT JOIN structures st ON st.id = s.structure_id
	LEFT JOIN contractors c ON c.id = s.contractor_id`

func scanSkladiste(row interface{ Scan(...any) error }) (models.Skladiste, error) {
	var s models.Skladiste
	var centralno, aktivno int
	err := row.Scan(&s.ID, &s.Sektor, &s.AreaID, &s.Naziv, &s.Adresa, &s.StructureID, &s.ContractorID, &centralno, &aktivno, &s.Napomena,
		&s.CreatedAt, &s.UpdatedAt, &s.AreaName, &s.StructureName, &s.ContractorName)
	s.Centralno, s.Aktivno = centralno != 0, aktivno != 0
	return s, err
}

const prometUpsert = `INSERT INTO mts_promet (id, datum, vrsta_id, oblik, kolicina, vrsta, sektor, skladiste_id, section_code, veza_id, journal_id,
	popis_id, nalozio, preuzeo, dokument, user_id, user_name, napomena, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET datum = excluded.datum, vrsta_id = excluded.vrsta_id, oblik = excluded.oblik, kolicina = excluded.kolicina,
		vrsta = excluded.vrsta, sektor = excluded.sektor, skladiste_id = excluded.skladiste_id, section_code = excluded.section_code, veza_id = excluded.veza_id,
		journal_id = excluded.journal_id, popis_id = excluded.popis_id, nalozio = excluded.nalozio, preuzeo = excluded.preuzeo,
		dokument = excluded.dokument, user_id = excluded.user_id, user_name = excluded.user_name, napomena = excluded.napomena,
		created_at = excluded.created_at, updated_at = excluded.updated_at`

func prometArgs(p *models.Promet) []any {
	return []any{p.ID, dayKey(p.Datum), p.VrstaID, p.Oblik, p.Kolicina, p.Vrsta, p.Sektor, p.SkladisteID, p.SectionCode, p.VezaID, p.JournalID,
		p.PopisID, p.Nalozio, p.Preuzeo, p.Dokument, p.UserID, p.UserName, p.Napomena, p.CreatedAt.UTC(), p.UpdatedAt.UTC()}
}

const prometColumns = `p.id, p.datum, p.vrsta_id, p.oblik, p.kolicina, p.vrsta, p.sektor, p.skladiste_id, p.section_code, p.veza_id, p.journal_id,
	p.popis_id, p.nalozio, p.preuzeo, p.dokument, p.user_id, p.user_name, p.napomena, p.created_at, p.updated_at,
	COALESCE(v.naziv, ''), COALESCE(v.jedinica, ''), COALESCE(s.naziv, '')`
const prometFrom = ` FROM mts_promet p LEFT JOIN mts_vrste v ON v.id = p.vrsta_id LEFT JOIN mts_skladista s ON s.id = p.skladiste_id`

func scanPromet(row interface{ Scan(...any) error }) (models.Promet, error) {
	var p models.Promet
	var datum string
	err := row.Scan(&p.ID, &datum, &p.VrstaID, &p.Oblik, &p.Kolicina, &p.Vrsta, &p.Sektor, &p.SkladisteID, &p.SectionCode, &p.VezaID, &p.JournalID,
		&p.PopisID, &p.Nalozio, &p.Preuzeo, &p.Dokument, &p.UserID, &p.UserName, &p.Napomena, &p.CreatedAt, &p.UpdatedAt,
		&p.VrstaNaziv, &p.Jedinica, &p.SkladisteNaziv)
	p.Datum = parseDay(datum)
	return p, err
}

const popisUpsert = `INSERT INTO mts_popisi (id, skladiste_id, sektor, dan, godina, stavke, izradio_id, izradio, izradeno_at, zakljuceno_at, napomena, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET skladiste_id = excluded.skladiste_id, sektor = excluded.sektor, dan = excluded.dan, godina = excluded.godina,
		stavke = excluded.stavke, izradio_id = excluded.izradio_id, izradio = excluded.izradio, izradeno_at = excluded.izradeno_at,
		zakljuceno_at = excluded.zakljuceno_at, napomena = excluded.napomena, created_at = excluded.created_at, updated_at = excluded.updated_at`

func popisArgs(p *models.Popis) ([]any, error) {
	stavke, err := json.Marshal(p.Stavke)
	if err != nil {
		return nil, err
	}
	var zakljuceno any
	if p.ZakljucenoAt != nil {
		zakljuceno = p.ZakljucenoAt.UTC()
	}
	return []any{p.ID, p.SkladisteID, p.Sektor, p.DanKey(), p.Godina, string(stavke), p.IzradioID, p.Izradio, p.IzradenoAt.UTC(),
		zakljuceno, p.Napomena, p.CreatedAt.UTC(), p.UpdatedAt.UTC()}, nil
}

const popisColumns = `p.id, p.skladiste_id, p.sektor, p.dan, p.godina, p.stavke, p.izradio_id, p.izradio, p.izradeno_at, p.zakljuceno_at,
	p.napomena, p.created_at, p.updated_at, COALESCE(s.naziv, ''), COALESCE(s.area_id, 0)`
const popisFrom = ` FROM mts_popisi p LEFT JOIN mts_skladista s ON s.id = p.skladiste_id`

func scanPopis(row interface{ Scan(...any) error }) (*models.Popis, error) {
	var p models.Popis
	var dan, stavke string
	var zakljuceno sql.NullTime
	if err := row.Scan(&p.ID, &p.SkladisteID, &p.Sektor, &dan, &p.Godina, &stavke, &p.IzradioID, &p.Izradio, &p.IzradenoAt, &zakljuceno,
		&p.Napomena, &p.CreatedAt, &p.UpdatedAt, &p.SkladisteNaziv, &p.AreaID); err != nil {
		return nil, err
	}
	p.Dan = parseDay(dan)
	if stavke != "" {
		if err := json.Unmarshal([]byte(stavke), &p.Stavke); err != nil {
			return nil, fmt.Errorf("stavke popisa %s: %w", p.ID, err)
		}
	}
	p.IzradenoAt = p.IzradenoAt.In(models.Zagreb)
	if zakljuceno.Valid {
		t := zakljuceno.Time.In(models.Zagreb)
		p.ZakljucenoAt = &t
	}
	return &p, nil
}

// MtsRepository vodi sredstva: katalog, skladišta, promet i popise
type MtsRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

func NewMtsRepository(db *sql.DB, rec *ledger.Recorder) *MtsRepository {
	return &MtsRepository{db: db, rec: rec}
}

// ---- vrste sredstava

// ListVrste vraća katalog redom popisa; sve ili samo aktivne
func (r *MtsRepository) ListVrste(ctx context.Context, sveUkljucivoUgasene bool) ([]models.VrstaSredstva, error) {
	q := vrstaSelect
	if !sveUkljucivoUgasene {
		q += ` WHERE aktivna = 1`
	}
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.VrstaSredstva
	for rows.Next() {
		v, err := scanVrsta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	models.SortirajVrste(out)
	return out, nil
}

// GetVrsta čita jednu vrstu; nil kad je nema
func (r *MtsRepository) GetVrsta(ctx context.Context, id string) (*models.VrstaSredstva, error) {
	v, err := scanVrsta(r.db.QueryRowContext(ctx, vrstaSelect+` WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// SaveVrsta upisuje vrstu i bilježi verziju
func (r *MtsRepository) SaveVrsta(ctx context.Context, v *models.VrstaSredstva) error {
	v.UpdatedAt = time.Now().UTC()
	args, err := vrstaArgs(v)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, vrstaUpsert, args...); err != nil {
		return fmt.Errorf("upis vrste sredstva: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityMtsVrste, v.ID, v); err != nil {
		return err
	}
	return tx.Commit()
}

// OsigurajKatalog puni prazan katalog propisanim popisom sredstava. Radi se
// pri pokretanju prvog čvora; svaki sljedeći katalog dobiva sinkronizacijom,
// pa se ovdje ništa ne prepisuje.
func (r *MtsRepository) OsigurajKatalog(ctx context.Context) error {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM mts_vrste`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, v := range models.KatalogSredstava() {
		kopija := v
		if err := r.SaveVrsta(ctx, &kopija); err != nil {
			return err
		}
	}
	return nil
}

// ---- skladišta

// ListSkladista vraća skladišta sektora (prazan sektor: sva), po području pa nazivu
func (r *MtsRepository) ListSkladista(ctx context.Context, sektor string, areaID int, iUgasena bool) ([]models.Skladiste, error) {
	q := `SELECT ` + skladisteColumns + skladisteFrom + ` WHERE 1=1`
	var args []any
	if sektor != "" {
		q += ` AND s.sektor = ?`
		args = append(args, sektor)
	}
	if areaID > 0 {
		q += ` AND s.area_id = ?`
		args = append(args, areaID)
	}
	if !iUgasena {
		q += ` AND s.aktivno = 1`
	}
	q += ` ORDER BY s.sektor, s.area_id, s.centralno DESC, s.naziv COLLATE NOCASE`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Skladiste
	for rows.Next() {
		s, err := scanSkladiste(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSkladiste čita jedno skladište; nil kad ga nema
func (r *MtsRepository) GetSkladiste(ctx context.Context, id string) (*models.Skladiste, error) {
	s, err := scanSkladiste(r.db.QueryRowContext(ctx, `SELECT `+skladisteColumns+skladisteFrom+` WHERE s.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveSkladiste upisuje skladište i bilježi verziju
func (r *MtsRepository) SaveSkladiste(ctx context.Context, s *models.Skladiste) error {
	now := time.Now().UTC()
	if s.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		s.ID = id.String()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, skladisteUpsert, skladisteArgs(s)...); err != nil {
		return fmt.Errorf("upis skladišta: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityMtsSkladista, s.ID, s); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- promet

// SavePromet upisuje retke prometa u jednom zahvatu i bilježi svaki
func (r *MtsRepository) SavePromet(ctx context.Context, redci []models.Promet) error {
	if len(redci) == 0 {
		return nil
	}
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := range redci {
		p := &redci[i]
		if p.ID == "" {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			p.ID = id.String()
		}
		if p.CreatedAt.IsZero() {
			p.CreatedAt = now
		}
		p.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, prometUpsert, prometArgs(p)...); err != nil {
			return fmt.Errorf("upis prometa: %w", err)
		}
		if _, err := r.rec.Record(ctx, tx, EntityMtsPromet, p.ID, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FiltarPrometa sužava knjigu prometa; prazna polja ne sužavaju
type FiltarPrometa struct {
	SkladisteID string
	Sektor      string
	VrstaID     string
	JournalID   string
	VezaID      string
	Od, Do      *time.Time
	Limit       int
}

// ListPromet vraća knjigu prometa, najnoviji prvi
func (r *MtsRepository) ListPromet(ctx context.Context, f FiltarPrometa) ([]models.Promet, error) {
	q := `SELECT ` + prometColumns + prometFrom + ` WHERE 1=1`
	var args []any
	if f.SkladisteID != "" {
		q += ` AND p.skladiste_id = ?`
		args = append(args, f.SkladisteID)
	}
	if f.Sektor != "" {
		q += ` AND p.sektor = ?`
		args = append(args, f.Sektor)
	}
	if f.VrstaID != "" {
		q += ` AND p.vrsta_id = ?`
		args = append(args, f.VrstaID)
	}
	if f.JournalID != "" {
		q += ` AND p.journal_id = ?`
		args = append(args, f.JournalID)
	}
	if f.VezaID != "" {
		q += ` AND p.veza_id = ?`
		args = append(args, f.VezaID)
	}
	if f.Od != nil {
		q += ` AND p.datum >= ?`
		args = append(args, dayKey(*f.Od))
	}
	if f.Do != nil {
		q += ` AND p.datum <= ?`
		args = append(args, dayKey(*f.Do))
	}
	q += ` ORDER BY p.datum DESC, p.created_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Promet
	for rows.Next() {
		p, err := scanPromet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Stanje zbraja promet po skladištima: koliko čega i u kojem obliku stoji.
// Prazno skladište i prazan sektor znače sva skladišta. Datum sužava zbroj
// na stanje toga dana, kako ga knjiga pokazuje.
func (r *MtsRepository) Stanje(ctx context.Context, skladisteID, sektor string, naDan *time.Time) ([]models.Stanje, error) {
	q := `SELECT p.skladiste_id, p.vrsta_id, p.oblik, SUM(p.kolicina)` + prometFrom + ` WHERE p.skladiste_id <> ''`
	var args []any
	if skladisteID != "" {
		q += ` AND p.skladiste_id = ?`
		args = append(args, skladisteID)
	}
	if sektor != "" {
		q += ` AND s.sektor = ?`
		args = append(args, sektor)
	}
	if naDan != nil {
		q += ` AND p.datum <= ?`
		args = append(args, dayKey(*naDan))
	}
	q += ` GROUP BY p.skladiste_id, p.vrsta_id, p.oblik`
	return r.zbroji(ctx, q, args, false)
}

// StanjeNaTerenu zbraja što je izdano a nije vraćeno ni ugrađeno, po
// dionicama; obrana i sektor sužavaju, prazno znači sve
func (r *MtsRepository) StanjeNaTerenu(ctx context.Context, journalID, sektor string) ([]models.Stanje, error) {
	q := `SELECT p.section_code, p.vrsta_id, p.oblik, SUM(p.kolicina)` + prometFrom + ` WHERE p.skladiste_id = ''`
	var args []any
	if journalID != "" {
		q += ` AND p.journal_id = ?`
		args = append(args, journalID)
	}
	if sektor != "" {
		q += ` AND p.sektor = ?`
		args = append(args, sektor)
	}
	q += ` GROUP BY p.section_code, p.vrsta_id, p.oblik`
	return r.zbroji(ctx, q, args, true)
}

func (r *MtsRepository) zbroji(ctx context.Context, q string, args []any, teren bool) ([]models.Stanje, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Stanje
	for rows.Next() {
		var s models.Stanje
		var mjesto string
		if err := rows.Scan(&mjesto, &s.VrstaID, &s.Oblik, &s.Kolicina); err != nil {
			return nil, err
		}
		if teren {
			s.SectionCode = mjesto
		} else {
			s.SkladisteID = mjesto
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- godišnji popis

// SavePopis upisuje popis i bilježi verziju
func (r *MtsRepository) SavePopis(ctx context.Context, p *models.Popis) error {
	now := time.Now().UTC()
	if p.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		p.ID = id.String()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	args, err := popisArgs(p)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, popisUpsert, args...); err != nil {
		return fmt.Errorf("upis popisa: %w", err)
	}
	if _, err := r.rec.Record(ctx, tx, EntityMtsPopisi, p.ID, p); err != nil {
		return err
	}
	return tx.Commit()
}

// GetPopis čita popis; nil kad ga nema
func (r *MtsRepository) GetPopis(ctx context.Context, id string) (*models.Popis, error) {
	p, err := scanPopis(r.db.QueryRowContext(ctx, `SELECT `+popisColumns+popisFrom+` WHERE p.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// PopisZaDan čita popis skladišta na dan; nil kad ga nema
func (r *MtsRepository) PopisZaDan(ctx context.Context, skladisteID string, dan time.Time) (*models.Popis, error) {
	p, err := scanPopis(r.db.QueryRowContext(ctx, `SELECT `+popisColumns+popisFrom+` WHERE p.skladiste_id = ? AND p.dan = ?`, skladisteID, dayKey(dan)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// ListPopisi vraća popise, najnoviji dan prvi; filtri su sektor, skladište i godina
func (r *MtsRepository) ListPopisi(ctx context.Context, sektor, skladisteID string, godina int) ([]models.Popis, error) {
	q := `SELECT ` + popisColumns + popisFrom + ` WHERE 1=1`
	var args []any
	if sektor != "" {
		q += ` AND p.sektor = ?`
		args = append(args, sektor)
	}
	if skladisteID != "" {
		q += ` AND p.skladiste_id = ?`
		args = append(args, skladisteID)
	}
	if godina > 0 {
		q += ` AND p.godina = ?`
		args = append(args, godina)
	}
	q += ` ORDER BY p.dan DESC, p.skladiste_id`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Popis
	for rows.Next() {
		p, err := scanPopis(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// BrojSkladista broji skladišta, za razdjelnicu
func (r *MtsRepository) BrojSkladista(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM mts_skladista WHERE aktivno = 1`).Scan(&n)
	return n, err
}

// UpotrebaVrste javlja koliko redaka prometa i koliko popisa s upisanom
// količinom pokazuje na vrstu — što se od toga našlo, vrsta se ne briše
func (r *MtsRepository) UpotrebaVrste(ctx context.Context, vrstaID string) (prometa, popisa int, err error) {
	if err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM mts_promet WHERE vrsta_id = ?`, vrstaID).Scan(&prometa); err != nil {
		return 0, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT stavke FROM mts_popisi WHERE stavke LIKE ?`, `%"vrsta_id":"`+vrstaID+`"%`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return 0, 0, err
		}
		var stavke []models.PopisnaStavka
		if json.Unmarshal([]byte(s), &stavke) != nil {
			continue
		}
		for _, st := range stavke {
			if st.VrstaID == vrstaID && (st.Utvrdjeno != 0 || st.Potrebno != 0 || st.Knjizno != 0) {
				popisa++
				break
			}
		}
	}
	return prometa, popisa, rows.Err()
}

// ArhivirajVrstu miče vrstu iz kataloga; u knjizi verzija ostaje arhivirana
func (r *MtsRepository) ArhivirajVrstu(ctx context.Context, v *models.VrstaSredstva) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mts_vrste WHERE id = ?`, v.ID); err != nil {
		return err
	}
	if _, err := r.rec.Archive(ctx, tx, EntityMtsVrste, v.ID, v); err != nil {
		return err
	}
	return tx.Commit()
}
