// Package sadrzaj je spremište nepromjenjivih sadržaja po otisku: PDF
// izvornici, fotografije, skenovi, slike potpisa. Živi u vlastitoj SQLite
// datoteci uz glavnu bazu (data/sadrzaj.db), bez knjige verzija, jer sadržaj
// nema verzija: isti bajtovi imaju isti SHA-256 otisak na svakom čvoru, a
// drukčiji bajtovi su drugi sadržaj.
//
// Knjiga verzija glavne baze nosi samo otisak, vrstu i veličinu; bajtovi
// putuju svojim putem, po dijelovima, koliko koji čvor prema pretplati treba.
// Zato glavna baza raste s brojem zapisa, a ne s njihovim megabajtima.
package sadrzaj

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// VelicinaDijela je veličina dijela za prijenos; dio se provjerava svojim
// otiskom pa se prekinut prijenos nastavlja od zadnjeg cijelog dijela
const VelicinaDijela = 1 << 20

// ErrNema javlja da čvor traženi sadržaj ne drži
var ErrNema = errors.New("sadržaj nije na ovom čvoru")

// ErrOtisak javlja da se primljeni bajtovi ne slažu s očekivanim otiskom
var ErrOtisak = errors.New("otisak sadržaja se ne slaže")

// Spremiste je otvorena datoteka spremišta
type Spremiste struct {
	db  *sql.DB
	put string
}

// Otisak računa SHA-256 sadržaja, hex malim slovima: to je ime sadržaja
func Otisak(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// probnih broji memorijska spremišta, da svako bude svoje: dva testa koja
// otvore spremište u memoriji ne smiju dijeliti istu bazu
var probnih atomic.Int64

// Otvori otvara spremište na putu i stvara tablice ako ih nema; prazan put
// daje spremište u memoriji, za probe
func Otvori(put string) (*Spremiste, error) {
	dsn := fmt.Sprintf("file:sadrzaj-proba-%d?mode=memory&cache=shared", probnih.Add(1))
	if put != "" {
		if dir := filepath.Dir(put); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
		dsn = put + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if put == "" {
		db.SetMaxOpenConns(1) // memorijska baza živi po vezi
	}
	s := &Spremiste{db: db, put: put}
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS sadrzaj (
			otisak TEXT PRIMARY KEY,
			vrsta TEXT NOT NULL,
			bajtova INTEGER NOT NULL,
			podaci BLOB NOT NULL,
			primljeno DATETIME NOT NULL,
			izvor TEXT NOT NULL DEFAULT 'ovdje',
			kanal TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS sadrzaj_veze (
			otisak TEXT NOT NULL,
			entitet TEXT NOT NULL,
			entitet_id TEXT NOT NULL,
			uloga TEXT NOT NULL,
			PRIMARY KEY (otisak, entitet, entitet_id, uloga)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sadrzaj_veze_zapis ON sadrzaj_veze(entitet, entitet_id)`,
		`CREATE TABLE IF NOT EXISTS sadrzaj_zeljen (
			otisak TEXT PRIMARY KEY,
			vrsta TEXT NOT NULL,
			bajtova INTEGER NOT NULL,
			trazeno DATETIME,
			razlog TEXT NOT NULL,
			kanal TEXT NOT NULL DEFAULT ''
		)`,
	} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, fmt.Errorf("spremište sadržaja: %w", err)
		}
	}
	// stupci uvedeni nakon što je spremište već stvoreno
	for _, c := range []struct{ tablica, stupac, opis string }{
		{"sadrzaj", "kanal", "TEXT NOT NULL DEFAULT ''"},
		{"sadrzaj_zeljen", "kanal", "TEXT NOT NULL DEFAULT ''"},
	} {
		ima, err := imaStupac(db, c.tablica, c.stupac)
		if err != nil {
			db.Close()
			return nil, err
		}
		if ima {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.tablica, c.stupac, c.opis)); err != nil {
			db.Close()
			return nil, fmt.Errorf("spremište sadržaja, stupac %s.%s: %w", c.tablica, c.stupac, err)
		}
	}
	return s, nil
}

// imaStupac javlja postoji li stupac u tablici
func imaStupac(db *sql.DB, tablica, stupac string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tablica))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var ime, tip string
		var notnull int
		var zadano any
		var pk int
		if err := rows.Scan(&cid, &ime, &tip, &notnull, &zadano, &pk); err != nil {
			return false, err
		}
		if ime == stupac {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Zatvori zatvara datoteku
func (s *Spremiste) Zatvori() error { return s.db.Close() }

// Put je putanja datoteke; prazno za memorijsko spremište
func (s *Spremiste) Put() string { return s.put }

// Veza kaže koji zapis sadržaj drži živim; Kanal je kanal tog zapisa, po
// kojem pretplata odlučuje dohvaća li se i koliko dugo ostaje
type Veza struct {
	Entitet   string
	EntitetID string
	Uloga     string // izvornik, slika, sken, potpis
	Kanal     string
}

// Upisi sprema sadržaj i vraća njegov otisak. Isti sadržaj drugi put je
// uspjeh bez upisa. Veza se upiše u istoj transakciji, da sadržaj ne bude
// siroče ni na tren.
func (s *Spremiste) Upisi(ctx context.Context, vrsta string, b []byte, izvor string, veze ...Veza) (string, error) {
	if len(b) == 0 {
		return "", errors.New("prazan sadržaj se ne sprema")
	}
	otisak := Otisak(b)
	return otisak, s.upisi(ctx, otisak, vrsta, b, izvor, veze)
}

// UpisiProvjereno sprema sadržaj primljen s drugog čvora ili iz paketa:
// bajtovi moraju dati očekivani otisak, inače se ništa ne upisuje
func (s *Spremiste) UpisiProvjereno(ctx context.Context, ocekivani, vrsta string, b []byte, izvor string, veze ...Veza) error {
	if Otisak(b) != ocekivani {
		return ErrOtisak
	}
	return s.upisi(ctx, ocekivani, vrsta, b, izvor, veze)
}

func (s *Spremiste) upisi(ctx context.Context, otisak, vrsta string, b []byte, izvor string, veze []Veza) error {
	if izvor == "" {
		izvor = "ovdje"
	}
	kanal := ""
	for _, v := range veze {
		if v.Kanal != "" {
			kanal = v.Kanal
			break
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if kanal == "" {
		_ = tx.QueryRowContext(ctx, `SELECT kanal FROM sadrzaj_zeljen WHERE otisak = ?`, otisak).Scan(&kanal)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sadrzaj (otisak, vrsta, bajtova, podaci, primljeno, izvor, kanal) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(otisak) DO NOTHING`, otisak, vrsta, len(b), b, time.Now().UTC(), izvor, kanal); err != nil {
		return fmt.Errorf("upis sadržaja: %w", err)
	}
	for _, v := range veze {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sadrzaj_veze (otisak, entitet, entitet_id, uloga) VALUES (?, ?, ?, ?)
			ON CONFLICT DO NOTHING`, otisak, v.Entitet, v.EntitetID, v.Uloga); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sadrzaj_zeljen WHERE otisak = ?`, otisak); err != nil {
		return err
	}
	return tx.Commit()
}

// Citaj vraća bajtove i vrstu sadržaja; ErrNema kad ga čvor ne drži
func (s *Spremiste) Citaj(ctx context.Context, otisak string) ([]byte, string, error) {
	var b []byte
	var vrsta string
	err := s.db.QueryRowContext(ctx, `SELECT podaci, vrsta FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&b, &vrsta)
	if err == sql.ErrNoRows {
		return nil, "", ErrNema
	}
	if err != nil {
		return nil, "", err
	}
	return b, vrsta, nil
}

// CitajSKanalom vraća bajtove, vrstu i kanal sadržaja; ErrNema kad ga nema
func (s *Spremiste) CitajSKanalom(ctx context.Context, otisak string) ([]byte, string, string, error) {
	var b []byte
	var vrsta, kanal string
	err := s.db.QueryRowContext(ctx, `SELECT podaci, vrsta, kanal FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&b, &vrsta, &kanal)
	if err == sql.ErrNoRows {
		return nil, "", "", ErrNema
	}
	if err != nil {
		return nil, "", "", err
	}
	return b, vrsta, kanal, nil
}

// Ima javlja drži li čvor sadržaj
func (s *Spremiste) Ima(ctx context.Context, otisak string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&n)
	return n > 0
}

// Vezi bilježi da zapis drži sadržaj; sadržaj ne mora još biti ovdje
func (s *Spremiste) Vezi(ctx context.Context, otisak string, v Veza) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sadrzaj_veze (otisak, entitet, entitet_id, uloga) VALUES (?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, otisak, v.Entitet, v.EntitetID, v.Uloga)
	return err
}

// OtisakPoUlozi vraća otisak sadržaja vezanog uz zapis u zadanoj ulozi;
// prazno kad takve veze nema. Uloga fotografije nosi njezinu oznaku
// ("slika:<id>"), pa se sadržaj nađe i bez znanja o otisku.
func (s *Spremiste) OtisakPoUlozi(ctx context.Context, entitet, uloga string) string {
	var otisak string
	_ = s.db.QueryRowContext(ctx, `SELECT otisak FROM sadrzaj_veze WHERE entitet = ? AND uloga = ? LIMIT 1`, entitet, uloga).Scan(&otisak)
	return otisak
}

// OdveziUlogu miče jednu vezu; sadržaj koji time ostane bez ijedne veze
// smije se ukloniti
func (s *Spremiste) OdveziUlogu(ctx context.Context, entitet, uloga string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sadrzaj_veze WHERE entitet = ? AND uloga = ?`, entitet, uloga)
	return err
}

// ObrisiKanal miče s računala sav sadržaj jednog kanala i njegove veze; radi
// se kad se kanal briše s čvora, pa sadržaj više nema tko tražiti
func (s *Spremiste) ObrisiKanal(ctx context.Context, kanal string) (int, int64, error) {
	if kanal == "" {
		return 0, 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT otisak, bajtova FROM sadrzaj WHERE kanal = ?`, kanal)
	if err != nil {
		return 0, 0, err
	}
	var otisci []string
	var bajtova int64
	for rows.Next() {
		var o string
		var n int
		if err := rows.Scan(&o, &n); err != nil {
			rows.Close()
			return 0, 0, err
		}
		otisci = append(otisci, o)
		bajtova += int64(n)
	}
	rows.Close()
	for _, o := range otisci {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sadrzaj_veze WHERE otisak = ?`, o); err != nil {
			return 0, 0, err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sadrzaj WHERE otisak = ?`, o); err != nil {
			return 0, 0, err
		}
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sadrzaj_zeljen WHERE kanal = ?`, kanal); err != nil {
		return 0, 0, err
	}
	return len(otisci), bajtova, nil
}

// Odvezi miče sve veze jednog zapisa (kad zapis nestane); sadržaj ostaje
// dok ga pospremanje ne prepozna kao siroče
func (s *Spremiste) Odvezi(ctx context.Context, entitet, entitetID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sadrzaj_veze WHERE entitet = ? AND entitet_id = ?`, entitet, entitetID)
	return err
}

// Zelja je sadržaj za koji čvor zna da postoji, a još ga nema
type Zelja struct {
	Otisak  string
	Vrsta   string
	Bajtova int
	Razlog  string
	Kanal   string
	Trazeno *time.Time
}

// Zeli bilježi sadržaj koji treba dohvatiti; ako ga čvor već drži, ništa
func (s *Spremiste) Zeli(ctx context.Context, otisak, vrsta string, bajtova int, razlog, kanal string) error {
	if s.Ima(ctx, otisak) {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sadrzaj_zeljen (otisak, vrsta, bajtova, razlog, kanal) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(otisak) DO UPDATE SET razlog = excluded.razlog, kanal = excluded.kanal`, otisak, vrsta, bajtova, razlog, kanal)
	return err
}

// Zeljeni vraća što čvor još treba dohvatiti, najstarije traženo prvo
func (s *Spremiste) Zeljeni(ctx context.Context, najvise int) ([]Zelja, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT otisak, vrsta, bajtova, razlog, kanal, trazeno FROM sadrzaj_zeljen
		ORDER BY trazeno IS NOT NULL, trazeno LIMIT ?`, najvise)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Zelja
	for rows.Next() {
		var z Zelja
		var t sql.NullTime
		if err := rows.Scan(&z.Otisak, &z.Vrsta, &z.Bajtova, &z.Razlog, &z.Kanal, &t); err != nil {
			return nil, err
		}
		if t.Valid {
			z.Trazeno = &t.Time
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// Trazeno bilježi pokušaj dohvata, da se redoslijed pokušaja vrti
func (s *Spremiste) Trazeno(ctx context.Context, otisak string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sadrzaj_zeljen SET trazeno = ? WHERE otisak = ?`, time.Now().UTC(), otisak)
	return err
}

// Primljen je sadržaj koji je stigao s drugog čvora; po kanalu i vremenu
// primitka pretplata odlučuje koliko dugo ostaje na ovom računalu
type Primljen struct {
	Otisak    string
	Kanal     string
	Bajtova   int
	Primljeno time.Time
}

// PrimljeniPrije vraća sadržaje primljene s drugih čvorova prije zadanog
// vremena; ono što je nastalo ovdje ne vraća, jer se ne otpušta
func (s *Spremiste) PrimljeniPrije(ctx context.Context, prije time.Time) ([]Primljen, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT otisak, kanal, bajtova, primljeno FROM sadrzaj WHERE izvor <> 'ovdje' AND primljeno < ?`, prije.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Primljen
	for rows.Next() {
		var p Primljen
		if err := rows.Scan(&p.Otisak, &p.Kanal, &p.Bajtova, &p.Primljeno); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Otpusti miče bajtove primljenog sadržaja s ovog računala, a veze ostaju:
// zapis i dalje zna svoj otisak i sadržaj se može opet dohvatiti. Što je
// nastalo ovdje ne otpušta se, jer bi ovaj čvor mogao biti jedini koji ga ima.
func (s *Spremiste) Otpusti(ctx context.Context, otisak string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sadrzaj WHERE otisak = ? AND izvor <> 'ovdje'`, otisak)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("sadržaj je nastao ovdje ili ga nema")
	}
	return nil
}

// Dio je jedan dio sadržaja za prijenos
type Dio struct {
	Redni   int    `json:"redni"`
	Otisak  string `json:"otisak"`
	Bajtova int    `json:"bajtova"`
}

// Dijelovi računa popis dijelova sadržaja: isti sadržaj daje iste dijelove
// svugdje, pa primatelj zna što već ima
func Dijelovi(b []byte) []Dio {
	var out []Dio
	for i := 0; i < len(b); i += VelicinaDijela {
		kraj := i + VelicinaDijela
		if kraj > len(b) {
			kraj = len(b)
		}
		out = append(out, Dio{Redni: len(out), Otisak: Otisak(b[i:kraj]), Bajtova: kraj - i})
	}
	return out
}

// Sirocad vraća otiske bez ijedne veze: nijedan zapis ih ne traži
func (s *Spremiste) Sirocad(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT otisak FROM sadrzaj s WHERE NOT EXISTS (SELECT 1 FROM sadrzaj_veze v WHERE v.otisak = s.otisak)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Ukloni briše bajtove sadržaja s ovog čvora, ali samo ako ga nijedan zapis
// ne drži: sadržaj koji zapis traži ostaje dok je zapisa
func (s *Spremiste) Ukloni(ctx context.Context, otisak string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sadrzaj WHERE otisak = ? AND NOT EXISTS (SELECT 1 FROM sadrzaj_veze v WHERE v.otisak = sadrzaj.otisak)`, otisak)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("sadržaj drži neki zapis ili ga nema")
	}
	return nil
}

// Stanje je pregled spremišta za administraciju
type Stanje struct {
	Sadrzaja int
	Bajtova  int64
	Zeljenih int
	Sirocadi int
}

// Stanje broji sadržaje, bajtove, željene i siročad
func (s *Spremiste) Stanje(ctx context.Context) (Stanje, error) {
	var st Stanje
	var bajtova sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*), sum(bajtova) FROM sadrzaj`).Scan(&st.Sadrzaja, &bajtova); err != nil {
		return st, err
	}
	st.Bajtova = bajtova.Int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sadrzaj_zeljen`).Scan(&st.Zeljenih); err != nil {
		return st, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sadrzaj s WHERE NOT EXISTS (SELECT 1 FROM sadrzaj_veze v WHERE v.otisak = s.otisak)`).Scan(&st.Sirocadi)
	return st, err
}
