package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
)

// Čitanje hidrološke arhive. Arhiva je zasebna datoteka i program u nju ne
// piše — otvara se samo za čitanje, i njezin izostanak nije greška nego
// stanje: čvor koji je nije preuzeo radi bez povijesti.

type ArhivaRepository struct {
	db *sql.DB
}

// OpenArhiva otvara arhivu za čitanje. Vraća nil bez greške kad datoteke nema.
func OpenArhiva(path string) (*ArhivaRepository, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro&_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, nil
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM nizovi`).Scan(&n); err != nil {
		db.Close()
		return nil, nil
	}
	return &ArhivaRepository{db: db}, nil
}

func (r *ArhivaRepository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// Nizovi vraća sve nizove jedne letve, poredane tako da ovjereno dolazi prvo.
func (r *ArhivaRepository) Nizovi(ctx context.Context, letva string) ([]models.HidroNiz, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, sliv, letva, izvor, velicina, vrsta, po_danu,
		od, do_, zapisa, otisak FROM nizovi WHERE letva = ?`, letva)
	if err != nil {
		return nil, fmt.Errorf("dohvat nizova arhive: %w", err)
	}
	defer rows.Close()
	var out []models.HidroNiz
	for rows.Next() {
		var n models.HidroNiz
		var poDanu int
		if err := rows.Scan(&n.ID, &n.Sliv, &n.Letva, &n.Izvor, &n.Velicina, &n.Vrsta,
			&poDanu, &n.Od, &n.Do, &n.Zapisa, &n.Otisak); err != nil {
			return nil, err
		}
		n.PoDanu = poDanu == 1
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := rangIzvora(out[i].Izvor), rangIzvora(out[j].Izvor); a != b {
			return a < b
		}
		if out[i].Velicina != out[j].Velicina {
			return rangVelicine(out[i].Velicina) < rangVelicine(out[j].Velicina)
		}
		return out[i].Vrsta < out[j].Vrsta
	})
	return out, rows.Err()
}

// rangIzvora je poredak povjerenja: ovjereno prije sirovog, sirovo prije
// preračunatog. Isti poredak kojim se bira kad se izvori ne slažu.
func rangIzvora(i string) int {
	switch {
	case i == "his2000":
		return 0
	case i == "cop":
		return 1
	case i == "letva-dhmz":
		return 2
	case i == "letva-hv":
		return 3
	case i == "vituki":
		return 4
	}
	return 9
}

func rangVelicine(v string) int {
	for i, x := range []string{"vodostaj", "protok", "temperatura", "koncentracija", "pronos"} {
		if x == v {
			return i
		}
	}
	return 9
}

// Pregled računa karakteristične vrijednosti niza po godinama. Ništa se ne
// pamti u bazi: brojevi se izvode iz niza pri svakom čitanju, pa se ne mogu
// razići s podacima iz kojih su nastali.
func (r *ArhivaRepository) Pregled(ctx context.Context, nizID int64) (*models.HidroPregled, error) {
	if r == nil {
		return nil, nil
	}
	var p models.HidroPregled
	if err := r.db.QueryRowContext(ctx, `SELECT id, sliv, letva, izvor, velicina, vrsta, po_danu,
		od, do_, zapisa, otisak FROM nizovi WHERE id = ?`, nizID).Scan(&p.Niz.ID, &p.Niz.Sliv,
		&p.Niz.Letva, &p.Niz.Izvor, &p.Niz.Velicina, &p.Niz.Vrsta, new(int),
		&p.Niz.Od, &p.Niz.Do, &p.Niz.Zapisa, &p.Niz.Otisak); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT strftime('%Y', vrijeme, 'unixepoch') AS g, count(*),
		       min(vrijednost), max(vrijednost), avg(vrijednost), sum(vrijednost)
		FROM ocitanja WHERE niz = ? GROUP BY g ORDER BY g`, nizID)
	if err != nil {
		return nil, fmt.Errorf("karakteristične vrijednosti: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var g models.HidroGodina
		var god string
		if err := rows.Scan(&god, &g.Zapisa, &g.Min, &g.Max, &g.Srednjak, &g.Zbroj); err != nil {
			return nil, err
		}
		fmt.Sscanf(god, "%d", &g.Godina)
		p.Godine = append(p.Godine, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(p.Godine) == 0 {
		return &p, nil
	}

	// datumi ekstrema po godini, jednim prolazom
	dat, err := r.db.QueryContext(ctx, `
		SELECT strftime('%Y', vrijeme, 'unixepoch') AS g, vrijeme, vrijednost
		FROM ocitanja WHERE niz = ? ORDER BY vrijeme`, nizID)
	if err != nil {
		return nil, err
	}
	defer dat.Close()
	type ekstrem struct {
		min, max     float64
		minNa, maxNa string
		prvi         bool
	}
	po := map[int]*ekstrem{}
	for dat.Next() {
		var god string
		var t int64
		var v float64
		if err := dat.Scan(&god, &t, &v); err != nil {
			return nil, err
		}
		var y int
		fmt.Sscanf(god, "%d", &y)
		e := po[y]
		if e == nil {
			e = &ekstrem{min: v, max: v, prvi: true}
			po[y] = e
		}
		kad := time.Unix(t, 0).UTC().Format("2006-01-02")
		if v <= e.min || e.minNa == "" {
			e.min, e.minNa = v, kad
		}
		if v >= e.max || e.maxNa == "" {
			e.max, e.maxNa = v, kad
		}
	}
	// prvi zapis godine daje ekstrem, ostali ga samo pomiču; zato se datumi
	// uzimaju iz prvog pojavljivanja vrijednosti, ne zadnjeg
	for i := range p.Godine {
		if e := po[p.Godine[i].Godina]; e != nil {
			p.Godine[i].MinNa, p.Godine[i].MaxNa = e.minNa, e.maxNa
		}
	}

	p.Min, p.Max = p.Godine[0].Min, p.Godine[0].Max
	p.MinNa, p.MaxNa = p.Godine[0].MinNa, p.Godine[0].MaxNa
	var zbroj float64
	var n int
	for _, g := range p.Godine {
		if g.Min < p.Min {
			p.Min, p.MinNa = g.Min, g.MinNa
		}
		if g.Max > p.Max {
			p.Max, p.MaxNa = g.Max, g.MaxNa
		}
		zbroj += g.Srednjak * float64(g.Zapisa)
		n += g.Zapisa
	}
	if n > 0 {
		p.Srednjak = zbroj / float64(n)
	}
	if models.SeZbraja(p.Niz.Velicina) {
		p.ZbrojIma = true
		for _, g := range p.Godine {
			p.Zbroj += g.Zbroj
		}
	}

	if p.Mjeseci, err = r.mjeseci(ctx, nizID); err != nil {
		return nil, err
	}
	if p.Trajanje, err = r.trajanje(ctx, nizID); err != nil {
		return nil, err
	}
	return &p, nil
}

// mjeseci računa godišnji hod: isti mjesec kroz sve godine niza.
func (r *ArhivaRepository) mjeseci(ctx context.Context, nizID int64) ([]models.HidroMjesec, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT strftime('%m', vrijeme, 'unixepoch') AS m, count(*),
		       min(vrijednost), max(vrijednost), avg(vrijednost)
		FROM ocitanja WHERE niz = ? GROUP BY m ORDER BY m`, nizID)
	if err != nil {
		return nil, fmt.Errorf("godišnji hod: %w", err)
	}
	defer rows.Close()
	var out []models.HidroMjesec
	for rows.Next() {
		var m models.HidroMjesec
		var mj string
		if err := rows.Scan(&mj, &m.Zapisa, &m.Min, &m.Max, &m.Srednjak); err != nil {
			return nil, err
		}
		fmt.Sscanf(mj, "%d", &m.Mjesec)
		out = append(out, m)
	}
	return out, rows.Err()
}

// trajanje računa krivulju trajanja: koja je vrijednost dosegnuta ili
// premašena zadani postotak vremena. Ekstremi kažu koliko je najviše bilo,
// trajanje koliko je često bilo — a za obranu je drugo jednako važno.
func (r *ArhivaRepository) trajanje(ctx context.Context, nizID int64) ([]models.TrajanjeTocka, error) {
	postoci := []int{1, 5, 10, 30, 50, 70, 90, 95, 99}
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM ocitanja WHERE niz = ?`, nizID).Scan(&n); err != nil {
		return nil, err
	}
	if n < 100 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT rn, vrijednost FROM (
			SELECT vrijednost, row_number() OVER (ORDER BY vrijednost DESC) rn
			FROM ocitanja WHERE niz = ?
		) WHERE rn IN (SELECT value FROM json_each(?))`, nizID, redniBrojevi(n, postoci))
	if err != nil {
		return nil, fmt.Errorf("krivulja trajanja: %w", err)
	}
	defer rows.Close()
	poRednom := map[int]float64{}
	for rows.Next() {
		var rn int
		var v float64
		if err := rows.Scan(&rn, &v); err != nil {
			return nil, err
		}
		poRednom[rn] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []models.TrajanjeTocka
	for _, p := range postoci {
		if v, ok := poRednom[redniBroj(n, p)]; ok {
			out = append(out, models.TrajanjeTocka{Postotak: p, Vrijednost: v})
		}
	}
	return out, nil
}

func redniBroj(n, postotak int) int {
	i := n * postotak / 100
	if i < 1 {
		return 1
	}
	if i > n {
		return n
	}
	return i
}

func redniBrojevi(n int, postoci []int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, p := range postoci {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d", redniBroj(n, p))
	}
	b.WriteByte(']')
	return b.String()
}

// Profili vraća snimke poprečnog profila korita, najnoviji prvi.
func (r *ArhivaRepository) Profili(ctx context.Context, letva string) ([]models.ProfilKorita, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, datum, COALESCE(vodostaj,0), COALESCE(kota_nule,0)
		FROM profili WHERE letva = ? ORDER BY datum DESC`, letva)
	if err != nil {
		return nil, fmt.Errorf("profili korita: %w", err)
	}
	defer rows.Close()
	var out []models.ProfilKorita
	for rows.Next() {
		var p models.ProfilKorita
		if err := rows.Scan(&p.ID, &p.Datum, &p.Vodostaj, &p.KotaNule); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		t, err := r.db.QueryContext(ctx, `SELECT stacionaza, visina FROM profil_tocke
			WHERE profil = ? ORDER BY stacionaza`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for t.Next() {
			var x models.TockaProfila
			if err := t.Scan(&x.Stacionaza, &x.Visina); err != nil {
				t.Close()
				return nil, err
			}
			out[i].Tocke = append(out[i].Tocke, x)
		}
		t.Close()
	}
	return out, nil
}

// Krivulje vraća HQ krivulje letve, najnovija prva.
func (r *ArhivaRepository) Krivulje(ctx context.Context, letva string) ([]models.HQKrivulja, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, letva, vrijedi_od, vrijedi_do, a, b, h0,
		prijelom, a2, b2, mjerenja, odstupanje, napomena
		FROM hq_krivulje WHERE letva = ? ORDER BY vrijedi_od DESC`, letva)
	if err != nil {
		return nil, fmt.Errorf("krivulje protoka: %w", err)
	}
	defer rows.Close()
	var out []models.HQKrivulja
	for rows.Next() {
		var k models.HQKrivulja
		var prijelom sql.NullInt64
		if err := rows.Scan(&k.ID, &k.Letva, &k.VrijediOd, &k.VrijediDo, &k.A, &k.B, &k.H0,
			&prijelom, &k.A2, &k.B2, &k.Mjerenja, &k.Odstupanje, &k.Napomena); err != nil {
			return nil, err
		}
		if prijelom.Valid {
			cm := int(prijelom.Int64)
			k.PrijelomCm = &cm
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Zadnje vraća zadnju vrijednost niza i njezin trenutak.
func (r *ArhivaRepository) Zadnje(ctx context.Context, nizID int64) (float64, time.Time, bool) {
	if r == nil {
		return 0, time.Time{}, false
	}
	var t int64
	var v float64
	err := r.db.QueryRowContext(ctx, `SELECT vrijeme, vrijednost FROM ocitanja
		WHERE niz = ? ORDER BY vrijeme DESC LIMIT 1`, nizID).Scan(&t, &v)
	if err != nil {
		return 0, time.Time{}, false
	}
	return v, time.Unix(t, 0).UTC(), true
}

// Raspon vraća vrijednosti niza u razdoblju, poredane po vremenu.
func (r *ArhivaRepository) Raspon(ctx context.Context, nizID int64, od, do time.Time) ([]models.HidroTocka, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT vrijeme, vrijednost FROM ocitanja
		WHERE niz = ? AND vrijeme BETWEEN ? AND ? ORDER BY vrijeme`, nizID, od.Unix(), do.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.HidroTocka
	for rows.Next() {
		var t int64
		var v float64
		if err := rows.Scan(&t, &v); err != nil {
			return nil, err
		}
		out = append(out, models.HidroTocka{Kad: time.Unix(t, 0).UTC(), Vrijednost: v})
	}
	return out, rows.Err()
}

// SpojDosezi opisuje spojene nizove letve: što pokrivaju i iz čega su
// sastavljeni. Spajanje je odluka programa, pa mora biti vidljivo od čega je
// niz sklopljen — inače je to samo broj bez podrijetla.
func (r *ArhivaRepository) SpojDosezi(ctx context.Context, letva string) ([]models.SpojDoseg, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT velicina, korak, count(*),
		       date(min(vrijeme),'unixepoch'), date(max(vrijeme),'unixepoch')
		FROM spoj WHERE letva = ? GROUP BY velicina, korak`, letva)
	if err != nil {
		return nil, fmt.Errorf("spojeni nizovi: %w", err)
	}
	defer rows.Close()
	var out []models.SpojDoseg
	for rows.Next() {
		d := models.SpojDoseg{Letva: letva}
		if err := rows.Scan(&d.Velicina, &d.Korak, &d.Zapisa, &d.Od, &d.Do); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		dio, err := r.db.QueryContext(ctx, `
			SELECT izvor, vrsta, count(*), date(min(vrijeme),'unixepoch'), date(max(vrijeme),'unixepoch'), max(tocnost)
			FROM spoj WHERE letva=? AND velicina=? AND korak=?
			GROUP BY izvor, vrsta ORDER BY min(vrijeme)`, letva, out[i].Velicina, out[i].Korak)
		if err != nil {
			return nil, err
		}
		for dio.Next() {
			var x models.SpojDio
			if err := dio.Scan(&x.Izvor, &x.Vrsta, &x.Zapisa, &x.Od, &x.Do, &x.Tocnost); err != nil {
				dio.Close()
				return nil, err
			}
			out[i].Dijelovi = append(out[i].Dijelovi, x)
		}
		dio.Close()
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Velicina != out[b].Velicina {
			return rangVelicine(out[a].Velicina) < rangVelicine(out[b].Velicina)
		}
		return out[a].Korak < out[b].Korak
	})
	return out, nil
}

// SpojZadnje vraća zadnju vrijednost spojenog niza — brzi podatak, s izvorom
// i odstupanjem.
func (r *ArhivaRepository) SpojZadnje(ctx context.Context, letva, velicina, korak string) (*models.SpojenaVrijednost, error) {
	if r == nil {
		return nil, nil
	}
	var v models.SpojenaVrijednost
	var kad int64
	err := r.db.QueryRowContext(ctx, `SELECT vrijeme, vrijednost, izvor, vrsta, tocnost FROM spoj
		WHERE letva=? AND velicina=? AND korak=? ORDER BY vrijeme DESC LIMIT 1`,
		letva, velicina, korak).Scan(&kad, &v.Vrijednost, &v.Izvor, &v.Vrsta, &v.Tocnost)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.Kad = time.Unix(kad, 0).UTC()
	return &v, nil
}

// SpojRaspon vraća vrijednosti spojenog niza u razdoblju, novije prvo. Svaka
// nosi izvor i odstupanje, pa se u tablici vidi odakle je koji redak.
func (r *ArhivaRepository) SpojRaspon(ctx context.Context, letva, velicina, korak string,
	od, do time.Time, granica, odmak int) ([]models.SpojenaVrijednost, error) {
	if r == nil {
		return nil, nil
	}
	// Satna godina ima 8.760 vrijednosti; niža granica tiho bi odrezala
	// početak godine, jer se čita od najnovije prema starijoj.
	if granica <= 0 || granica > 20000 {
		granica = 400
	}
	if odmak < 0 {
		odmak = 0
	}
	rows, err := r.db.QueryContext(ctx, `SELECT vrijeme, vrijednost, izvor, vrsta, tocnost FROM spoj
		WHERE letva=? AND velicina=? AND korak=? AND vrijeme BETWEEN ? AND ?
		ORDER BY vrijeme DESC LIMIT ? OFFSET ?`, letva, velicina, korak, od.Unix(), do.Unix(), granica, odmak)
	if err != nil {
		return nil, fmt.Errorf("spojeni niz: %w", err)
	}
	defer rows.Close()
	var out []models.SpojenaVrijednost
	for rows.Next() {
		var v models.SpojenaVrijednost
		var kad int64
		if err := rows.Scan(&kad, &v.Vrijednost, &v.Izvor, &v.Vrsta, &v.Tocnost); err != nil {
			return nil, err
		}
		v.Kad = time.Unix(kad, 0).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

// SpojGodine vraća godine koje spojeni niz pokriva, najnovija prva.
func (r *ArhivaRepository) SpojGodine(ctx context.Context, letva, velicina, korak string) ([]int, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT strftime('%Y', vrijeme, 'unixepoch')
		FROM spoj WHERE letva=? AND velicina=? AND korak=? ORDER BY 1 DESC`, letva, velicina, korak)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		var y int
		fmt.Sscanf(g, "%d", &y)
		out = append(out, y)
	}
	return out, rows.Err()
}

// Sazetak vraća jedan redak po veličini: razdoblje, srednjak i krajnosti s
// datumima, računato iz spojenog dnevnog niza. To je pregled koji se gleda
// prvi; razrada po godinama dolazi poslije.
func (r *ArhivaRepository) Sazetak(ctx context.Context, letva string) ([]models.SazetakVelicine, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT velicina, count(*), date(min(vrijeme),'unixepoch'), date(max(vrijeme),'unixepoch'),
		       avg(vrijednost), min(vrijednost), max(vrijednost), sum(vrijednost)
		FROM spoj WHERE letva = ? AND korak = 'dnevni' GROUP BY velicina`, letva)
	if err != nil {
		return nil, fmt.Errorf("sažetak po veličinama: %w", err)
	}
	defer rows.Close()
	var out []models.SazetakVelicine
	for rows.Next() {
		var s models.SazetakVelicine
		var zbroj float64
		if err := rows.Scan(&s.Velicina, &s.Zapisa, &s.Od, &s.Do, &s.Srednjak,
			&s.Min, &s.Max, &zbroj); err != nil {
			return nil, err
		}
		if models.SeZbraja(s.Velicina) {
			s.ZbrojIma, s.Zbroj = true, zbroj
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// datumi krajnosti, po veličini
	for i := range out {
		_ = r.db.QueryRowContext(ctx, `SELECT date(vrijeme,'unixepoch') FROM spoj
			WHERE letva=? AND korak='dnevni' AND velicina=? ORDER BY vrijednost DESC, vrijeme LIMIT 1`,
			letva, out[i].Velicina).Scan(&out[i].MaxNa)
		_ = r.db.QueryRowContext(ctx, `SELECT date(vrijeme,'unixepoch') FROM spoj
			WHERE letva=? AND korak='dnevni' AND velicina=? ORDER BY vrijednost ASC, vrijeme LIMIT 1`,
			letva, out[i].Velicina).Scan(&out[i].MinNa)
	}
	sort.SliceStable(out, func(a, b int) bool {
		return rangVelicine(out[a].Velicina) < rangVelicine(out[b].Velicina)
	})
	return out, nil
}

// SpojBroj vraća koliko vrijednosti spojeni niz ima u razdoblju — za listanje,
// da se ne mora dohvatiti cijela godina da bi se znalo koliko je ima.
func (r *ArhivaRepository) SpojBroj(ctx context.Context, letva, velicina, korak string, od, do time.Time) (int, error) {
	if r == nil {
		return 0, nil
	}
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM spoj
		WHERE letva=? AND velicina=? AND korak=? AND vrijeme BETWEEN ? AND ?`,
		letva, velicina, korak, od.Unix(), do.Unix()).Scan(&n)
	return n, err
}
