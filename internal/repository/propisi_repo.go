package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/propisi"
)

// Propisi iz Narodnih novina putuju kao cijeli zapisi: jedan zapis po aktu,
// s cijelim tekstom podijeljenim na dijelove, i zaseban zapis za svaku
// izmjenu. Oba su u zajedničkom kanalu, pa stižu na svako računalo, i preko
// starijih posrednika, a brisanje kanala ih ne dira. Oznaka zapisa izvedena
// je iz izdanja NN (models.IzdanjeNN.ID), pa isti akt učitan na dva računala
// daje jedan zapis. „Dodaj izmjenu” ne mijenja zapis propisa, pa se dvije
// izmjene dodane na dva računala ne pregaze.
//
// Uz zapis se pri svakom upisu i primjeni slaže kazalo dijelova
// (propisi_dijelovi) za čitanje, pretragu i opseg. Kazalo ne putuje i odlazi
// sa zapisom kaskadom.

const (
	// EntityPropisi su propisi u knjizi verzija
	EntityPropisi = "propisi"
	// EntityPropisiIzmjene su izmjene propisa u knjizi verzija
	EntityPropisiIzmjene = "propisi_izmjene"
)

// NajveciPropis je najveći sadržaj propisa ili izmjene, u bajtovima JSON-a.
// Primatelj odbija verziju veću od 63 MiB, a JSON za slanje znakove <, > i
// & piše šest puta dulje, pa ni takav tekst od 8 MiB nije blizu granice.
// Zakon o vodama ima oko 0,6 MB.
const NajveciPropis = 8 << 20

// ErrPrevelikPropis javlja da propis ili izmjena ne bi prošli razmjenu
var ErrPrevelikPropis = errors.New("propis je veći od 8 MB i ne može se razmijeniti")

// vezaIzmjenePropisa: izmjena se oslanja na svoj propis; uklonjen propis
// odnosi i izmjene
var vezaIzmjenePropisa = vezaNaRoditelja{EntityPropisiIzmjene, "propis_id", EntityPropisi}

// PropisiRepository sprema i čita propise i njihove izmjene. Prava se ovdje
// ne provjeravaju (servis i web), ni oblik oznaka.
type PropisiRepository struct {
	db  *sql.DB
	rec *ledger.Recorder
}

// NewPropisiRepository vraća repozitorij propisa nad bazom i knjigom
func NewPropisiRepository(db *sql.DB, rec *ledger.Recorder) *PropisiRepository {
	return &PropisiRepository{db: db, rec: rec}
}

// SazetakPropisa je redak popisa propisa, bez teksta
type SazetakPropisa struct {
	ID, Adresa, Naziv, Skupina, ELI string
	NN                              models.IzdanjeNN
	Opseg                           propisi.Opseg
	Izmjena                         int
	CreatedAt, UpdatedAt            time.Time
}

// RedakKazala je jedan dio propisa ili izmjene iz kazala: oznaka, put i
// spljošten tekst. IzmjenaID je prazan za osnovni tekst propisa.
type RedakKazala struct {
	PropisID, IzmjenaID, Oznaka, Vrsta, Broj, Naslov, Roditelj, Tekst string
	Redni                                                             int
}

const propisUpsert = `INSERT INTO propisi (id, adresa, naziv, vrsta, skupina, nn_godina, nn_broj, nn_akt, eli, datum_objave,
		otisak, sadrzaj, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET adresa = excluded.adresa, naziv = excluded.naziv, vrsta = excluded.vrsta,
		skupina = excluded.skupina, nn_godina = excluded.nn_godina, nn_broj = excluded.nn_broj, nn_akt = excluded.nn_akt,
		eli = excluded.eli, datum_objave = excluded.datum_objave, otisak = excluded.otisak, sadrzaj = excluded.sadrzaj,
		created_at = excluded.created_at, updated_at = excluded.updated_at`

const izmjenaPropisaUpsert = `INSERT INTO propisi_izmjene (id, propis_id, nn_godina, nn_broj, nn_akt, datum_objave, stupa_na_snagu,
		otisak, sadrzaj, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET propis_id = excluded.propis_id, nn_godina = excluded.nn_godina, nn_broj = excluded.nn_broj,
		nn_akt = excluded.nn_akt, datum_objave = excluded.datum_objave, stupa_na_snagu = excluded.stupa_na_snagu,
		otisak = excluded.otisak, sadrzaj = excluded.sadrzaj, created_at = excluded.created_at, updated_at = excluded.updated_at`

// dvojnik oznake (čitač ga ne daje, ali zapis s drugog računala smije) ne
// ruši primjenu: u kazalu ostaje prvi
const kazaloUpis = `INSERT INTO propisi_dijelovi (id, propis_id, izmjena_id, oznaka, redni, vrsta, broj, naslov, roditelj, tekst, svedeno)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`

const stupciKazala = `propis_id, COALESCE(izmjena_id, ''), oznaka, vrsta, broj, naslov, roditelj, tekst, redni`

// upisiPropis stavlja propis na površinu i iznova slaže kazalo njegova
// osnovnog teksta. Isti upis rade lokalno spremanje i primjena s drugog
// računala, pa u njemu nema sata. Upis je upsert, nikad zamjena retka:
// brisanje retka kaskadom bi odnijelo izmjene i njihovo kazalo.
func upisiPropis(ctx context.Context, tx *sql.Tx, p *models.Propis) error {
	sadrzaj, _ := json.Marshal(p) // struktura bez mapa, kanala i funkcija: ne može pasti
	_, err := tx.ExecContext(ctx, propisUpsert, p.ID, p.Adresa, p.Naziv, p.Vrsta, p.Skupina, p.NN.Godina, p.NN.Broj, p.NN.Akt,
		p.ELI, p.DatumObjave, p.Otisak, string(sadrzaj), p.CreatedAt.UTC(), p.UpdatedAt.UTC())
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM propisi_dijelovi WHERE propis_id = ? AND izmjena_id IS NULL`, p.ID)
	}
	if err != nil {
		return err
	}
	return upisiKazalo(ctx, tx, p.ID, "", p.Dijelovi)
}

// upisiIzmjenuPropisa stavlja izmjenu na površinu i iznova slaže kazalo
// njezina teksta, kao upisiPropis
func upisiIzmjenuPropisa(ctx context.Context, tx *sql.Tx, iz *models.IzmjenaPropisa) error {
	sadrzaj, _ := json.Marshal(iz) // struktura bez mapa, kanala i funkcija: ne može pasti
	_, err := tx.ExecContext(ctx, izmjenaPropisaUpsert, iz.ID, iz.PropisID, iz.NN.Godina, iz.NN.Broj, iz.NN.Akt,
		iz.DatumObjave, iz.StupaNaSnagu, iz.Otisak, string(sadrzaj), iz.CreatedAt.UTC(), iz.UpdatedAt.UTC())
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM propisi_dijelovi WHERE izmjena_id = ?`, iz.ID)
	}
	if err != nil {
		return err
	}
	return upisiKazalo(ctx, tx, iz.PropisID, iz.ID, iz.Dijelovi)
}

// upisiKazalo upisuje dijelove propisa (izmjenaID prazan) ili izmjene u
// kazalo, sa spljoštenim i svedenim tekstom za pretragu
func upisiKazalo(ctx context.Context, tx *sql.Tx, propisID, izmjenaID string, d []models.DioPropisa) error {
	vlasnik, izmjena := propisID, any(nil)
	if izmjenaID != "" {
		vlasnik, izmjena = izmjenaID, izmjenaID
	}
	for i, dio := range d {
		tekst := propisi.TekstDijela(dio)
		if _, err := tx.ExecContext(ctx, kazaloUpis, vlasnik+"#"+dio.Oznaka, propisID, izmjena, dio.Oznaka, i+1, dio.Vrsta,
			dio.Broj, dio.Naslov, dio.Roditelj, tekst, propisi.Svedi(tekst)); err != nil {
			return err
		}
	}
	return nil
}

// otisakDijelova je sha256 JSON-a dijelova, heksadecimalno: sažimanje knjige
// po njemu čuva svaki različit tekst
func otisakDijelova(d []models.DioPropisa) string {
	b, _ := json.Marshal(d) // strukture bez mapa, kanala i funkcija: ne može pasti
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// vremenaZapisa su vrijeme nastanka (zadržava se kad je zadano) i vrijeme
// izmjene zapisa koji se upravo sprema
func vremenaZapisa(nastao time.Time) (created, updated time.Time) {
	sada := time.Now().UTC()
	if nastao.IsZero() {
		return sada, sada
	}
	return nastao, sada
}

// staneURazmjenu javlja ErrPrevelikPropis kad je sadržaj zapisa veći od
// NajveciPropis
func staneURazmjenu(zapis any) error {
	b, _ := json.Marshal(zapis) // struktura bez mapa, kanala i funkcija: ne može pasti
	if len(b) > NajveciPropis {
		return ErrPrevelikPropis
	}
	return nil
}

// uTransakciji izvodi f u jednoj transakciji i potvrđuje je kad f uspije
func (r *PropisiRepository) uTransakciji(ctx context.Context, f func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Spremi upisuje propis s verzijom u knjizi: novi ili novo čitanje istog
// akta. Uklonjen propis se tako vraća, a s njim i izmjene koje su u knjizi
// žive (zabiljeziSDjecom).
func (r *PropisiRepository) Spremi(ctx context.Context, p *models.Propis) error {
	p.Uklonjeno = nil
	p.Otisak = otisakDijelova(p.Dijelovi)
	p.CreatedAt, p.UpdatedAt = vremenaZapisa(p.CreatedAt)
	if err := staneURazmjenu(p); err != nil {
		return err
	}
	return r.uTransakciji(ctx, func(tx *sql.Tx) error {
		if err := upisiPropis(ctx, tx, p); err != nil {
			return err
		}
		return zabiljeziSDjecom(ctx, tx, r.rec, EntityPropisi, p.ID, p)
	})
}

// SpremiIzmjenu upisuje izmjenu propisa s verzijom u knjizi
func (r *PropisiRepository) SpremiIzmjenu(ctx context.Context, iz *models.IzmjenaPropisa) error {
	iz.Uklonjeno = nil
	iz.Otisak = otisakDijelova(iz.Dijelovi)
	iz.CreatedAt, iz.UpdatedAt = vremenaZapisa(iz.CreatedAt)
	if err := staneURazmjenu(iz); err != nil {
		return err
	}
	return r.uTransakciji(ctx, func(tx *sql.Tx) error {
		if err := upisiIzmjenuPropisa(ctx, tx, iz); err != nil {
			return err
		}
		_, err := r.rec.Record(ctx, tx, EntityPropisiIzmjene, iz.ID, iz)
		return err
	})
}

// Ukloni miče propis s površine i bilježi spomenik s razlogom, zajedno s
// izmjenama: onima na površini i onima koje su u knjizi žive, a na površini
// ih nema. Propis kojeg nema ne mijenja ništa.
func (r *PropisiRepository) Ukloni(ctx context.Context, id string, u models.Uklanjanje) error {
	return r.uTransakciji(ctx, func(tx *sql.Tx) error {
		p, err := jedanZapis[models.Propis](ctx, tx, `SELECT sadrzaj FROM propisi WHERE id = ?`, id)
		if err != nil || p == nil {
			return err
		}
		if err := r.spomeniciIzmjena(ctx, tx, id, &u); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM propisi WHERE id = ?`, id); err != nil {
			return err
		}
		p.Uklonjeno = &u
		_, err = r.rec.Archive(ctx, tx, EntityPropisi, id, p)
		return err
	})
}

// spomeniciIzmjena bilježi uklanjanje svih izmjena propisa: onih na
// površini, s razlogom, i onih koje su samo u knjizi, s njihovom zadnjom
// verzijom
func (r *PropisiRepository) spomeniciIzmjena(ctx context.Context, tx *sql.Tx, propisID string, u *models.Uklanjanje) error {
	izmjene, err := viseZapisa[models.IzmjenaPropisa](ctx, tx, `SELECT sadrzaj FROM propisi_izmjene WHERE propis_id = ? ORDER BY id`, propisID)
	for i := range izmjene {
		if err == nil {
			izmjene[i].Uklonjeno = u
			_, err = r.rec.Archive(ctx, tx, EntityPropisiIzmjene, izmjene[i].ID, &izmjene[i])
		}
	}
	if err != nil {
		return err
	}
	return arhivirajSamoUKnjizi(ctx, tx, r.rec, vezaIzmjenePropisa, propisID)
}

// UkloniIzmjenu miče izmjenu s površine i bilježi spomenik s razlogom.
// Izmjena koje nema ne mijenja ništa.
func (r *PropisiRepository) UkloniIzmjenu(ctx context.Context, id string, u models.Uklanjanje) error {
	return r.uTransakciji(ctx, func(tx *sql.Tx) error {
		iz, err := jedanZapis[models.IzmjenaPropisa](ctx, tx, `SELECT sadrzaj FROM propisi_izmjene WHERE id = ?`, id)
		if err != nil || iz == nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM propisi_izmjene WHERE id = ?`, id); err != nil {
			return err
		}
		iz.Uklonjeno = &u
		_, err = r.rec.Archive(ctx, tx, EntityPropisiIzmjene, id, iz)
		return err
	})
}

// Get čita propis po oznaci zapisa; nil, nil kad ga nema
func (r *PropisiRepository) Get(ctx context.Context, id string) (*models.Propis, error) {
	return jedanZapis[models.Propis](ctx, r.db, `SELECT sadrzaj FROM propisi WHERE id = ?`, id)
}

// PoAdresi čita propis po stalnoj adresi ili po oznaci zapisa. Kad dva
// zapisa imaju istu adresu (učitana na dva računala prije razmjene),
// vrijedi stariji.
func (r *PropisiRepository) PoAdresi(ctx context.Context, adresa string) (*models.Propis, error) {
	return jedanZapis[models.Propis](ctx, r.db, `SELECT sadrzaj FROM propisi WHERE adresa = ? OR id = ?
		ORDER BY created_at, id LIMIT 1`, adresa, adresa)
}

// Izmjena čita izmjenu po oznaci zapisa; nil, nil kad je nema
func (r *PropisiRepository) Izmjena(ctx context.Context, id string) (*models.IzmjenaPropisa, error) {
	return jedanZapis[models.IzmjenaPropisa](ctx, r.db, `SELECT sadrzaj FROM propisi_izmjene WHERE id = ?`, id)
}

// Izmjene su izmjene propisa redom izdanja u Narodnim novinama
func (r *PropisiRepository) Izmjene(ctx context.Context, propisID string) ([]models.IzmjenaPropisa, error) {
	return viseZapisa[models.IzmjenaPropisa](ctx, r.db, `SELECT sadrzaj FROM propisi_izmjene WHERE propis_id = ?
		ORDER BY nn_godina, nn_broj, nn_akt, id`, propisID)
}

// Popis su svi propisi bez teksta, po nazivu, s opsegom osnovnog teksta iz
// kazala i brojem izmjena
func (r *PropisiRepository) Popis(ctx context.Context) ([]SazetakPropisa, error) {
	opsezi, err := r.opsezi(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id, p.adresa, p.naziv, p.skupina, p.eli, p.nn_godina, p.nn_broj, p.nn_akt,
		(SELECT count(*) FROM propisi_izmjene i WHERE i.propis_id = p.id), p.created_at, p.updated_at
		FROM propisi p ORDER BY p.naziv, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SazetakPropisa
	for err == nil && rows.Next() {
		var s SazetakPropisa
		err = rows.Scan(&s.ID, &s.Adresa, &s.Naziv, &s.Skupina, &s.ELI, &s.NN.Godina, &s.NN.Broj, &s.NN.Akt,
			&s.Izmjena, &s.CreatedAt, &s.UpdatedAt)
		s.Opseg = opsezi[s.ID]
		out = append(out, s)
	}
	return procitano(out, rows, err)
}

// opsezi broje dijelove osnovnog teksta svakog propisa po vrsti
func (r *PropisiRepository) opsezi(ctx context.Context) (map[string]propisi.Opseg, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT propis_id, vrsta, count(*) FROM propisi_dijelovi
		WHERE izmjena_id IS NULL GROUP BY propis_id, vrsta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]propisi.Opseg{}
	for err == nil && rows.Next() {
		var id, vrsta string
		var n int
		err = rows.Scan(&id, &vrsta, &n)
		o := out[id]
		o.Dodaj(vrsta, n)
		out[id] = o
	}
	if err == nil {
		err = rows.Err()
	}
	return out, err
}

// zaLike štiti znakove koji u LIKE imaju posebno značenje (uz ESCAPE '\')
var zaLike = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Trazi su dijelovi propisa i izmjena čiji svedeni tekst sadrži svaki od
// korijena, redom: propis, osnovni tekst prije izmjena, redni broj dijela.
// Uz propisID traži samo u tom propisu i njegovim izmjenama. Iz svakog
// propisa uzima najviše najvise dijelova (0 ili manje: sve). Bez korijena
// ne nalazi ništa.
func (r *PropisiRepository) Trazi(ctx context.Context, korijeni []string, propisID string, najvise int) ([]RedakKazala, error) {
	if len(korijeni) == 0 {
		return nil, nil
	}
	uvjeti := make([]string, len(korijeni))
	args := []any{propisID, propisID}
	for i, k := range korijeni {
		uvjeti[i] = `svedeno LIKE ? ESCAPE '\'`
		args = append(args, "%"+zaLike.Replace(propisi.Svedi(k))+"%")
	}
	args = append(args, najvise, najvise)
	return r.redciKazala(ctx, `SELECT `+stupciKazala+` FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY propis_id
			ORDER BY izmjena_id IS NOT NULL, izmjena_id, redni) AS poredak
		FROM propisi_dijelovi WHERE (? = '' OR propis_id = ?) AND `+strings.Join(uvjeti, " AND ")+`)
		WHERE ? <= 0 OR poredak <= ? ORDER BY propis_id, izmjena_id IS NOT NULL, izmjena_id, redni`, args...)
}

// DioPoOznaci su dijelovi osnovnog teksta s danom oznakom, u svim
// propisima ili samo u propisID: izravan pogodak za „točka XXIII.” ili
// „članak 119.”
func (r *PropisiRepository) DioPoOznaci(ctx context.Context, oznaka, propisID string) ([]RedakKazala, error) {
	return r.redciKazala(ctx, `SELECT `+stupciKazala+` FROM propisi_dijelovi
		WHERE oznaka = ? AND izmjena_id IS NULL AND (? = '' OR propis_id = ?) ORDER BY propis_id, redni`, oznaka, propisID, propisID)
}

// redciKazala čita retke kazala upitom koji vraća stupciKazala
func (r *PropisiRepository) redciKazala(ctx context.Context, upit string, args ...any) ([]RedakKazala, error) {
	rows, err := r.db.QueryContext(ctx, upit, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RedakKazala
	for err == nil && rows.Next() {
		var k RedakKazala
		err = rows.Scan(&k.PropisID, &k.IzmjenaID, &k.Oznaka, &k.Vrsta, &k.Broj, &k.Naslov, &k.Roditelj, &k.Tekst, &k.Redni)
		out = append(out, k)
	}
	return procitano(out, rows, err)
}

// jedanZapis čita sadržaj jednog zapisa (stupac sadrzaj) u tip T; nil, nil
// kad ga nema
func jedanZapis[T any](ctx context.Context, q db.Execer, upit string, args ...any) (*T, error) {
	var sadrzaj string
	err := q.QueryRowContext(ctx, upit, args...).Scan(&sadrzaj)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	var x T
	if err == nil {
		err = json.Unmarshal([]byte(sadrzaj), &x)
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// viseZapisa čita sadržaje zapisa (stupac sadrzaj) u tip T, redom upita
func viseZapisa[T any](ctx context.Context, q db.Execer, upit string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, upit, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for err == nil && rows.Next() {
		var sadrzaj string
		var x T
		if err = rows.Scan(&sadrzaj); err == nil {
			err = json.Unmarshal([]byte(sadrzaj), &x)
		}
		out = append(out, x)
	}
	return procitano(out, rows, err)
}

// procitano vraća pročitane retke kad ni čitanje ni redovi nisu javili
// grešku, inače samo grešku
func procitano[T any](out []T, rows *sql.Rows, err error) ([]T, error) {
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
