// Baza prognoza stoji odvojeno od operativne i od arhive.
//
// Prognoza nije zapis nego račun: izgubi li se, ponovno se izračuna iz istih
// ulaza. Zato ne ide kroz knjigu verzija, gdje bi svaki redak stajao kilobajt
// i gdje bi se versionirala povijest nečega što je nema. Ni u arhivu ne ide —
// ondje stoji izmjereno, a razlika između izmjerenog i prognoziranog je upravo
// ono što pogrešnik mjeri.
//
// Ali izdana prognoza jest zapis. Ono što smo rekli u 14 h za sutra u 14 h ne
// može se poslije rekonstruirati ako se model u međuvremenu promijenio — pa se
// pamti svaka, s trenutkom izdavanja.
package prognoza

import (
	"database/sql"
	"fmt"
	"gocop/internal/models"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

const shema = `
-- Jedan pojas vodnosti jedne letve: što se u njemu očekuje i koliko se na to
-- može osloniti. Pojas postoji jer se ponašanje mijenja s razinom — na Batini
-- → Aljmaš val putuje 4 sata pri maloj vodi i 38 pri velikoj, jer se Kopački
-- rit puni i val uspori. Jedan pomak za sve vode dao bi prognozu koja je pri
-- velikoj vodi — kad je jedino važna — sustavno preuranjena.
--
-- Granice pojasa mjere se u prvom ulazu, onom na glavnom toku, i u njegovoj
-- veličini — koja ne mora biti ista kao ciljeva: Vrbovka se vodi u
-- centimetrima, a pojasi su joj u kubicima Terezina Polja.
CREATE TABLE IF NOT EXISTS pojasi (
	letva      TEXT NOT NULL,          -- ona koja se prognozira
	velicina   TEXT NOT NULL,          -- vodostaj | protok
	pojas_od   REAL NOT NULL,
	pojas_do   REAL NOT NULL,
	odsjecak   REAL NOT NULL,
	r          REAL NOT NULL,          -- koliko veza drži
	rasap      REAL NOT NULL,          -- standardno odstupanje ostatka
	sati       INTEGER NOT NULL,       -- na koliko je sati namješteno
	namjesteno TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (letva, velicina, pojas_od)
) WITHOUT ROWID;

-- Ulazi jednog pojasa. Letva ih može imati više: Botovo bez Mure drži svega
-- R² 0,24–0,45 po pojasu, a s njom 0,83–0,87. Bez drugog ulaza regresija
-- Murin doprinos pripiše Dravi, pa Donjoj Dubravi ispadne nagib 2,07 — protok
-- koji se na dvadeset kilometara udvostruči.
CREATE TABLE IF NOT EXISTS ulazi (
	letva       TEXT NOT NULL,
	velicina    TEXT NOT NULL,
	pojas_od    REAL NOT NULL,
	redni       INTEGER NOT NULL,      -- 0 je glavni tok; po njemu je pojas
	uzvodna     TEXT NOT NULL,
	uz_velicina TEXT NOT NULL,         -- ne mora biti ista: Letenye daje cm, Botovo m³/s
	pomak_h     INTEGER NOT NULL,
	-- Koliko sati unatrag se ulaz prosječi prije nego uđe u račun. Rijeka
	-- kratke valove guši: dnevni val hidroelektrane nosi na Donjoj Dubravi
	-- 21,6 m³/s promjene po satu, a Belišće se nikad nije pomaknulo više od 9
	-- cm. Bez prozora pomak i množenje val samo prenesu, pa prognoza poskakuje
	-- kako ta letva ne poznaje.
	sirina_h    INTEGER NOT NULL DEFAULT 1,
	nagib       REAL NOT NULL,
	PRIMARY KEY (letva, velicina, pojas_od, redni)
) WITHOUT ROWID;

-- Svaka izdana prognoza. Ključ nosi i trenutak izdavanja jer se ista ciljna
-- ura prognozira iznova sa svakim novim satom očitanja — a pogrešnik treba
-- znati što smo mislili kad.
CREATE TABLE IF NOT EXISTS izdane (
	letva      TEXT NOT NULL,
	-- Ista letva stoji u obje veličine: model radi u jednoj, a krivulja daje
	-- drugu. Gornja Drava se računa u protoku jer joj se korito produbljuje pa
	-- vodostaj kroz desetljeća mijenja značenje, a dežurni ipak čita
	-- centimetre. Gdje krivulje nema, stoji samo ona u kojoj se računa.
	velicina   TEXT NOT NULL,         -- vodostaj | protok
	izdano     INTEGER NOT NULL,      -- sat kad je prognoza izdana, UTC
	ciljni     INTEGER NOT NULL,      -- sat na koji se odnosi, UTC
	vrijednost REAL NOT NULL,
	-- Granice raspona, a ne jedna simetrična brojka: kroz krivulju se raspon
	-- prenosi rubovima, a krivulja je zakrivljena, pa u drugoj veličini
	-- ispadne nesimetričan. Raspored se pritom ne mijenja — granica od 68 %
	-- ostaje granica od 68 % i nakon rastuće preslike.
	dolje      REAL NOT NULL,
	gore       REAL NOT NULL,
	racunata   INTEGER NOT NULL DEFAULT 0, -- 1 kad je dobivena iz krivulje
	izvan      INTEGER NOT NULL DEFAULT 0, -- 1 kad je krivulja produljena preko ruba
	model      TEXT NOT NULL,
	PRIMARY KEY (letva, velicina, izdano, ciljni)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS izdane_ciljni ON izdane(letva, ciljni);

-- Dnevna prognoza za 1.–6. dan, uz satnu. Stoji zasebno jer se ne odnosi na
-- sat nego na 24 sata koja završavaju u satu ciljni; u istoj tablici sudarala
-- bi se sa satnom na istom ciljnom satu. Dan 0 je izmjereni srednjak zadnjih
-- 24 sata, od kojeg prognoza kreće.
CREATE TABLE IF NOT EXISTS dnevne (
	letva      TEXT NOT NULL,
	izdano     INTEGER NOT NULL,      -- sat izdavanja, UTC
	dan        INTEGER NOT NULL,      -- 0 … 6
	ciljni     INTEGER NOT NULL,      -- sat kojim dan završava, UTC
	vrijednost REAL NOT NULL,         -- cm
	dolje      REAL NOT NULL,
	gore       REAL NOT NULL,
	model      TEXT NOT NULL,
	PRIMARY KEY (letva, izdano, dan)
) WITHOUT ROWID;

-- Koliko prognoza promašuje, izmjereno puštanjem unatrag po arhivi. Raspon uz
-- izdanu prognozu dolazi odavde, a ne iz rasapa namještanja: ispravak prema
-- mjerenju u trenutku izdavanja ukloni velik dio te pogreške, pa bi rasap
-- namještanja obećavao lošije nego što doista jest — pokrivenost je bila 100 %
-- ondje gdje bi trebala biti oko 68.
--
-- Uz svaki doseg stoji i promašaj postojanosti, prognoze da se ništa neće
-- promijeniti. Ondje gdje je naš veći, prognozu ne treba izdavati.
CREATE TABLE IF NOT EXISTS promasaji (
	letva       TEXT NOT NULL,
	velicina    TEXT NOT NULL,
	doseg_h     INTEGER NOT NULL,
	pomak       REAL NOT NULL,     -- sustavni, prosječni promašaj
	rasap       REAL NOT NULL,     -- standardno odstupanje promašaja
	postojanost REAL NOT NULL,     -- promašaj prognoze da se ništa ne mijenja
	slucaja     INTEGER NOT NULL,
	mjereno     TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (letva, velicina, doseg_h)
) WITHOUT ROWID;

-- Tuđe prognoze, radi usporedbe. Nikad ne ulaze u naš račun: prognoza koja se
-- oslanja na tuđu ne može biti provjera tuđoj.
CREATE TABLE IF NOT EXISTS tude (
	izvor      TEXT NOT NULL,         -- hydroinfo.hu …
	letva      TEXT NOT NULL,
	izdano     INTEGER NOT NULL,
	ciljni     INTEGER NOT NULL,
	vrijednost REAL NOT NULL,
	raspon     REAL NOT NULL DEFAULT 0,
	PRIMARY KEY (izvor, letva, izdano, ciljni)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS tude_ciljni ON tude(letva, ciljni);
`

// Otvori otvara bazu prognoza i slaže shemu ako je nema.
func Otvori(put string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", put+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(shema); err != nil {
		db.Close()
		return nil, fmt.Errorf("shema baze prognoza: %w", err)
	}
	if err := uskladi(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// uskladi dograđuje tablice koje su nastale prije nego što su dobile sve
// stupce. CREATE TABLE IF NOT EXISTS zatečenu tablicu ne dira, pa bi inače
// baza nastala jučer danas pucala na upisu.
func uskladi(db *sql.DB) error {
	// Pojasi i ulazi su izračunati podaci: namjesti-prognozu ih izgradi iznova.
	for tablica, stupac := range map[string]string{
		"izdane": "gore", "tude": "vrijednost", "ulazi": "sirina_h"} {
		ima, err := imaStupac(db, tablica, stupac)
		if err != nil {
			return err
		}
		if ima {
			continue
		}
		// Prognoza je račun, ne zapis: izgubi li se, ponovno se izračuna iz
		// istih ulaza. Zato se stara tablica smije jednostavno odbaciti.
		odbaci := []string{tablica}
		if tablica == "ulazi" {
			odbaci = append(odbaci, "pojasi") // idu zajedno; bez ulaza pojas ništa ne znači
		}
		for _, x := range odbaci {
			if _, err := db.Exec(`DROP TABLE IF EXISTS ` + x); err != nil {
				return fmt.Errorf("uklanjanje stare tablice %s: %w", x, err)
			}
		}
		if _, err := db.Exec(shema); err != nil {
			return fmt.Errorf("ponovna gradnja %s: %w", tablica, err)
		}
	}
	return nil
}

func imaStupac(db *sql.DB, tablica, stupac string) (bool, error) {
	r, err := db.Query(`SELECT name FROM pragma_table_info(?)`, tablica)
	if err != nil {
		return false, err
	}
	defer r.Close()
	for r.Next() {
		var ime string
		if err := r.Scan(&ime); err != nil {
			return false, err
		}
		if ime == stupac {
			return true, nil
		}
	}
	return false, r.Err()
}

// Ulaz je jedna uzvodna letva koja ulazi u račun, s vlastitim kašnjenjem.
type Ulaz struct {
	Letva    string
	Velicina string
	PomakH   int
	Sirina   int // koliko sati unatrag se prosječi; 1 je bez glačanja
	Nagib    float64
}

// Pojas je namješten račun za jednu letvu u jednom pojasu vodnosti.
type Pojas struct {
	Letva    string
	Velicina string
	Od, Do   float64
	Ulazi    []Ulaz
	Odsjecak float64
	R, Rasap float64
	Sati     int
}

// Vrijedi javlja pripada li vrijednost glavnog ulaza ovom pojasu. Vrijednost
// se mjeri u veličini glavnog ulaza, ne cilja.
func (p Pojas) Vrijedi(vrijednost float64) bool {
	return vrijednost >= p.Od && vrijednost <= p.Do
}

// Racunaj zbraja doprinose ulaza. Vrijednosti idu redom kojim stoje Ulazi,
// svaka već očitana u svojem satu.
func (p Pojas) Racunaj(vrijednosti []float64) (float64, error) {
	if len(vrijednosti) != len(p.Ulazi) {
		return 0, fmt.Errorf("%s: %d vrijednosti za %d ulaza", p.Letva, len(vrijednosti), len(p.Ulazi))
	}
	v := p.Odsjecak
	for i, u := range p.Ulazi {
		v += u.Nagib * vrijednosti[i]
	}
	return v, nil
}

// Spremi zapisuje namještene pojase, zamjenjujući zatečene za iste letve.
func Spremi(db *sql.DB, pojasi []Pojas, kad string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Zatečeno stanje odlazi u cijelosti, jer pojasi su jedna cjelina: ono
	// što u novom namještanju nema svoj račun nema ga ni u bazi.
	//
	// Brisalo se najprije po letvi i veličini zajedno, pa su Aljmašu, Dalju i
	// Vukovaru pri prelasku s protoka na vodostaj ostali visjeti protočni
	// pojasi. Brisanje po letvi to je riješilo, ali ne i teži slučaj: izbaci
	// li se karika iz lanca, njezini pojasi ostaju jer ih novi skup više ne
	// spominje. Tako je Vidovićev Mlin nakon probe s Plitvicom ostao u bazi s
	// Krkancem kao vrhom lanca, a Krkanec očitanja nije imao — i cijela
	// prognoza je pala na "nijedna ulazna letva nema svježa očitanja".
	for _, t := range []string{"pojasi", "ulazi"} {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return fmt.Errorf("čišćenje %s: %w", t, err)
		}
	}
	for _, p := range pojasi {
		if _, err := tx.Exec(`INSERT INTO pojasi
			(letva, velicina, pojas_od, pojas_do, odsjecak, r, rasap, sati, namjesteno)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			p.Letva, p.Velicina, p.Od, p.Do, p.Odsjecak, p.R, p.Rasap, p.Sati, kad); err != nil {
			return fmt.Errorf("pojas %s %.0f: %w", p.Letva, p.Od, err)
		}
		for i, u := range p.Ulazi {
			if _, err := tx.Exec(`INSERT INTO ulazi
				(letva, velicina, pojas_od, redni, uzvodna, uz_velicina, pomak_h, sirina_h, nagib)
				VALUES (?,?,?,?,?,?,?,?,?)`,
				p.Letva, p.Velicina, p.Od, i, u.Letva, u.Velicina, u.PomakH, u.Sirina, u.Nagib); err != nil {
				return fmt.Errorf("ulaz %s ← %s: %w", p.Letva, u.Letva, err)
			}
		}
	}
	return tx.Commit()
}

// ZaLetvu čita namještene pojase jedne letve, po granicama.
func ZaLetvu(db *sql.DB, letva string) ([]Pojas, error) {
	r, err := db.Query(`SELECT letva, velicina, pojas_od, pojas_do, odsjecak, r, rasap, sati
		FROM pojasi WHERE letva = ? ORDER BY pojas_od`, letva)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []Pojas
	for r.Next() {
		var p Pojas
		if err := r.Scan(&p.Letva, &p.Velicina, &p.Od, &p.Do,
			&p.Odsjecak, &p.R, &p.Rasap, &p.Sati); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		u, err := ulaziPojasa(db, out[i])
		if err != nil {
			return nil, err
		}
		out[i].Ulazi = u
	}
	return out, nil
}

func ulaziPojasa(db *sql.DB, p Pojas) ([]Ulaz, error) {
	r, err := db.Query(`SELECT uzvodna, uz_velicina, pomak_h, sirina_h, nagib FROM ulazi
		WHERE letva = ? AND velicina = ? AND pojas_od = ? ORDER BY redni`,
		p.Letva, p.Velicina, p.Od)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []Ulaz
	for r.Next() {
		var u Ulaz
		if err := r.Scan(&u.Letva, &u.Velicina, &u.PomakH, &u.Sirina, &u.Nagib); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, r.Err()
}

// SpremiIzdane zapisuje izdane prognoze. Ista ciljna ura prognozira se iznova
// sa svakim novim satom očitanja, pa se zapisi ne gaze: ključ nosi i trenutak
// izdavanja, a pogrešnik poslije uspoređuje što smo mislili kad.
func SpremiIzdane(db *sql.DB, izdane []Izdana) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, i := range izdane {
		if _, err := tx.Exec(`INSERT INTO izdane
			(letva, velicina, izdano, ciljni, vrijednost, dolje, gore, racunata, izvan, model)
			VALUES (?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(letva, velicina, izdano, ciljni) DO UPDATE SET
				vrijednost=excluded.vrijednost, dolje=excluded.dolje, gore=excluded.gore,
				racunata=excluded.racunata, izvan=excluded.izvan, model=excluded.model`,
			i.Letva, i.Velicina, i.Izdano, i.Ciljni, i.Vrijednost, i.Dolje, i.Gore,
			i.Racunata, i.Izvan, i.Model); err != nil {
			return fmt.Errorf("izdana %s za %d: %w", i.Letva, i.Ciljni, err)
		}
	}
	return tx.Commit()
}

// SpremiDnevne zapisuje dnevnu prognozu.
func SpremiDnevne(db *sql.DB, dnevne []DnevnaIzdana) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, d := range dnevne {
		if _, err := tx.Exec(`INSERT INTO dnevne
			(letva, izdano, dan, ciljni, vrijednost, dolje, gore, model)
			VALUES (?,?,?,?,?,?,?,?)
			ON CONFLICT(letva, izdano, dan) DO UPDATE SET ciljni=excluded.ciljni,
				vrijednost=excluded.vrijednost, dolje=excluded.dolje, gore=excluded.gore,
				model=excluded.model`,
			d.Letva, d.Izdano, d.Dan, d.Ciljni, d.Vrijednost, d.Dolje, d.Gore, d.Model); err != nil {
			return fmt.Errorf("dnevna %s, %d. dan: %w", d.Letva, d.Dan, err)
		}
	}
	return tx.Commit()
}

// ZadnjeDnevno čita najnovije dnevno izdanje, složeno po letvi i danu.
func ZadnjeDnevno(db *sql.DB) (int64, map[string][]DnevnaIzdana, error) {
	var izdano sql.NullInt64
	if err := db.QueryRow(`SELECT max(izdano) FROM dnevne`).Scan(&izdano); err != nil || !izdano.Valid {
		return 0, nil, err
	}
	r, err := db.Query(`SELECT letva, izdano, dan, ciljni, vrijednost, dolje, gore, model
		FROM dnevne WHERE izdano = ? ORDER BY letva, dan`, izdano.Int64)
	if err != nil {
		return 0, nil, err
	}
	defer r.Close()
	out := map[string][]DnevnaIzdana{}
	for r.Next() {
		var d DnevnaIzdana
		if err := r.Scan(&d.Letva, &d.Izdano, &d.Dan, &d.Ciljni, &d.Vrijednost,
			&d.Dolje, &d.Gore, &d.Model); err != nil {
			return 0, nil, err
		}
		out[d.Letva] = append(out[d.Letva], d)
	}
	return izdano.Int64, out, r.Err()
}

// SpremiTude zapisuje tuđu prognozu za letve koje i mi vodimo. Isto izdanje
// dolazi sa svakim satom dok ne izađe novo, pa se ponovljeni zapis preskače.
// Vraća koliko je novih vrijednosti upisano.
func SpremiTude(db *sql.DB, izvor string, letve []Letva, sifra func(string) string) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	novih := 0
	for _, l := range letve {
		kod := sifra(l.Naziv)
		if kod == "" || l.Izdano.IsZero() {
			continue
		}
		izdano := l.Izdano.UTC().Unix() / 3600
		dani := l.Dani
		// Jutarnje mjerenje iz njihove tablice stoji uz prognozu kao sidro:
		// ciljni sat mu je 07 h dana izdanja, dakle prije izdanja, pa se po
		// tome i razlikuje od prognoze. Hod tuđe prognoze od sata izdanja
		// računa se od njega.
		if l.ImaDanas {
			lok := l.Izdano.In(models.Zagreb)
			jutro := time.Date(lok.Year(), lok.Month(), lok.Day(), 7, 0, 0, 0, models.Zagreb)
			dani = append([]Dan{{Kad: jutro, Cm: l.Danas}}, dani...)
		}
		for _, d := range dani {
			r, err := tx.Exec(`INSERT OR IGNORE INTO tude (izvor, letva, izdano, ciljni, vrijednost, raspon)
				VALUES (?,?,?,?,?,?)`, izvor, kod, izdano, d.Kad.UTC().Unix()/3600, d.Cm, d.PlusMin)
			if err != nil {
				return 0, fmt.Errorf("%s %s: %w", izvor, kod, err)
			}
			if n, _ := r.RowsAffected(); n > 0 {
				novih += int(n)
			}
		}
	}
	return novih, tx.Commit()
}

// TudaPrognoza vraća najnoviju tuđu prognozu letve izdanu u zadanom
// razdoblju, satno pravocrtno između njezinih točaka; ok je netočno kad je
// nema.
func TudaPrognoza(db *sql.DB, izvor, letva string, od, do int64) (Niz, bool, error) {
	var izdano sql.NullInt64
	if err := db.QueryRow(`SELECT max(izdano) FROM tude WHERE izvor = ? AND letva = ?
		AND izdano BETWEEN ? AND ?`, izvor, letva, od, do).Scan(&izdano); err != nil || !izdano.Valid {
		return Niz{}, false, err
	}
	r, err := db.Query(`SELECT ciljni, vrijednost FROM tude WHERE izvor = ? AND letva = ? AND izdano = ?`,
		izvor, letva, izdano.Int64)
	if err != nil {
		return Niz{}, false, err
	}
	defer r.Close()
	tocke := map[int64]float64{}
	for r.Next() {
		var t int64
		var v float64
		if err := r.Scan(&t, &v); err != nil {
			return Niz{}, false, err
		}
		tocke[t] = v
	}
	if len(tocke) < 2 {
		return Niz{}, false, r.Err()
	}
	return NizIzTocaka(tocke), true, r.Err()
}

// NizIzTocaka slaže satni niz iz rijetkih točaka, pravocrtno između njih.
func NizIzTocaka(tocke map[int64]float64) Niz {
	sati := make([]int64, 0, len(tocke))
	for t := range tocke {
		sati = append(sati, t)
	}
	sort.Slice(sati, func(i, j int) bool { return sati[i] < sati[j] })
	v := map[int64]float64{}
	for i := 0; i+1 < len(sati); i++ {
		a, b := sati[i], sati[i+1]
		for t := a; t <= b; t++ {
			v[t] = tocke[a] + (tocke[b]-tocke[a])*float64(t-a)/float64(b-a)
		}
	}
	if len(sati) == 1 {
		v[sati[0]] = tocke[sati[0]]
	}
	return NoviNiz(v)
}

// SviPojasi čita namještene pojase svih letvi, složene po letvi.
func SviPojasi(db *sql.DB) (map[string][]Pojas, error) {
	r, err := db.Query(`SELECT DISTINCT letva FROM pojasi`)
	if err != nil {
		return nil, err
	}
	var imena []string
	for r.Next() {
		var l string
		if err := r.Scan(&l); err != nil {
			r.Close()
			return nil, err
		}
		imena = append(imena, l)
	}
	r.Close()
	if err := r.Err(); err != nil {
		return nil, err
	}
	out := make(map[string][]Pojas, len(imena))
	for _, l := range imena {
		p, err := ZaLetvu(db, l)
		if err != nil {
			return nil, err
		}
		out[l] = p
	}
	return out, nil
}

// ZadnjeIzdanje vraća sat najnovijeg izdanja.
func ZadnjeIzdanje(db *sql.DB) (int64, bool, error) {
	var sat sql.NullInt64
	if err := db.QueryRow(`SELECT max(izdano) FROM izdane`).Scan(&sat); err != nil {
		return 0, false, err
	}
	return sat.Int64, sat.Valid, nil
}

// Izdanje čita sve prognoze jednog izdanja, složene po letvi i poredane po
// ciljnom satu.
func Izdanje(db *sql.DB, izdano int64) (map[string][]Izdana, error) {
	r, err := db.Query(`SELECT letva, velicina, izdano, ciljni, vrijednost, dolje, gore,
		racunata, izvan, model FROM izdane WHERE izdano = ? ORDER BY letva, velicina, ciljni`, izdano)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := map[string][]Izdana{}
	for r.Next() {
		var i Izdana
		if err := r.Scan(&i.Letva, &i.Velicina, &i.Izdano, &i.Ciljni,
			&i.Vrijednost, &i.Dolje, &i.Gore, &i.Racunata, &i.Izvan, &i.Model); err != nil {
			return nil, err
		}
		out[i.Letva] = append(out[i.Letva], i)
	}
	return out, r.Err()
}

// Redom slaže letve tako da uzvodne idu prije nizvodnih. Popis se time čita kao
// lanac, onako kako voda i teče, a ne po abecedi.
func Redom(pojasi map[string][]Pojas) []string {
	gotovo := map[string]bool{}
	var out []string
	var stavi func(string)
	stavi = func(l string) {
		if gotovo[l] || len(pojasi[l]) == 0 {
			return
		}
		gotovo[l] = true
		for _, u := range pojasi[l][0].Ulazi {
			stavi(u.Letva)
		}
		out = append(out, l)
	}
	imena := make([]string, 0, len(pojasi))
	for l := range pojasi {
		imena = append(imena, l)
	}
	sort.Strings(imena)
	for _, l := range imena {
		stavi(l)
	}
	return out
}

// ModelLanac je oznaka pod kojom se prognoza zapisuje, da se poslije zna po
// čemu je izdana. Mijenja se kad se promijeni oblik računa, ne kad se samo
// osvježe koeficijenti.
const ModelLanac = "lanac-1"

// UdioURasponu je koliki dio promašaja mora stati u raspon koji uz prognozu
// piše. Raspon se po njemu mjeri brojanjem, pa tvrdnja "ostaje unutar raspona
// u 68 % slučajeva" vrijedi kao izmjerena činjenica, a ne kao pretpostavka o
// rasporedu pogrešaka. Mađarska služba uz svoj graf navodi 70 %; 68 se poklapa
// s jednim standardnim odstupanjem, pa se dvije mjere daju uspoređivati.
const UdioURasponu = 0.68

// Promasaj je izmjereno koliko prognoza promašuje na jednom dosegu.
type Promasaj struct {
	Letva       string
	Velicina    string
	DosegH      int
	Pomak       float64
	Rasap       float64
	Postojanost float64
	Slucaja     int
}

// BoljaOdPostojanosti javlja isplati li se prognoza na tom dosegu.
func (p Promasaj) BoljaOdPostojanosti() bool {
	return p.Postojanost > 0 && p.Rasap < p.Postojanost
}

// SpremiPromasaje zapisuje izmjerene promašaje, zamjenjujući zatečene.
func SpremiPromasaje(db *sql.DB, promasaji []Promasaj, kad string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range promasaji {
		if _, err := tx.Exec(`INSERT INTO promasaji
			(letva, velicina, doseg_h, pomak, rasap, postojanost, slucaja, mjereno)
			VALUES (?,?,?,?,?,?,?,?)
			ON CONFLICT(letva, velicina, doseg_h) DO UPDATE SET
				pomak=excluded.pomak, rasap=excluded.rasap,
				postojanost=excluded.postojanost, slucaja=excluded.slucaja,
				mjereno=excluded.mjereno`,
			p.Letva, p.Velicina, p.DosegH, p.Pomak, p.Rasap, p.Postojanost,
			p.Slucaja, kad); err != nil {
			return fmt.Errorf("promašaj %s na %d h: %w", p.Letva, p.DosegH, err)
		}
	}
	return tx.Commit()
}

// Promasaji čita izmjerene promašaje: letva → doseg u satima → promašaj.
func Promasaji(db *sql.DB) (map[string]map[int]Promasaj, error) {
	r, err := db.Query(`SELECT letva, velicina, doseg_h, pomak, rasap, postojanost, slucaja
		FROM promasaji ORDER BY letva, doseg_h`)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := map[string]map[int]Promasaj{}
	for r.Next() {
		var p Promasaj
		if err := r.Scan(&p.Letva, &p.Velicina, &p.DosegH, &p.Pomak, &p.Rasap,
			&p.Postojanost, &p.Slucaja); err != nil {
			return nil, err
		}
		if out[p.Letva] == nil {
			out[p.Letva] = map[int]Promasaj{}
		}
		out[p.Letva][p.DosegH] = p
	}
	return out, r.Err()
}

// ZaVrijednost bira pojas kojem vrijednost glavnog ulaza pripada. Izvan svih
// pojasa vraća najbliži rub: prognoza pri vodi kakvu nismo vidjeli nije
// pouzdana, ali šutjeti o njoj bilo bi gore — rasap uz nju to i kaže.
func ZaVrijednost(pojasi []Pojas, vrijednost float64) (Pojas, bool) {
	if len(pojasi) == 0 {
		return Pojas{}, false
	}
	for _, p := range pojasi {
		if p.Vrijedi(vrijednost) {
			return p, true
		}
	}
	if vrijednost < pojasi[0].Od {
		return pojasi[0], true
	}
	return pojasi[len(pojasi)-1], true
}
