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

	_ "modernc.org/sqlite"
)

const shema = `
-- Jedan pojas vodnosti jedne letve: što se u njemu očekuje i koliko se na to
-- može osloniti. Pojas postoji jer se ponašanje mijenja s razinom — na Batini
-- → Aljmaš val putuje 4 sata pri maloj vodi i 38 pri velikoj, jer se Kopački
-- rit puni i val uspori. Jedan pomak za sve vode dao bi prognozu koja je pri
-- velikoj vodi — kad je jedino važna — sustavno preuranjena.
--
-- Granice pojasa mjere se u prvom ulazu, onom na glavnom toku.
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
	nagib       REAL NOT NULL,
	PRIMARY KEY (letva, velicina, pojas_od, redni)
) WITHOUT ROWID;

-- Svaka izdana prognoza. Ključ nosi i trenutak izdavanja jer se ista ciljna
-- ura prognozira iznova sa svakim novim satom očitanja — a pogrešnik treba
-- znati što smo mislili kad.
CREATE TABLE IF NOT EXISTS izdane (
	letva   TEXT NOT NULL,
	izdano  INTEGER NOT NULL,         -- sat kad je prognoza izdana, UTC
	ciljni  INTEGER NOT NULL,         -- sat na koji se odnosi, UTC
	cm      REAL NOT NULL,
	raspon  REAL NOT NULL,            -- koliko se očekuje da promaši
	model   TEXT NOT NULL,
	PRIMARY KEY (letva, izdano, ciljni)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS izdane_ciljni ON izdane(letva, ciljni);

-- Tuđe prognoze, radi usporedbe. Nikad ne ulaze u naš račun: prognoza koja se
-- oslanja na tuđu ne može biti provjera tuđoj.
CREATE TABLE IF NOT EXISTS tude (
	izvor   TEXT NOT NULL,            -- hydroinfo.hu …
	letva   TEXT NOT NULL,
	izdano  INTEGER NOT NULL,
	ciljni  INTEGER NOT NULL,
	cm      REAL NOT NULL,
	raspon  REAL NOT NULL DEFAULT 0,
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
	return db, nil
}

// Ulaz je jedna uzvodna letva koja ulazi u račun, s vlastitim kašnjenjem.
type Ulaz struct {
	Letva    string
	Velicina string
	PomakH   int
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

// Vrijedi javlja pripada li vrijednost glavnog ulaza ovom pojasu.
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

	// Stari pojasi letve moraju otići: nova podjela ne mora imati iste
	// granice, pa bi inače ostali visjeti pojasi kojima više ništa ne
	// odgovara.
	ocisceno := map[string]bool{}
	for _, p := range pojasi {
		k := p.Letva + "\x00" + p.Velicina
		if ocisceno[k] {
			continue
		}
		ocisceno[k] = true
		for _, t := range []string{"pojasi", "ulazi"} {
			if _, err := tx.Exec(`DELETE FROM `+t+` WHERE letva = ? AND velicina = ?`,
				p.Letva, p.Velicina); err != nil {
				return fmt.Errorf("čišćenje %s za %s: %w", t, p.Letva, err)
			}
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
				(letva, velicina, pojas_od, redni, uzvodna, uz_velicina, pomak_h, nagib)
				VALUES (?,?,?,?,?,?,?,?)`,
				p.Letva, p.Velicina, p.Od, i, u.Letva, u.Velicina, u.PomakH, u.Nagib); err != nil {
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
	r, err := db.Query(`SELECT uzvodna, uz_velicina, pomak_h, nagib FROM ulazi
		WHERE letva = ? AND velicina = ? AND pojas_od = ? ORDER BY redni`,
		p.Letva, p.Velicina, p.Od)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []Ulaz
	for r.Next() {
		var u Ulaz
		if err := r.Scan(&u.Letva, &u.Velicina, &u.PomakH, &u.Nagib); err != nil {
			return nil, err
		}
		out = append(out, u)
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
