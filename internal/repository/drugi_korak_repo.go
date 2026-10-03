package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Drugi korak prijave izvana: prijave na čekanju, zapamćena računala i
// kodovi za prijavu. Sve stoji samo na ovom čvoru i ne ide u knjigu verzija
// — kao i sesije. Tokeni i kodovi dolaze ovamo već kao HMAC; repozitorij ih
// samo sprema i uspoređuje po jednakosti ključa.

// Vrste kodova za prijavu
const (
	KodRezervni   = "rezervni"   // osoba ih sama napravi na profilu, deset odjednom
	KodPrivremeni = "privremeni" // daje ga administrator, vrijedi 24 sata
)

// PrijavaNaCekanju je prijava kojoj je lozinka točna, a čeka PIN ili kod.
type PrijavaNaCekanju struct {
	ID               []byte // HMAC tokena iz kolačića
	UserID           uuid.UUID
	PinHash          []byte // nil = PIN nije poslan, vrijede samo kodovi
	Pokusaja         int
	PoslanoNa        string // maskirana adresa, za prikaz
	IPAddress        string
	UserAgent        string
	PosljednjeSlanje *time.Time
	ExpiresAt        time.Time
	CreatedAt        time.Time
	// Razlog je oznaka zašto PIN nije poslan (prazno kad jest), da je
	// stranica s upisom koda zna i nakon krivog unosa
	Razlog string
}

// ZapamcenoRacunalo je preglednik koji osobi preskače PIN (ne lozinku).
type ZapamcenoRacunalo struct {
	ID            string
	UserID        uuid.UUID
	TokenHash     []byte
	LozinkaOtisak []byte // HMAC sažetka lozinke u trenutku pamćenja
	UserAgent     string
	IPPrvi        string
	IPZadnji      string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	ExpiresAt     time.Time
}

// KodPrijave je rezervni ili privremeni kod, sačuvan samo kao HMAC.
type KodPrijave struct {
	ID        string
	UserID    uuid.UUID
	Vrsta     string // KodRezervni ili KodPrivremeni
	Oznaka    string // vidljivi dio koda (R3, P)
	KodHash   []byte
	Izdao     string // tko ga je izdao (za privremeni: administrator)
	CreatedAt time.Time
	ExpiresAt *time.Time
	UsedAt    *time.Time
}

// DrugiKorakRepository čita i piše tablice drugog koraka prijave.
type DrugiKorakRepository struct{ db *sql.DB }

// NewDrugiKorakRepository sastavlja repozitorij nad bazom čvora.
func NewDrugiKorakRepository(db *sql.DB) *DrugiKorakRepository {
	return &DrugiKorakRepository{db: db}
}

func vrijemeIli(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func nullVrijeme(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

// ---- prijave na čekanju ----

// ZapocniPrijavu sprema novu prijavu na čekanju i briše starije iste osobe:
// u svakom trenutku osoba ima najviše jednu, pa nova prijava ne daje svježih
// pet pokušaja uz staru.
func (r *DrugiKorakRepository) ZapocniPrijavu(ctx context.Context, p *PrijavaNaCekanju) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM prijave_na_cekanju WHERE user_id = ?`, p.UserID.String()); err != nil {
		return err
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO prijave_na_cekanju
		(id, user_id, pin_hash, pokusaja, poslano_na, ip_address, user_agent, posljednje_slanje, expires_at, created_at, razlog)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.UserID.String(), p.PinHash, p.Pokusaja, p.PoslanoNa, p.IPAddress, p.UserAgent,
		vrijemeIli(p.PosljednjeSlanje), p.ExpiresAt.UTC(), p.CreatedAt.UTC(), p.Razlog); err != nil {
		return fmt.Errorf("prijava na čekanju nije spremljena: %w", err)
	}
	return tx.Commit()
}

// Prijava vraća prijavu na čekanju po HMAC-u tokena; nil kad je nema.
// Istek ne provjerava: to radi servis, koji zna koje je vrijeme.
func (r *DrugiKorakRepository) Prijava(ctx context.Context, id []byte) (*PrijavaNaCekanju, error) {
	var p PrijavaNaCekanju
	var user string
	var slanje sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT id, user_id, pin_hash, pokusaja, poslano_na, ip_address, user_agent,
		posljednje_slanje, expires_at, created_at, razlog FROM prijave_na_cekanju WHERE id = ?`, id).
		Scan(&p.ID, &user, &p.PinHash, &p.Pokusaja, &p.PoslanoNa, &p.IPAddress, &p.UserAgent, &slanje, &p.ExpiresAt, &p.CreatedAt, &p.Razlog)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.UserID, _ = uuid.Parse(user)
	p.PosljednjeSlanje = nullVrijeme(slanje)
	if len(p.PinHash) == 0 {
		p.PinHash = nil
	}
	return &p, nil
}

// RezervirajPokusaj zauzme pokušaj prije usporedbe koda, jednom naredbom:
// od istodobnih unosa najviše ih najvise dobije pokušaj. Vraća redni broj
// pokušaja; false kad su pokušaji potrošeni ili prijave više nema.
func (r *DrugiKorakRepository) RezervirajPokusaj(ctx context.Context, id []byte, najvise int) (int, bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`UPDATE prijave_na_cekanju SET pokusaja = pokusaja + 1 WHERE id = ? AND pokusaja < ? RETURNING pokusaja`,
		id, najvise).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

// PostaviPIN upisuje novi PIN prijavi (ponovno slanje); stari prestaje
// vrijediti, a razlog neposlanog PIN-a se briše.
func (r *DrugiKorakRepository) PostaviPIN(ctx context.Context, id, pinHash []byte, poslanoNa string, kad time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE prijave_na_cekanju SET pin_hash = ?, poslano_na = ?, posljednje_slanje = ?, razlog = '' WHERE id = ?`,
		pinHash, poslanoNa, kad.UTC(), id)
	return err
}

// PostaviRazlog upisuje zašto PIN nije poslan (oznaka; prazno briše).
func (r *DrugiKorakRepository) PostaviRazlog(ctx context.Context, id []byte, razlog string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE prijave_na_cekanju SET razlog = ? WHERE id = ?`, razlog, id)
	return err
}

// ObrisiPrijavu briše prijavu na čekanju i javlja je li je bilo: od dva
// istodobna točna unosa samo jedan dobije true, pa se kod ne iskoristi dvaput.
func (r *DrugiKorakRepository) ObrisiPrijavu(ctx context.Context, id []byte) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM prijave_na_cekanju WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ObrisiPrijaveKorisnika briše sve prijave na čekanju jedne osobe.
func (r *DrugiKorakRepository) ObrisiPrijaveKorisnika(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM prijave_na_cekanju WHERE user_id = ?`, userID.String())
	return err
}

// ---- zapamćena računala ----

// SpremiRacunalo pamti preglednik za osobu; isti token iste osobe zamjenjuje
// stari redak (novi rok i otisak lozinke).
func (r *DrugiKorakRepository) SpremiRacunalo(ctx context.Context, x *ZapamcenoRacunalo) error {
	if x.ID == "" {
		x.ID = uuid.NewString()
	}
	if x.CreatedAt.IsZero() {
		x.CreatedAt = time.Now().UTC()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM zapamcena_racunala WHERE token_hash = ? AND user_id = ?`,
		x.TokenHash, x.UserID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO zapamcena_racunala
		(id, user_id, token_hash, lozinka_otisak, user_agent, ip_prvi, ip_zadnji, created_at, last_used_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.UserID.String(), x.TokenHash, x.LozinkaOtisak, x.UserAgent, x.IPPrvi, x.IPZadnji,
		x.CreatedAt.UTC(), vrijemeIli(x.LastUsedAt), x.ExpiresAt.UTC()); err != nil {
		return fmt.Errorf("računalo nije zapamćeno: %w", err)
	}
	return tx.Commit()
}

const racunaloStupci = `id, user_id, token_hash, lozinka_otisak, user_agent, ip_prvi, ip_zadnji, created_at, last_used_at, expires_at`

func skenirajRacunalo(s interface{ Scan(...any) error }) (*ZapamcenoRacunalo, error) {
	var x ZapamcenoRacunalo
	var user string
	var zadnje sql.NullTime
	if err := s.Scan(&x.ID, &user, &x.TokenHash, &x.LozinkaOtisak, &x.UserAgent, &x.IPPrvi, &x.IPZadnji,
		&x.CreatedAt, &zadnje, &x.ExpiresAt); err != nil {
		return nil, err
	}
	x.UserID, _ = uuid.Parse(user)
	x.LastUsedAt = nullVrijeme(zadnje)
	return &x, nil
}

// Racunalo vraća zapamćeno računalo po HMAC-u tokena i osobi; nil kad ga nema.
func (r *DrugiKorakRepository) Racunalo(ctx context.Context, tokenHash []byte, userID uuid.UUID) (*ZapamcenoRacunalo, error) {
	x, err := skenirajRacunalo(r.db.QueryRowContext(ctx, `SELECT `+racunaloStupci+`
		FROM zapamcena_racunala WHERE token_hash = ? AND user_id = ?`, tokenHash, userID.String()))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return x, err
}

// OznaciKoristenje bilježi zadnju upotrebu; rok se ne produljuje.
func (r *DrugiKorakRepository) OznaciKoristenje(ctx context.Context, id, ip string, kad time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE zapamcena_racunala SET last_used_at = ?, ip_zadnji = ? WHERE id = ?`,
		kad.UTC(), ip, id)
	return err
}

// Racunala vraća zapamćena računala osobe, najnovija prva.
func (r *DrugiKorakRepository) Racunala(ctx context.Context, userID uuid.UUID) ([]ZapamcenoRacunalo, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+racunaloStupci+`
		FROM zapamcena_racunala WHERE user_id = ? ORDER BY created_at DESC`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ZapamcenoRacunalo
	for rows.Next() {
		x, err := skenirajRacunalo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// ObrisiRacunalo zaboravlja jedno računalo osobe; false kad ga nema (ili
// je tuđe).
func (r *DrugiKorakRepository) ObrisiRacunalo(ctx context.Context, userID uuid.UUID, id string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM zapamcena_racunala WHERE id = ? AND user_id = ?`, id, userID.String())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ObrisiRacunaloPoTokenu zaboravlja računalo po tokenu preglednika.
func (r *DrugiKorakRepository) ObrisiRacunaloPoTokenu(ctx context.Context, userID uuid.UUID, tokenHash []byte) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM zapamcena_racunala WHERE token_hash = ? AND user_id = ?`, tokenHash, userID.String())
	return err
}

// ObrisiRacunalaKorisnika zaboravlja sva računala osobe i vraća koliko ih je bilo.
func (r *DrugiKorakRepository) ObrisiRacunalaKorisnika(ctx context.Context, userID uuid.UUID) (int, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM zapamcena_racunala WHERE user_id = ?`, userID.String())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ---- kodovi za prijavu ----

// ZamijeniRezervne briše sve rezervne kodove osobe (i iskorištene) i
// upisuje novi niz.
func (r *DrugiKorakRepository) ZamijeniRezervne(ctx context.Context, userID uuid.UUID, kodovi []KodPrijave) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM kodovi_prijave WHERE user_id = ? AND vrsta = ?`, userID.String(), KodRezervni); err != nil {
		return err
	}
	for i := range kodovi {
		k := &kodovi[i]
		k.UserID, k.Vrsta = userID, KodRezervni
		if err := upisiKod(ctx, tx, k); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DodajPrivremeni upisuje privremeni kod; neiskorišteni stariji privremeni
// iste osobe prestaju vrijediti, pa u svakom trenutku vrijedi samo zadnji.
func (r *DrugiKorakRepository) DodajPrivremeni(ctx context.Context, k *KodPrijave) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM kodovi_prijave WHERE user_id = ? AND vrsta = ?`, k.UserID.String(), KodPrivremeni); err != nil {
		return err
	}
	k.Vrsta = KodPrivremeni
	if err := upisiKod(ctx, tx, k); err != nil {
		return err
	}
	return tx.Commit()
}

func upisiKod(ctx context.Context, tx *sql.Tx, k *KodPrijave) error {
	if k.ID == "" {
		k.ID = uuid.NewString()
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO kodovi_prijave (id, user_id, vrsta, oznaka, kod_hash, izdao, created_at, expires_at, used_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		k.ID, k.UserID.String(), k.Vrsta, strings.ToUpper(k.Oznaka), k.KodHash, k.Izdao, k.CreatedAt.UTC(), vrijemeIli(k.ExpiresAt), vrijemeIli(k.UsedAt))
	if err != nil {
		return fmt.Errorf("kod za prijavu nije spremljen: %w", err)
	}
	return nil
}

// Kod vraća neiskorišteni kod osobe s tom oznakom (najnoviji); nil kad ga
// nema. Istek provjerava servis.
func (r *DrugiKorakRepository) Kod(ctx context.Context, userID uuid.UUID, oznaka string) (*KodPrijave, error) {
	var k KodPrijave
	var user string
	var istek, iskoristen sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT id, user_id, vrsta, oznaka, kod_hash, izdao, created_at, expires_at, used_at
		FROM kodovi_prijave WHERE user_id = ? AND oznaka = ? AND used_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, userID.String(), strings.ToUpper(oznaka)).
		Scan(&k.ID, &user, &k.Vrsta, &k.Oznaka, &k.KodHash, &k.Izdao, &k.CreatedAt, &istek, &iskoristen)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	k.UserID, _ = uuid.Parse(user)
	k.ExpiresAt, k.UsedAt = nullVrijeme(istek), nullVrijeme(iskoristen)
	return &k, nil
}

// IskoristiKod troši kod; false kad je već iskorišten (ili ga nema), pa od
// dva istodobna unosa prolazi samo jedan.
func (r *DrugiKorakRepository) IskoristiKod(ctx context.Context, id string, kad time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE kodovi_prijave SET used_at = ? WHERE id = ? AND used_at IS NULL`, kad.UTC(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ObrisiPrivremene briše privremene kodove osobe.
func (r *DrugiKorakRepository) ObrisiPrivremene(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM kodovi_prijave WHERE user_id = ? AND vrsta = ?`, userID.String(), KodPrivremeni)
	return err
}

// StanjeRezervnih vraća koliko je rezervnih kodova osobi ostalo i kad je
// niz napravljen; nil vrijeme kad ga nikad nije napravila.
func (r *DrugiKorakRepository) StanjeRezervnih(ctx context.Context, userID uuid.UUID) (int, *time.Time, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT used_at IS NULL, created_at FROM kodovi_prijave
		WHERE user_id = ? AND vrsta = ?`, userID.String(), KodRezervni)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	ostalo := 0
	var napravljen *time.Time
	for rows.Next() {
		var neiskoristen bool
		var kad time.Time
		if err := rows.Scan(&neiskoristen, &kad); err != nil {
			return 0, nil, err
		}
		if neiskoristen {
			ostalo++
		}
		if napravljen == nil || kad.After(*napravljen) {
			napravljen = &kad
		}
	}
	return ostalo, napravljen, rows.Err()
}

// ---- zajedničko ----

// AktivnihSAdresom broji druge aktivne osobe s istom adresom e-pošte (bez
// obzira na velika slova): PIN se na zajedničku adresu ne šalje.
func (r *DrugiKorakRepository) AktivnihSAdresom(ctx context.Context, adresa string, osim uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users
		WHERE lower(trim(email)) = lower(trim(?)) AND is_active = 1 AND id <> ?`, adresa, osim.String()).Scan(&n)
	return n, err
}

// OcistiIstekle briše istekle prijave na čekanju, istekla računala i
// privremene kodove kojima je rok prošao; rezervni kodovi ostaju dok ih
// osoba ne zamijeni novima.
func (r *DrugiKorakRepository) OcistiIstekle(ctx context.Context, sad time.Time) (int, error) {
	ukupno := 0
	for _, q := range []string{
		`DELETE FROM prijave_na_cekanju WHERE expires_at <= ?`,
		`DELETE FROM zapamcena_racunala WHERE expires_at <= ?`,
		`DELETE FROM kodovi_prijave WHERE vrsta = '` + KodPrivremeni + `' AND expires_at IS NOT NULL AND expires_at <= ?`,
	} {
		res, err := r.db.ExecContext(ctx, q, sad.UTC())
		if err != nil {
			return ukupno, err
		}
		n, _ := res.RowsAffected()
		ukupno += int(n)
	}
	return ukupno, nil
}
