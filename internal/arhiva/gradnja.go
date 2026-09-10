// Paket arhiva gradi i osvježava arhivsku bazu hidroloških nizova iz datoteka
// u vodostaji/.
//
// Arhiva je odvojena od gocop.db namjerno. Povijesni niz se ne uređuje, nitko
// ga ne ispravlja rukom i uvijek se može ponovno napraviti iz datoteka — pa mu
// ne treba knjiga verzija ni sinkronizacija. Bez toga jedno očitanje stoji oko
// 20 bajta umjesto 1.400, a milijuni satnih zapisa postaju izvedivi.
//
// Između čvorova se prenosi kao datoteka ili se preuzme na zahtjev; u redovnu
// razmjenu verzija ide samo katalog nizova, ne i njihov sadržaj.
//
// Gradnja stoji ovdje, a ne u alatu, jer je zovu oboje: alat pri velikom uvozu
// i program kad netko upiše nova očitanja. Dvije izvedbe istog posla razišle bi
// se prvom izmjenom.
package arhiva

import (
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"

	_ "modernc.org/sqlite"
)

// dopuniShemu dodaje stupce koji su nastali nakon prvih arhiva. CREATE TABLE
// IF NOT EXISTS ih ne dodaje u postojeću tablicu, a arhiva se ne gradi iznova
// zbog jednog stupca — 471 MB se ne prepisuje bez potrebe.
func dopuniShemu(db *sql.DB) error {
	if _, err := db.Exec(shemaIzvora); err != nil {
		return fmt.Errorf("tablica izvora: %w", err)
	}
	stupci := []struct{ tablica, stupac, opis string }{
		{"nizovi", "napomena", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range stupci {
		var ima int
		if err := db.QueryRow(
			`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`,
			c.tablica, c.stupac).Scan(&ima); err != nil {
			return fmt.Errorf("provjera stupca %s.%s: %w", c.tablica, c.stupac, err)
		}
		if ima > 0 {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
			c.tablica, c.stupac, c.opis)); err != nil {
			return fmt.Errorf("dodavanje stupca %s.%s: %w", c.tablica, c.stupac, err)
		}
	}
	return upisiZadaneIzvore(db)
}

// shemaIzvora stoji odvojeno od ostatka jer se dodaje i u arhive koje su
// nastale prije nje.
const shemaIzvora = `
CREATE TABLE IF NOT EXISTS izvori (
	naziv    TEXT PRIMARY KEY,
	tocnost  REAL    NOT NULL DEFAULT 20,
	red      INTEGER NOT NULL DEFAULT 900,
	ukljucen INTEGER NOT NULL DEFAULT 0,
	napomena TEXT    NOT NULL DEFAULT ''
);`

// Shema arhive. Vrijeme je broj sekundi od 1970. u UTC-u, jer tekstualni
// vremenski žig na sedam milijuna redaka stoji više nego sam podatak.
const shema = `
CREATE TABLE IF NOT EXISTS nizovi (
	id        INTEGER PRIMARY KEY,
	zona      TEXT NOT NULL DEFAULT '',   -- vremenska zona izvora prije pretvorbe
	sliv      TEXT NOT NULL,
	letva     TEXT NOT NULL,
	izvor     TEXT NOT NULL,
	velicina  TEXT NOT NULL,   -- vodostaj | protok | temperatura
	vrsta     TEXT NOT NULL,   -- satni | dvokratni | jutarnji | srednjak | dnevni
	po_danu   INTEGER NOT NULL DEFAULT 0, -- 1 kad izvor daje samo datum, bez sata
	od        TEXT NOT NULL DEFAULT '',
	do_       TEXT NOT NULL DEFAULT '',
	zapisa    INTEGER NOT NULL DEFAULT 0,
	otisak    TEXT NOT NULL DEFAULT '',   -- sadržajni otisak, za provjeru pri preuzimanju
	datoteke  TEXT NOT NULL DEFAULT '',
	osvjezeno TEXT NOT NULL DEFAULT '',
	-- Ograda uz niz: što se o njemu zna, a iz brojki se ne vidi. Zaleđen
	-- mjerač, sumnjive zimske vrijednosti, prekid u mjerenju. Putuje s
	-- podacima, da se ne otkriva iznova na svakom čvoru.
	napomena  TEXT NOT NULL DEFAULT '',
	UNIQUE(letva, izvor, velicina, vrsta)
);
-- Spojeni niz: jedna vrijednost po trenutku, uzeta iz najboljeg izvora koji
-- je taj trenutak pokrio. Uz svaku stoji odakle je i kolika joj je točnost, pa
-- se brzi podatak može uzeti bez razmišljanja, a podrijetlo se ne gubi.
CREATE TABLE IF NOT EXISTS spoj (
	letva      TEXT NOT NULL,
	velicina   TEXT NOT NULL,
	korak      TEXT NOT NULL,   -- satni | dnevni
	vrijeme    INTEGER NOT NULL,
	vrijednost REAL NOT NULL,
	izvor      TEXT NOT NULL,
	vrsta      TEXT NOT NULL,   -- trenutna | srednjak | jutarnji
	tocnost    REAL NOT NULL,   -- ± u jedinici veličine, 68 % vrijednosti
	PRIMARY KEY (letva, velicina, korak, vrijeme)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS profili (
	id       INTEGER PRIMARY KEY,
	letva    TEXT NOT NULL,
	datum    TEXT NOT NULL,          -- kad je korito snimljeno
	vodostaj INTEGER,                -- vodostaj pri snimanju, cm
	kota_nule REAL,
	-- Koliko dodati stacionaži da snimka legne na zajedničku mrežu. Snimke
	-- se s godinama iznova stacioniraju: Batinina iz 2020. počinje 104,5 m
	-- desno od one iz 2010. Bez poravnanja se ne mogu ni usporediti ni spojiti.
	pomak_m REAL NOT NULL DEFAULT 0,
	UNIQUE(letva, datum)
);
CREATE TABLE IF NOT EXISTS profil_tocke (
	profil     INTEGER NOT NULL REFERENCES profili(id) ON DELETE CASCADE,
	stacionaza REAL NOT NULL,        -- m od lijeve obale (tako je označeno na listovima HIS-2000)
	visina     REAL NOT NULL,        -- apsolutna kota, m
	PRIMARY KEY (profil, stacionaza)
) WITHOUT ROWID;
-- Promjene kote nule letve. Očitanje je visina nad nulom, pa premještanje
-- nule pomiče cijeli niz prije tog datuma. Bez ovoga se vrijednosti s dviju
-- strana datuma tiho miješaju: dunavskim letvama od Paksa do Mohácsa nula je
-- 1.1.1943. spuštena za 2 m, i niz iz 1909. bez ispravka je toliko prenizak.
CREATE TABLE IF NOT EXISTS promjene_kote (
	letva    TEXT NOT NULL,
	datum    TEXT NOT NULL,   -- od tog dana vrijedi nova nula
	pomak_cm INTEGER NOT NULL,-- koliko dodati vrijednostima PRIJE tog dana
	izvor    TEXT NOT NULL DEFAULT '',
	napomena TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (letva, datum)
);
CREATE TABLE IF NOT EXISTS hq_krivulje (
	id         INTEGER PRIMARY KEY,
	letva      TEXT NOT NULL,
	vrijedi_od TEXT NOT NULL,
	vrijedi_do TEXT NOT NULL DEFAULT '',
	izvor      TEXT NOT NULL DEFAULT '',
	napomena   TEXT NOT NULL DEFAULT '',
	UNIQUE(letva, vrijedi_od)
);
-- Krivulja se sastoji od odsječaka: svaki vrijedi u svom rasponu vodostaja.
-- Tako je i DHMZ objavljuje, a isti zapis nosi i potenciju za nizove koje smo
-- sami preračunali ondje gdje službene krivulje nema.
CREATE TABLE IF NOT EXISTS hq_odsjecci (
	krivulja INTEGER NOT NULL REFERENCES hq_krivulje(id) ON DELETE CASCADE,
	od_cm    INTEGER NOT NULL,
	do_cm    INTEGER NOT NULL,
	oblik    TEXT NOT NULL DEFAULT 'polinom',
	p1       REAL NOT NULL,
	p2       REAL NOT NULL,
	p3       REAL NOT NULL,
	PRIMARY KEY (krivulja, od_cm)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS ocitanja (
	niz        INTEGER NOT NULL REFERENCES nizovi(id) ON DELETE CASCADE,
	vrijeme    INTEGER NOT NULL,
	vrijednost REAL NOT NULL,
	PRIMARY KEY (niz, vrijeme)
) WITHOUT ROWID;
-- Bez posebnog indeksa po vremenu: ključ (niz, vrijeme) već nosi svaki upit
-- oblika „daj mi ovaj niz u ovom razdoblju", a to je jedini oblik koji nam
-- treba. Zaseban indeks stajao bi 119 MB, gotovo koliko i sami podaci.
`

type niz struct {
	sliv, letva, izvor, velicina, vrsta string
	datoteke                            []string
}

// Zagreb je vremenska zona hrvatskih izvora.
var Zagreb = func() *time.Location {
	l, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return l
}()

// zonaIzvora govori u kojem je vremenu izvor zapisan.
//
// HIS-2000 i letva.voda.hr daju LOKALNI sat, onakav kakav pokazuje sat na
// zidu, sa zimskim i ljetnim pomakom — a preuzeti su u stupac nazvan
// vrijeme_utc. Mađarski vituki daje stvarni UTC, jer ga servis tako i vraća.
//
// Da se to ne ispravi, isti trenutak s dvije strane granice pada na različit
// sat: mjereno je da se Mohács i Batina najbolje poklapaju uz 6 sati zimi i
// 7 ljeti, a ta razlika od točno jednog sata nije hidrologija nego ovaj pomak.
// Pravo putovanje vala je oko 5 sati.
func zonaIzvora(izvor string) *time.Location {
	switch {
	case izvor == "vituki", izvor == "danubehis", strings.HasPrefix(izvor, "preracun-"):
		return time.UTC
	}
	return Zagreb
}

// Izvjestaj je što je gradnja napravila.
type Izvjestaj struct {
	Nizova   int
	Ocitanja int
	Spojenih int
}

// Izgradi čita datoteke iz koren/ i upisuje ih u arhivsku bazu. Prazna letva
// znači sve. Ispis ide u zapisi, ako je zadan.
func Izgradi(koren, baza, samo string, zapisi io.Writer) (Izvjestaj, error) {
	var iz Izvjestaj
	if zapisi == nil {
		zapisi = io.Discard
	}
	nizovi, err := popisi(koren, samo)
	if err != nil {
		return iz, err
	}
	if len(nizovi) == 0 {
		return iz, fmt.Errorf("nema nijednog niza za uvoz")
	}

	db, err := sql.Open("sqlite", baza+"?_pragma=journal_mode(WAL)&_pragma=synchronous(OFF)")
	if err != nil {
		return iz, err
	}
	defer db.Close()
	// Bez ovoga SQLite ne provodi ON DELETE CASCADE. Gradnja briše krivulje i
	// profile pa ih upisuje iznova, a djeca su ostajala — zatečena arhiva je
	// tako skupila 330 odsječaka i 164 točke bez roditelja. Kad se id poslije
	// ponovno dodijeli, novi upis naleti na te ostatke i padne.
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return iz, err
	}
	if _, err := db.Exec(shema); err != nil {
		return iz, err
	}
	if err := dopuniShemu(db); err != nil {
		return iz, err
	}
	promjene, err := promjeneKote(db, koren, zapisi)
	if err != nil {
		return iz, err
	}
	poravnanja, err := poravnanjaProfila(koren)
	if err != nil {
		return iz, err
	}
	if err := profili(db, koren, samo, poravnanja); err != nil {
		return iz, err
	}
	if err := krivulje(db, koren, samo); err != nil {
		return iz, err
	}

	kljucevi := make([]string, 0, len(nizovi))
	for k := range nizovi {
		kljucevi = append(kljucevi, k)
	}
	sort.Strings(kljucevi)
	for _, k := range kljucevi {
		n := nizovi[k]
		upisano, od, do, otisak, err := ubaci(db, n, promjene[n.letva])
		if err != nil {
			return iz, fmt.Errorf("%s: %w", k, err)
		}
		fmt.Fprintf(zapisi, "%-8s %-16s %-22s %-12s %-10s %8d  %s .. %s  %s\n",
			n.sliv, n.letva, n.izvor, n.velicina, n.vrsta, upisano, od, do, otisak[:8])
		iz.Ocitanja += upisano
	}
	iz.Nizova = len(nizovi)

	iz.Spojenih, err = spoji(db, samo)
	return iz, err
}

// popisi prolazi stablo i grupira datoteke u nizove. Jedan niz je jedna letva,
// jedan izvor, jedna veličina i jedna vrsta — bez obzira na koliko je godišnjih
// datoteka razlomljen.
func popisi(koren, samo string) (map[string]*niz, error) {
	out := map[string]*niz{}
	slivovi, err := os.ReadDir(koren)
	if err != nil {
		return nil, err
	}
	for _, s := range slivovi {
		if !s.IsDir() || strings.HasPrefix(s.Name(), "PRISTUP") {
			continue
		}
		letve, err := os.ReadDir(filepath.Join(koren, s.Name()))
		if err != nil {
			return nil, err
		}
		for _, l := range letve {
			if !l.IsDir() || (samo != "" && l.Name() != samo) {
				continue
			}
			d := filepath.Join(koren, s.Name(), l.Name())
			dats, err := os.ReadDir(d)
			if err != nil {
				return nil, err
			}
			for _, f := range dats {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".csv") {
					continue
				}
				dj := strings.Split(strings.TrimSuffix(f.Name(), ".csv"), "_")
				if len(dj) != 5 {
					fmt.Fprintf(os.Stderr, "  preskačem, naziv nije po dogovoru: %s\n", f.Name())
					continue
				}
				k := strings.Join([]string{l.Name(), dj[1], dj[2], dj[3]}, "|")
				if out[k] == nil {
					out[k] = &niz{sliv: s.Name(), letva: l.Name(), izvor: dj[1], velicina: dj[2], vrsta: dj[3]}
				}
				out[k].datoteke = append(out[k].datoteke, filepath.Join(d, f.Name()))
			}
		}
	}
	for _, n := range out {
		sort.Strings(n.datoteke)
	}
	return out, nil
}

type zapis struct {
	t int64
	v float64
}

func ubaci(db *sql.DB, n *niz, promjene []promjena) (int, string, string, string, error) {
	var zapisi []zapis
	poDanu := false
	for _, p := range n.datoteke {
		z, d, err := citaj(p, zonaIzvora(n.izvor), n.velicina)
		if err != nil {
			return 0, "", "", "", err
		}
		poDanu = poDanu || d
		zapisi = append(zapisi, z...)
	}
	// Svođenje na današnju kotu nule prije svega ostaloga: sve dalje —
	// spajanje, srednjaci, krajnosti — pretpostavlja jednu kotu kroz niz.
	if n.velicina == "vodostaj" {
		if svedeno, pomak := naKotu(zapisi, promjene); svedeno > 0 {
			fmt.Printf("%-8s %-16s %-22s svedeno na današnju kotu: %d zapisa (%+.0f cm)\n",
				"", n.letva, n.izvor, svedeno, pomak)
		}
	}
	// isti trenutak iz dvije datoteke: zadnja pročitana vrijedi
	sort.SliceStable(zapisi, func(i, j int) bool { return zapisi[i].t < zapisi[j].t })
	saz := make([]zapis, 0, len(zapisi))
	for i, z := range zapisi {
		if i > 0 && z.t == zapisi[i-1].t {
			saz[len(saz)-1] = z
			continue
		}
		saz = append(saz, z)
	}
	zapisi = saz
	if len(zapisi) == 0 {
		return 0, "", "", "", fmt.Errorf("nijedan čitljiv redak")
	}

	h := sha256.New()
	for _, z := range zapisi {
		fmt.Fprintf(h, "%d:%g\n", z.t, z.v)
	}
	otisak := hex.EncodeToString(h.Sum(nil))

	od := time.Unix(zapisi[0].t, 0).UTC().Format("2006-01-02")
	do := time.Unix(zapisi[len(zapisi)-1].t, 0).UTC().Format("2006-01-02")

	tx, err := db.Begin()
	if err != nil {
		return 0, "", "", "", err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(`SELECT id FROM nizovi WHERE letva=? AND izvor=? AND velicina=? AND vrsta=?`,
		n.letva, n.izvor, n.velicina, n.vrsta).Scan(&id)
	if err == sql.ErrNoRows {
		res, err := tx.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta) VALUES (?,?,?,?,?)`,
			n.sliv, n.letva, n.izvor, n.velicina, n.vrsta)
		if err != nil {
			return 0, "", "", "", err
		}
		id, _ = res.LastInsertId()
	} else if err != nil {
		return 0, "", "", "", err
	}

	// niz se gradi cijeli iz svojih datoteka, pa se stari sadržaj miče
	if _, err := tx.Exec(`DELETE FROM ocitanja WHERE niz = ?`, id); err != nil {
		return 0, "", "", "", err
	}
	st, err := tx.Prepare(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,?)`)
	if err != nil {
		return 0, "", "", "", err
	}
	defer st.Close()
	for _, z := range zapisi {
		if _, err := st.Exec(id, z.t, z.v); err != nil {
			return 0, "", "", "", err
		}
	}
	pd := 0
	if poDanu {
		pd = 1
	}
	if _, err := tx.Exec(`UPDATE nizovi SET sliv=?, zona=?, po_danu=?, od=?, do_=?, zapisa=?, otisak=?, datoteke=?, osvjezeno=? WHERE id=?`,
		n.sliv, zonaIzvora(n.izvor).String(), pd, od, do, len(zapisi), otisak,
		strings.Join(kratka(n.datoteke), " "),
		time.Now().UTC().Format(time.RFC3339), id); err != nil {
		return 0, "", "", "", err
	}
	return len(zapisi), od, do, otisak, tx.Commit()
}

func kratka(p []string) []string {
	out := make([]string, len(p))
	for i, x := range p {
		out[i] = filepath.Base(x)
	}
	return out
}

// citaj čita jednu datoteku iz vodostaji/. Prvi stupac je vrijeme_utc ili
// datum, drugi vrijednost; decimalni zarez je hrvatski zapis.
// promjeneKote učitava zabilježena premještanja nule i sprema ih u arhivu.
// Vraća ih po letvi, da se pri čitanju nizova mogu i primijeniti.
func promjeneKote(db *sql.DB, koren string, zapisi io.Writer) (map[string][]promjena, error) {
	put := filepath.Join(koren, "promjene-kote.csv")
	f, err := os.Open(put)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	sve, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("promjene kote: %w", err)
	}
	out := map[string][]promjena{}
	for i, r := range sve {
		if i == 0 || len(r) < 3 {
			continue
		}
		letva := strings.TrimSpace(strings.TrimPrefix(r[0], "\ufeff"))
		cm, err := strconv.Atoi(strings.TrimSpace(r[2]))
		if err != nil {
			return nil, fmt.Errorf("promjene kote, redak %d: pomak %q", i+1, r[2])
		}
		datum := strings.TrimSpace(r[1])
		t, err := time.Parse("2006-01-02", datum)
		if err != nil {
			return nil, fmt.Errorf("promjene kote, redak %d: datum %q", i+1, datum)
		}
		if _, err := db.Exec(`INSERT OR REPLACE INTO promjene_kote (letva, datum, pomak_cm, izvor, napomena)
			VALUES (?,?,?,?,?)`, letva, datum, cm, nth(r, 3), nth(r, 4)); err != nil {
			return nil, err
		}
		out[letva] = append(out[letva], promjena{do: t.UTC().Unix(), pomak: float64(cm)})
	}
	if len(out) > 0 {
		fmt.Fprintf(zapisi, "%-8s %-16s promjene kote nule: %d letvi\n", "", "", len(out))
	}
	return out, nil
}

// promjena je jedno premještanje nule: vrijednostima prije "do" dodaje se pomak.
type promjena struct {
	do    int64
	pomak float64
}

// naKotu svodi niz na današnju kotu nule. Očitanje je visina nad nulom, pa
// premještanje nule pomiče sve prije tog datuma; bez toga se vrijednosti s
// dviju strana datuma tiho miješaju.
func naKotu(z []zapis, p []promjena) (int, float64) {
	if len(p) == 0 {
		return 0, 0
	}
	n := 0
	var zadnji float64
	for i := range z {
		for _, pr := range p {
			if z[i].t < pr.do {
				z[i].v += pr.pomak
				zadnji = pr.pomak
				n++
			}
		}
	}
	return n, zadnji
}

// mogucaVrijednost odbacuje ono što fizički ne postoji. Telemetrija zna
// javiti −2270 cm; dno korita na Batini je na −764, pa takva vrijednost nije
// niska voda nego kvar. Odbacuje se pri gradnji, jer se poslije više ne zna
// je li brojka mjerenje ili šum.
func mogucaVrijednost(velicina string, v float64) bool {
	switch velicina {
	case "vodostaj":
		return v > -1500 && v < 2000 // cm; najdublje korito u nizu je -764
	case "protok":
		return v >= 0 && v < 100000 // m³/s
	case "temperatura":
		return v >= -2 && v <= 45 // °C
	case "koncentracija", "pronos":
		return v >= 0
	}
	return true
}

func citaj(put string, zona *time.Location, velicina string) ([]zapis, bool, error) {
	f, err := os.Open(put)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	glava, err := cr.Read()
	if err != nil {
		return nil, false, err
	}
	poDanu := len(glava) > 0 && strings.EqualFold(strings.TrimPrefix(glava[0], "\ufeff"), "datum")

	var out []zapis
	for i := 2; ; i++ {
		r, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("%s redak %d: %w", filepath.Base(put), i, err)
		}
		if len(r) < 2 || strings.TrimSpace(r[1]) == "" {
			continue
		}
		s := strings.TrimSpace(r[0])
		var t time.Time
		if len(s) >= 19 {
			// sat postoji, pa zona ima smisla: čita se u zoni izvora
			t, err = time.ParseInLocation("2006-01-02 15:04:05", s[:19], zona)
		} else {
			// samo datum: dan je dan, bez obzira na zonu
			t, err = time.Parse("2006-01-02", s[:10])
		}
		if err != nil {
			return nil, false, fmt.Errorf("%s redak %d: vrijeme %q: %w", filepath.Base(put), i, s, err)
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r[1]), ",", "."), 64)
		if err != nil {
			return nil, false, fmt.Errorf("%s redak %d: vrijednost %q: %w", filepath.Base(put), i, r[1], err)
		}
		if !mogucaVrijednost(velicina, v) {
			continue
		}
		out = append(out, zapis{t: t.UTC().Unix(), v: v})
	}
	return bezSiljaka(velicina, out), poDanu, nil
}

// bezSiljaka izbacuje pojedinačne ispade telemetrije. Prepoznaju se po tome
// što susjedi oko njih mirno stoje: 65, −640, 63 nije pad vodostaja nego kvar
// mjerila. Traže se oba susjeda, i to bliska u vremenu — vrijednost uz
// prazninu u nizu ne dira se, jer ondje ne znamo što je između.
func bezSiljaka(velicina string, z []zapis) []zapis {
	if velicina != "vodostaj" || len(z) < 3 {
		return z
	}
	const (
		blizu = 2 * 3600 // koliko susjed smije biti udaljen, sekundi
		sloga = 30.0     // koliko se susjedi smiju razlikovati međusobno, cm
		ispad = 150.0    // koliko vrijednost mora odskakati da bude ispad, cm
	)
	// Skok koji nijedna naša rijeka ne može napraviti u dva sata. Njime se
	// hvata i ispad uz prazninu u nizu, gdje drugog susjeda nema.
	const nemoguc = 300.0

	out := z[:0:0]
	izbaceno := 0
	for i := range z {
		v := z[i]
		var a, b *zapis
		if i > 0 && v.t-z[i-1].t <= blizu {
			a = &z[i-1]
		}
		if i < len(z)-1 && z[i+1].t-v.t <= blizu {
			b = &z[i+1]
		}
		siljak := false
		switch {
		case a != nil && b != nil:
			siljak = math.Abs(b.v-a.v) <= sloga && math.Abs(v.v-(a.v+b.v)/2) > ispad
		case a != nil:
			siljak = math.Abs(v.v-a.v) > nemoguc
		case b != nil:
			siljak = math.Abs(v.v-b.v) > nemoguc
		}
		if siljak {
			izbaceno++
			continue
		}
		out = append(out, v)
	}
	if izbaceno > 0 {
		fmt.Printf("%-8s %-16s izbačeno ispada telemetrije: %d\n", "", "", izbaceno)
	}
	return out
}

// profili učitava snimke poprečnog profila korita. Nisu vremenski niz nego
// oblik korita u jednom danu, pa idu u svoje tablice — ali u istu datoteku,
// jer arhiva mora putovati kao jedna cjelina.
// poravnanjaProfila učitava koliko koju snimku treba pomaknuti da legne na
// zajedničku stacionažu, po letvi i datumu.
func poravnanjaProfila(koren string) (map[string]float64, error) {
	f, err := os.Open(filepath.Join(koren, "poravnanje-profila.csv"))
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	sve, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("poravnanje profila: %w", err)
	}
	out := map[string]float64{}
	for i, r := range sve {
		if i == 0 || len(r) < 3 {
			continue
		}
		letva := strings.TrimSpace(strings.TrimPrefix(r[0], "\ufeff"))
		v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r[2]), ",", "."), 64)
		if err != nil {
			return nil, fmt.Errorf("poravnanje profila, redak %d: pomak %q", i+1, r[2])
		}
		out[letva+"|"+strings.TrimSpace(r[1])] = v
	}
	return out, nil
}

func profili(db *sql.DB, koren, samo string, poravnanja map[string]float64) error {
	puts, err := filepath.Glob(filepath.Join(koren, "*", "*", "profil", "*.csv"))
	if err != nil {
		return err
	}
	for _, p := range puts {
		letva := filepath.Base(filepath.Dir(filepath.Dir(p)))
		if samo != "" && letva != samo {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		datum, vod, kota, tocke, err := citajProfil(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		if len(tocke) == 0 {
			continue
		}
		res, err := db.Exec(`INSERT INTO profili (letva, datum, vodostaj, kota_nule, pomak_m) VALUES (?,?,?,?,?)
			ON CONFLICT(letva, datum) DO UPDATE SET vodostaj=excluded.vodostaj, kota_nule=excluded.kota_nule,
				pomak_m=excluded.pomak_m`,
			letva, datum, vod, kota, poravnanja[letva+"|"+datum])
		if err != nil {
			return err
		}
		var id int64
		if id, _ = res.LastInsertId(); id == 0 {
			if err := db.QueryRow(`SELECT id FROM profili WHERE letva=? AND datum=?`, letva, datum).Scan(&id); err != nil {
				return err
			}
		}
		if _, err := db.Exec(`DELETE FROM profil_tocke WHERE profil = ?`, id); err != nil {
			return err
		}
		for _, t := range tocke {
			if _, err := db.Exec(`INSERT OR REPLACE INTO profil_tocke (profil, stacionaza, visina) VALUES (?,?,?)`,
				id, t[0], t[1]); err != nil {
				return err
			}
		}
		fmt.Printf("%-8s %-16s profil korita %s   %d točaka, vodostaj %d cm\n", "", letva, datum, len(tocke), vod)
	}
	return nil
}

func citajProfil(f *os.File) (datum string, vodostaj int, kota float64, tocke [][2]float64, err error) {
	cr := csv.NewReader(f)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	cr.Comment = 0
	sve, err := cr.ReadAll()
	if err != nil {
		return "", 0, 0, nil, err
	}
	for _, r := range sve {
		if len(r) == 0 {
			continue
		}
		if strings.HasPrefix(r[0], "#") {
			// # poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80.45
			for _, d := range strings.Split(r[0], ",") {
				d = strings.TrimSpace(d)
				switch {
				case strings.HasPrefix(d, "mjereno "):
					datum = strings.TrimSpace(strings.TrimPrefix(d, "mjereno "))
				case strings.HasPrefix(d, "vodostaj pri mjerenju "):
					fmt.Sscanf(strings.TrimPrefix(d, "vodostaj pri mjerenju "), "%d", &vodostaj)
				case strings.HasPrefix(d, "kota nule "):
					fmt.Sscanf(strings.TrimPrefix(d, "kota nule "), "%f", &kota)
				}
			}
			continue
		}
		if len(r) < 2 || strings.HasPrefix(r[0], "stacionaza") {
			continue
		}
		s, e1 := strconv.ParseFloat(strings.ReplaceAll(r[0], ",", "."), 64)
		v, e2 := strconv.ParseFloat(strings.ReplaceAll(r[1], ",", "."), 64)
		if e1 != nil || e2 != nil {
			continue
		}
		tocke = append(tocke, [2]float64{s, v})
	}
	return datum, vodostaj, kota, tocke, nil
}

// krivulje učitava HQ krivulje s razdobljem valjanosti. Krivulja se povremeno
// iznova postavlja jer se korito mijenja, pa ih letva ima više.
func krivulje(db *sql.DB, koren, samo string) error {
	puts, err := filepath.Glob(filepath.Join(koren, "*", "*", "hq", "*_hq_krivulje.csv"))
	if err != nil {
		return err
	}
	for _, p := range puts {
		letva := filepath.Base(filepath.Dir(filepath.Dir(p)))
		if samo != "" && letva != samo {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		cr := csv.NewReader(f)
		cr.Comma = ';'
		cr.FieldsPerRecord = -1
		sve, err := cr.ReadAll()
		f.Close()
		if err != nil {
			return err
		}
		br := func(x string) float64 {
			v, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(x), ",", "."), 64)
			return v
		}
		cijeli := func(x string) int {
			v, _ := strconv.Atoi(strings.TrimSpace(x))
			return v
		}
		// Datoteka nosi po jedan redak za svaki odsječak; zaglavlje krivulje se
		// ponavlja. Krivulja se prvo obriše pa iznova složi, da uklonjeni
		// odsječak ne ostane visjeti.
		vidjeno := map[string]int64{}
		n := 0
		for i, r := range sve {
			if i == 0 || len(r) < 8 {
				continue
			}
			od, do := strings.TrimSpace(r[0]), strings.TrimSpace(r[1])
			id, ima := vidjeno[od]
			if !ima {
				if _, err := db.Exec(`DELETE FROM hq_krivulje WHERE letva=? AND vrijedi_od=?`, letva, od); err != nil {
					return err
				}
				res, err := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od, vrijedi_do, izvor, napomena)
					VALUES (?,?,?,?,?)`, letva, od, do, nth(r, 8), nth(r, 9))
				if err != nil {
					return err
				}
				id, _ = res.LastInsertId()
				vidjeno[od] = id
				n++
			}
			oblik := strings.TrimSpace(r[4])
			if oblik != models.OblikPotencija {
				oblik = models.OblikPolinom
			}
			if _, err := db.Exec(`INSERT OR REPLACE INTO hq_odsjecci
				(krivulja, od_cm, do_cm, oblik, p1, p2, p3) VALUES (?,?,?,?,?,?,?)`,
				id, cijeli(r[2]), cijeli(r[3]), oblik, br(r[5]), br(r[6]), br(r[7])); err != nil {
				return err
			}
		}
		fmt.Printf("%-8s %-16s HQ krivulje: %d\n", "", letva, n)
	}
	return nil
}

func nth(r []string, i int) string {
	if i < len(r) {
		return r[i]
	}
	return ""
}

// Izvor je jedna dojava: tko javlja, koliko mu se vjeruje i ulazi li u spoj.
// Stoji u arhivi, ne u kodu — dok je bio u kodu, razlika između "namjerno
// isključeno" i "zaboravljeno" nije postojala, pa je 1,67 milijuna zapisa iz
// pet izvora ležalo u bazi i nigdje se nije vidjelo.
type Izvor struct {
	Naziv    string
	Tocnost  float64 // ± u jedinici veličine, 68 % vrijednosti
	Red      int     // manji broj, veće povjerenje
	Ukljucen bool
	Napomena string
}

// zadaniIzvori je ono što je do sada stajalo u kodu, prepisano u tablicu.
// Točnosti su izmjerene usporedbom sa službeno ovjerenim nizom, na stotinama
// tisuća sati kroz devet letava — osim ondje gdje napomena kaže drukčije.
var zadaniIzvori = []Izvor{
	{"his2000", 0, 10, true, "referenca — po njoj su ostali izmjereni"},
	{"letva-dhmz", 1, 20, true, ""},
	{"cop", 3, 30, true, ""},
	{"letva-hv", 5, 40, true, "dobra većinu vremena; u zamrznutim razdobljima javlja istu vrijednost danima"},
	{"vituki", 5, 50, true, "točnost proglašena, ne izmjerena — nema preklapanja s ovjerenim nizom"},
	{"his2000-cs", 0, 11, false, "Donji Miholjac — odlučuje se kad dođe Drava"},
	{"his2000-spojeno", 0, 12, false, "Donji Miholjac — odlučuje se kad dođe Drava"},
	{"his2000-ukinuta-nizv", 0, 13, false, "Donji Miholjac — odlučuje se kad dođe Drava"},
	{"his2000-ukinuto", 0, 14, false, "Donji Miholjac — odlučuje se kad dođe Drava"},
}

// zadanaTocnost vrijedi za izvor kojeg u tablici nema. Preračun se prepoznaje
// po imenu jer ga tablica i ne treba: on ne ulazi u red povjerenja nego uvijek
// na kraj, ondje gdje mjerenja nema.
func zadanaTocnost(izvor string) float64 {
	if strings.HasSuffix(izvor, "-izvan") {
		// Rekonstrukcija ondje gdje odnos dviju letvi nikad nije izmjeren.
		// Za Batinu 7.1.1909. promašuje za oko 170 cm prema onome što daju
		// Bezdan i Apatin, pa se tako i vodi. Vidi docs/rekonstrukcija-nizova.md.
		return 150
	}
	if strings.HasPrefix(izvor, "preracun-") {
		return 14 // rekonstrukcija: 90 % unutar ±14 cm
	}
	return 20
}

// upisiZadaneIzvore puni tablicu prvi put i dopunjuje ju izvorima koji su se
// u međuvremenu pojavili u nizovima. Novi izvor ulazi **isključen**: bolje da
// čeka odluku nego da tiho promijeni brojeve po kojima se brani od poplave.
func upisiZadaneIzvore(db *sql.DB) error {
	for _, i := range zadaniIzvori {
		if _, err := db.Exec(`INSERT OR IGNORE INTO izvori (naziv, tocnost, red, ukljucen, napomena)
			VALUES (?,?,?,?,?)`, i.Naziv, i.Tocnost, i.Red, i.Ukljucen, i.Napomena); err != nil {
			return fmt.Errorf("upis izvora %s: %w", i.Naziv, err)
		}
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO izvori (naziv, tocnost, red, ukljucen, napomena)
		SELECT DISTINCT izvor, ?, 900, 0, 'novi izvor — uključiti ručno'
		FROM nizovi WHERE izvor NOT LIKE 'preracun-%'`, zadanaTocnost(""))
	return err
}

// citajIzvore vraća uključene izvore po redu povjerenja i točnost svih.
func citajIzvore(db *sql.DB) (red []string, tocnosti map[string]float64, err error) {
	rows, err := db.Query(`SELECT naziv, tocnost, ukljucen FROM izvori ORDER BY red, naziv`)
	if err != nil {
		return nil, nil, fmt.Errorf("čitanje izvora: %w", err)
	}
	defer rows.Close()
	tocnosti = map[string]float64{}
	for rows.Next() {
		var n string
		var t float64
		var ukljucen bool
		if err := rows.Scan(&n, &t, &ukljucen); err != nil {
			return nil, nil, err
		}
		tocnosti[n] = t
		if ukljucen {
			red = append(red, n)
		}
	}
	return red, tocnosti, rows.Err()
}

// spoji gradi dva niza po letvi i veličini — satni i dnevni — uzimajući svaku
// vrijednost iz najboljeg izvora koji je taj trenutak pokrio.
//
// Dnevni niz je srednjak, jer to znači "koliko je vode toga dana bilo". Gdje
// srednjaka nema pa se uzme jutarnje očitanje, to piše uz vrijednost: jutarnja
// vrijednost i dnevni srednjak razilaze se na naglom porastu i po više od
// metra, i ne smiju se tiho pomiješati.
func spoji(db *sql.DB, samo string) (int, error) {
	// Red povjerenja i točnosti čitaju se jednom, iz arhive. Isti paket mora
	// na svakom čvoru dati iste brojeve, pa postavke ne smiju ovisiti o tome
	// koju je verziju programa tko pokrenuo.
	redSpajanja, tocnosti, err := citajIzvore(db)
	if err != nil {
		return 0, err
	}
	q := `SELECT DISTINCT letva, velicina FROM nizovi`
	var args []any
	if samo != "" {
		q += ` WHERE letva = ?`
		args = append(args, samo)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return 0, err
	}
	type par struct{ letva, velicina string }
	var parovi []par
	for rows.Next() {
		var p par
		if err := rows.Scan(&p.letva, &p.velicina); err != nil {
			rows.Close()
			return 0, err
		}
		parovi = append(parovi, p)
	}
	rows.Close()

	ukupno := 0
	for _, p := range parovi {
		n, err := spojiJedan(db, p.letva, p.velicina, redSpajanja, tocnosti)
		if err != nil {
			return ukupno, fmt.Errorf("%s/%s: %w", p.letva, p.velicina, err)
		}
		ukupno += n
	}
	return ukupno, nil
}

type spojena struct {
	v       float64
	izvor   string
	vrsta   string
	tocnost float64
}

func spojiJedan(db *sql.DB, letva, velicina string, redSpajanja []string, tocnosti map[string]float64) (int, error) {
	tocnost := func(izvor string) float64 {
		if t, ok := tocnosti[izvor]; ok {
			return t
		}
		return zadanaTocnost(izvor)
	}
	// koji nizovi postoje i kojim korakom
	rows, err := db.Query(`SELECT id, izvor, vrsta FROM nizovi WHERE letva=? AND velicina=?`, letva, velicina)
	if err != nil {
		return 0, err
	}
	type niz struct {
		id    int64
		izvor string
		vrsta string
	}
	var nizovi []niz
	for rows.Next() {
		var n niz
		if err := rows.Scan(&n.id, &n.izvor, &n.vrsta); err != nil {
			rows.Close()
			return 0, err
		}
		nizovi = append(nizovi, n)
	}
	rows.Close()

	satni := map[int64]spojena{}
	dnevni := map[int64]spojena{}

	uzmi := func(cilj map[int64]spojena, n niz, vrsta string, poDanu bool) error {
		r, err := db.Query(`SELECT vrijeme, vrijednost FROM ocitanja WHERE niz = ?`, n.id)
		if err != nil {
			return err
		}
		defer r.Close()
		t := tocnost(n.izvor)
		for r.Next() {
			var kad int64
			var v float64
			if err := r.Scan(&kad, &v); err != nil {
				return err
			}
			if poDanu {
				kad = kad - kad%86400
			}
			if prije, ima := cilj[kad]; ima && prije.tocnost <= t {
				continue
			}
			cilj[kad] = spojena{v: v, izvor: n.izvor, vrsta: vrsta, tocnost: t}
		}
		return r.Err()
	}

	// satni: samo nizovi koji doista imaju sat
	for _, izvor := range redSpajanja {
		for _, n := range nizovi {
			if n.izvor == izvor && n.vrsta == "satni" {
				if err := uzmi(satni, n, "trenutna", false); err != nil {
					return 0, err
				}
			}
		}
	}
	// dnevni: prvo ovjereni srednjaci, pa srednjak izveden iz spojenog satnog,
	// pa jutarnja očitanja, pa rekonstrukcija
	for _, izvor := range redSpajanja {
		for _, n := range nizovi {
			if n.izvor == izvor && n.vrsta == "srednjak" {
				if err := uzmi(dnevni, n, "srednjak", true); err != nil {
					return 0, err
				}
			}
		}
	}
	if len(satni) > 0 {
		zbroj := map[int64]float64{}
		broj := map[int64]int{}
		najgora := map[int64]float64{}
		izvorDana := map[int64]string{}
		for kad, s := range satni {
			d := kad - kad%86400
			zbroj[d] += s.v
			broj[d]++
			if s.tocnost > najgora[d] {
				najgora[d] = s.tocnost
			}
			izvorDana[d] = s.izvor
		}
		for d, n := range broj {
			if n < 20 { // nepotpun dan ne daje srednjak
				continue
			}
			t := najgora[d]
			if prije, ima := dnevni[d]; ima && prije.tocnost <= t {
				continue
			}
			dnevni[d] = spojena{v: zbroj[d] / float64(n), izvor: izvorDana[d], vrsta: "srednjak", tocnost: t}
		}
	}
	for _, izvor := range redSpajanja {
		for _, n := range nizovi {
			if n.izvor == izvor && (n.vrsta == "jutarnji" || n.vrsta == "dnevni" || n.vrsta == "dvokratni") {
				if err := uzmi(dnevni, n, n.vrsta, true); err != nil {
					return 0, err
				}
			}
		}
	}
	// preračun ide na kraj, samo tamo gdje ničega drugoga nema
	for _, n := range nizovi {
		if strings.HasPrefix(n.izvor, "preracun-") {
			cilj, vrsta := dnevni, n.vrsta
			if n.vrsta == "satni" {
				cilj, vrsta = satni, "trenutna"
				if err := uzmi(cilj, n, vrsta, false); err != nil {
					return 0, err
				}
				continue
			}
			if err := uzmi(cilj, n, vrsta, true); err != nil {
				return 0, err
			}
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM spoj WHERE letva=? AND velicina=?`, letva, velicina); err != nil {
		return 0, err
	}
	st, err := tx.Prepare(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost)
		VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer st.Close()
	n := 0
	for korak, m := range map[string]map[int64]spojena{"satni": satni, "dnevni": dnevni} {
		for kad, s := range m {
			if _, err := st.Exec(letva, velicina, korak, kad, s.v, s.izvor, s.vrsta, s.tocnost); err != nil {
				return n, err
			}
			n++
		}
	}
	if n > 0 {
		fmt.Printf("%-8s %-16s %-22s %-12s spojeno %8d  (satnih %d, dnevnih %d)\n",
			"", letva, "→ spojeni niz", velicina, n, len(satni), len(dnevni))
	}
	return n, tx.Commit()
}
