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
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	gocopdb "gocop/internal/db"
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
	if _, err := db.Exec(shemaPrimljenih); err != nil {
		return fmt.Errorf("tablica primljenih izdanja: %w", err)
	}
	stupci := []struct{ tablica, stupac, opis string }{
		{"nizovi", "napomena", "TEXT NOT NULL DEFAULT ''"},
		{"izvori", "mapa", "TEXT NOT NULL DEFAULT ''"},
		{"profili", "crtaj", "INTEGER NOT NULL DEFAULT 1"},
		{"hq_odsjecci", "p4", "REAL NOT NULL DEFAULT 0"},
		{"profili", "sustav_kote", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range stupci {
		// Zatečena arhiva ne mora imati svaku tablicu; stupac se dodaje samo
		// onoj koja postoji, a ostale nastaju iz sheme pri prvoj gradnji.
		var imaTablicu int
		if err := db.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
			c.tablica).Scan(&imaTablicu); err != nil {
			return fmt.Errorf("provjera tablice %s: %w", c.tablica, err)
		}
		if imaTablicu == 0 {
			continue
		}
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
	mapa     TEXT    NOT NULL DEFAULT '',
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
	-- sustav visina u kojem su kote snimke; prazno = ne zna se
	sustav_kote TEXT NOT NULL DEFAULT '',
	-- Koliko dodati stacionaži da snimka legne na zajedničku mrežu. Snimke
	-- se s godinama iznova stacioniraju: Batinina iz 2020. počinje 104,5 m
	-- desno od one iz 2010. Bez poravnanja se ne mogu ni usporediti ni spojiti.
	pomak_m REAL NOT NULL DEFAULT 0,
	-- Snimka koja se ni poravnanjem ne da složiti s ostalima ostaje u arhivi
	-- kao podatak, ali ne ulazi u crtež korita, da ne prikazuje lažan oblik.
	crtaj INTEGER NOT NULL DEFAULT 1,
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
	-- zbrojni član potencije; polinom ga nema, pa mu je nula
	p4       REAL NOT NULL DEFAULT 0,
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
	// pravila su retci popisa kvarova za letvu ovog niza; losi su zapisi koje
	// su pri čitanju izdvojili, a odvojeni ono što je niz -kvar od izvornog
	// niza primio i upisuje uz svoje datoteke
	pravila        []pravilo
	losi, odvojeni []zapis
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
	case izvor == "vituki", izvor == "danubehis", strings.HasPrefix(izvor, "preracun-"), strings.HasPrefix(izvor, "openmeteo"):
		// Open-Meteo vraća sat kakav se zatraži; kišomjeri se preuzimaju u UTC
		return time.UTC
	}
	return Zagreb
}

// Izvjestaj je što je gradnja napravila.
type Izvjestaj struct {
	Nizova   int
	Ocitanja int
	Spojenih int
	Sirotani []Sirotan
	// MaknuteKrivulje su krivulje protoka kojih u datoteci krivulja letve
	// više nema, pa ih je gradnja maknula iz arhive.
	MaknuteKrivulje []MaknutaKrivulja
	// ZadrzaneKrivulje su krivulje protoka kojih u datoteci krivulja letve
	// nema, a gradnja ih nije maknula, jer je letva stigla paketom s drugog
	// čvora.
	ZadrzaneKrivulje []MaknutaKrivulja
	// NeupisaneKrivulje su krivulje protoka iz datoteke krivulja koje gradnja
	// nije upisala, jer je letva stigla paketom s drugog čvora, a arhiva od
	// istog dana ima drukčiju krivulju.
	NeupisaneKrivulje []MaknutaKrivulja
}

// Sirotan je niz koji stoji u arhivi, a datoteke iz koje je nastao više nema u
// stablu. Gradnja ga ne briše sama: izvor sa svojom mapom na vanjskom disku
// izgleda isto tako kad disk nije priključen, a tiho brisanje bi tad odnijelo
// godine podataka. Zato se samo javlja, a miče ga čovjek.
//
// Isto vrijedi za snimku korita kojoj je nestala datoteka (Odluke
// (13. 10. 2026.)): tada je Snimka true, Datum je dan snimke, a Zapisa broj
// njezinih točaka. Za krivulje protoka letve kojoj je nestala datoteka
// krivulja Krivulje je true, Zapisa je broj krivulja, a Datum početak
// valjanosti najstarije. Snimka čija se datoteka ne da pročitati ima Datoteku
// i Gresku: takva se ne miče, nego popravlja.
type Sirotan struct {
	Letva    string
	Izvor    string
	Velicina string
	Vrsta    string
	Zapisa   int
	USpoju   int
	Snimka   bool
	Krivulje bool
	Datum    string
	Datoteka string
	Greska   string
}

// Izgradi čita datoteke iz koren/ i upisuje ih u arhivsku bazu. Prazna letva
// znači sve. Ispis ide u zapisi, ako je zadan.
// Javljac je odredište ispisa koje uz retke zna primiti i korak: čime se
// gradnja bavi i koliki je dio toga gotov. Naredbenom retku to ne treba i ne
// mora to znati — njemu su dovoljni retci koje gradnja ionako piše. Stranica,
// koja mora nacrtati traku napretka, javljanje prima.
type Javljac interface {
	Korak(sto string, gotovo, ukupno int)
}

// javi šalje korak ako ga odredište zna primiti; inače ne radi ništa.
func javi(zapisi io.Writer, sto string, gotovo, ukupno int) {
	if j, ok := zapisi.(Javljac); ok {
		j.Korak(sto, gotovo, ukupno)
	}
}

func Izgradi(koren, baza, samo string, zapisi io.Writer) (Izvjestaj, error) {
	var iz Izvjestaj
	if zapisi == nil {
		zapisi = io.Discard
	}
	// Gradnja niz briše pa upisuje iznova. Dvije istodobne nad istom letvom
	// presreću se usred toga, a izdavanje koje naiđe usred gradnje pročita
	// polovičan sadržaj i zapiše njegov otisak u katalog kao pravo izdanje.
	sto := "gradnja letve " + samo
	if samo == "" {
		sto = "gradnja cijele arhive"
	}
	brava, err := Uzmi(baza, sto, "")
	if err != nil {
		return iz, err
	}
	defer brava.Pusti()
	if bila, opis := brava.Preuzeta(); bila {
		fmt.Fprintf(zapisi, "preuzeta brava prekinutog posla: %s — provjeriti je li ostao nered\n", opis)
	}
	// Gradnja piše ravno u arhivu koju čvor koristi. Bez trajnog upisa
	// nestanak struje usred gradnje mogao je pokvariti cijelu arhivu; na
	// Macu to ni sad nije sasvim isključeno (vidi TrajniUpis).
	db, err := sql.Open("sqlite", gocopdb.ZaPisanje(baza, "_pragma=journal_mode(WAL)"))
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

	// Popis datoteka nastaje tek sad, jer se mora znati ima li koji izvor
	// vlastito stablo. Zajedničko stablo daje sve, a izvor s vlastitom mapom
	// nadjačava ono što je o njemu ondje nađeno.
	javi(zapisi, "popisujem datoteke", 0, 0)
	nizovi, err := popisi(koren, samo)
	if err != nil {
		return iz, err
	}
	nedostupni, err := dodajIzVlastitihMapa(db, nizovi, samo, zapisi)
	if err != nil {
		return iz, err
	}
	if len(nizovi) == 0 {
		return iz, fmt.Errorf("nema nijednog niza za uvoz")
	}

	javi(zapisi, "čitam promjene kote nule", 0, 0)
	promjene, err := promjeneKote(db, koren, zapisi)
	if err != nil {
		return iz, err
	}
	javi(zapisi, "čitam poprečne profile", 0, 0)
	poravnanja, err := poravnanjaProfila(koren)
	if err != nil {
		return iz, err
	}
	if err := profili(db, koren, samo, poravnanja); err != nil {
		return iz, err
	}
	javi(zapisi, "čitam krivulje protoka", 0, 0)
	kg, err := krivulje(db, koren, samo)
	if err != nil {
		return iz, err
	}
	iz.MaknuteKrivulje, iz.ZadrzaneKrivulje, iz.NeupisaneKrivulje = kg.maknute, kg.zadrzane, kg.neupisane
	javiKrivuljeGradnje(zapisi, kg)

	javi(zapisi, "čitam popis kvarova", 0, 0)
	kvarovi, err := kvaroviGradnje(db, koren, nizovi, nedostupni)
	if err != nil {
		return iz, err
	}

	kljucevi := redGradnje(nizovi)
	for i, k := range kljucevi {
		n := nizovi[k]
		javi(zapisi, "upisujem "+n.letva+" · "+n.izvor+" · "+n.velicina, i, len(kljucevi))
		upisano, err := gradiNiz(db, nizovi, k, kvarovi[n.letva], promjene[n.letva], zapisi)
		if err != nil {
			return iz, fmt.Errorf("%s: %w", k, err)
		}
		iz.Ocitanja += upisano
	}
	iz.Nizova = len(nizovi)

	// Izvor koji se prvi put pojavio u datotekama upisuje se sada, kad su nizovi
	// već u bazi. Prvi prolaz tablice bio je prije njih, pa novi izvor dotad
	// nije imao odakle biti viđen — i čekao bi sljedeću gradnju da se uopće
	// pojavi na popisu.
	if err := upisiZadaneIzvore(db); err != nil {
		return iz, err
	}

	javi(zapisi, "spajam izvore u jedan niz", len(kljucevi), len(kljucevi))
	iz.Spojenih, err = spoji(db, samo)
	if err != nil {
		return iz, err
	}

	iz.Sirotani, err = bezDatoteke(db, koren, nizovi, samo, nedostupni)
	if err != nil {
		return iz, err
	}
	javiBezDatoteke(zapisi, iz.Sirotani)
	return iz, nil
}

// javiBezDatoteke piše u ispis gradnje nizove i snimke korita koji su ostali u
// arhivi bez datoteke u stablu.
func javiBezDatoteke(zapisi io.Writer, sirotani []Sirotan) {
	for _, o := range sirotani {
		fmt.Fprintf(zapisi, "%-8s %-16s %s\n", "", o.Letva, o.redakIspisa())
	}
}

// redakIspisa je redak ispisa gradnje za niz ili snimku bez datoteke, bez
// letve na početku.
func (o Sirotan) redakIspisa() string {
	if o.Greska != "" {
		return fmt.Sprintf("snimka korita u datoteci %s ne da se pročitati: %s", o.Datoteka, o.Greska)
	}
	if o.Krivulje {
		return fmt.Sprintf("krivulje protoka od %s, %d %s  SIROTAN — datoteke nema u stablu",
			o.Datum, o.Zapisa, models.UzBrojHR(o.Zapisa, "krivulja", "krivulje", "krivulja"))
	}
	if o.Snimka {
		return fmt.Sprintf("snimka korita %s, %d %s  SIROTAN — datoteke nema u stablu",
			o.Datum, o.Zapisa, models.UzBrojHR(o.Zapisa, "točka", "točke", "točaka"))
	}
	niz := fmt.Sprintf("%-22s %-12s %-10s %8d", o.Izvor, o.Velicina, o.Vrsta, o.Zapisa)
	if o.USpoju == 0 {
		return niz + "  SIROTAN — datoteke nema u stablu"
	}
	return fmt.Sprintf("%s  SIROTAN — datoteke nema u stablu, a %d %s %s u spojenom nizu", niz, o.USpoju,
		models.UzBrojHR(o.USpoju, "vrijednost", "vrijednosti", "vrijednosti"), models.UzBrojHR(o.USpoju, "je", "su", "je"))
}

// bezDatoteke vraća nizove, snimke korita i krivulje protoka koji stoje u
// arhivi bez datoteke u stablu. Snimke i krivulje idu na isti popis, jer i njih
// miče čovjek, a ne gradnja.
func bezDatoteke(db *sql.DB, koren string, nizovi map[string]*niz, samo string,
	nedostupni map[string]bool) ([]Sirotan, error) {
	out, err := nadiSirotane(db, nizovi, samo, nedostupni)
	if err != nil {
		return nil, err
	}
	snimke, err := snimkeBezDatoteke(db, koren, samo)
	if err != nil {
		return nil, err
	}
	krivulje, err := krivuljeBezDatoteke(db, koren, samo)
	if err != nil {
		return nil, err
	}
	return append(append(out, snimke...), krivulje...), nil
}

// krivuljeBezDatoteke traži letve kojima je iz stabla nestala datoteka
// krivulja, a arhiva njihove krivulje još ima (Odluke (13. 10. 2026.)). Kao i
// kod snimki, sudi samo o letvama koje u stablu imaju mapu. Letve koje su
// stigle paketom preskače: njihove krivulje nisu nastale iz ovog stabla, pa
// ni datoteka nije nestala.
func krivuljeBezDatoteke(db Izvrsitelj, koren, samo string) ([]Sirotan, error) {
	letve, err := Letve(koren)
	if err != nil {
		return nil, err
	}
	sDatotekom, err := letveSDatotekomKrivulja(koren)
	if err != nil {
		return nil, err
	}
	sve, err := krivuljeUArhivi(db, samo)
	if err != nil {
		return nil, err
	}
	var out []Sirotan
	for _, o := range sve {
		if letve[o.Letva] != "" && !sDatotekom[o.Letva] {
			out = append(out, o)
		}
	}
	return out, nil
}

// letveSDatotekomKrivulja vraća letve koje u stablu imaju datoteku krivulja.
func letveSDatotekomKrivulja(koren string) (map[string]bool, error) {
	puts, err := filepath.Glob(filepath.Join(koren, "*", "*", "hq", "*_hq_krivulje.csv"))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, p := range puts {
		out[filepath.Base(filepath.Dir(filepath.Dir(p)))] = true
	}
	return out, nil
}

// krivuljeUArhivi vraća, po letvi, koliko krivulja protoka arhiva ima i od
// kada vrijedi najstarija, bez letvi koje su stigle paketom.
func krivuljeUArhivi(db Izvrsitelj, samo string) ([]Sirotan, error) {
	q := `SELECT k.letva, count(*), min(k.vrijedi_od) FROM hq_krivulje k
		WHERE NOT EXISTS (SELECT 1 FROM primljena_izdanja p WHERE p.letva = k.letva)`
	var args []any
	if samo != "" {
		q += ` AND k.letva = ?`
		args = append(args, samo)
	}
	rows, err := db.Query(q+` GROUP BY k.letva ORDER BY k.letva`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sirotan
	for rows.Next() {
		o := Sirotan{Krivulje: true}
		if err := rows.Scan(&o.Letva, &o.Zapisa, &o.Datum); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// snimkeBezDatoteke traži snimke korita kojima u stablu nema datoteke. Sudi
// samo o letvama koje u stablu imaju mapu: snimke letve primljene paketom,
// bez mape u ovom stablu, ne javljaju se.
//
// Datoteka snimke koja se ne da pročitati ide na popis kao zaseban redak, a
// snimke te letve iz arhive ne: ne zna se koju od njih ta datoteka drži, pa bi
// gumb Makni mogao maknuti baš nju (Odluke (13. 10. 2026.)). Prije je takva
// datoteka sakrila cijeli popis.
func snimkeBezDatoteke(db *sql.DB, koren, samo string) ([]Sirotan, error) {
	letve, err := Letve(koren)
	if err != nil {
		return nil, err
	}
	uStablu, necitljive, err := snimkeUStablu(koren, samo)
	if err != nil {
		return nil, err
	}
	sve, err := snimkeUArhivi(db, samo)
	if err != nil {
		return nil, err
	}
	return append(snimkeBezSvojeDatoteke(sve, letve, uStablu, necitljive), necitljive...), nil
}

// snimkeBezSvojeDatoteke odabire snimke iz arhive kojima stablo nema
// datoteku: letva ima mapu u stablu, nijedna njezina datoteka snimke nije
// nečitljiva, a dana te snimke nema ni u jednoj.
func snimkeBezSvojeDatoteke(sve []Sirotan, letve map[string]string, uStablu map[string]bool, necitljive []Sirotan) []Sirotan {
	nejasne := map[string]bool{}
	for _, o := range necitljive {
		nejasne[o.Letva] = true
	}
	var out []Sirotan
	for _, o := range sve {
		if letve[o.Letva] != "" && !nejasne[o.Letva] && !uStablu[o.Letva+"|"+o.Datum] {
			out = append(out, o)
		}
	}
	return out
}

// snimkeUStablu vraća snimke korita koje stablo ima, kao „letva|datum”, i
// datoteke snimki koje se ne daju pročitati. Dan snimke čita se iz datoteke,
// kao i pri gradnji, a ne iz njezina imena.
func snimkeUStablu(koren, samo string) (map[string]bool, []Sirotan, error) {
	puts, err := filepath.Glob(filepath.Join(koren, "*", "*", "profil", "*.csv"))
	if err != nil {
		return nil, nil, err
	}
	out := map[string]bool{}
	var necitljive []Sirotan
	for _, p := range puts {
		letva := filepath.Base(filepath.Dir(filepath.Dir(p)))
		if samo != "" && letva != samo {
			continue
		}
		datum, err := datumSnimke(p)
		if err != nil {
			necitljive = append(necitljive, Sirotan{Letva: letva, Snimka: true, Datoteka: filepath.Base(p), Greska: err.Error()})
			continue
		}
		out[letva+"|"+datum] = true
	}
	return out, necitljive, nil
}

// datumSnimke čita dan snimke iz datoteke snimke korita.
func datumSnimke(put string) (string, error) {
	b, err := os.ReadFile(put)
	if err != nil {
		return "", err
	}
	datum, _, _, _, _, err := citajProfil(bytes.NewReader(b))
	return datum, err
}

// snimkeUArhivi vraća snimke korita koje arhiva ima, s brojem točaka.
func snimkeUArhivi(db *sql.DB, samo string) ([]Sirotan, error) {
	q := `SELECT p.letva, p.datum, count(t.profil) FROM profili p
		LEFT JOIN profil_tocke t ON t.profil = p.id`
	var args []any
	if samo != "" {
		q += ` WHERE p.letva = ?`
		args = append(args, samo)
	}
	rows, err := db.Query(q+` GROUP BY p.id ORDER BY p.letva, p.datum`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sirotan
	for rows.Next() {
		o := Sirotan{Snimka: true}
		if err := rows.Scan(&o.Letva, &o.Datum, &o.Zapisa); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// nadiSirotane traži nizove koji su u arhivi a nemaju datoteku u stablu.
//
// Ne briše ih: izvor sa svojom mapom na vanjskom disku izgleda isto tako kad
// disk nije priključen, pa bi tiho brisanje odnijelo godine podataka. Izvori
// čija mapa nije bila dostupna ovdje se i izrijekom preskaču.
func nadiSirotane(db *sql.DB, nizovi map[string]*niz, samo string,
	nedostupni map[string]bool) ([]Sirotan, error) {
	uStablu := map[string]bool{}
	letve := map[string]bool{}
	for _, n := range nizovi {
		uStablu[n.letva+"|"+n.izvor+"|"+n.velicina+"|"+n.vrsta] = true
		letve[n.letva] = true
	}
	q := `SELECT letva, izvor, velicina, vrsta, zapisa FROM nizovi WHERE izvor <> 'spoj'`
	var args []any
	if samo != "" {
		q += ` AND letva = ?`
		args = append(args, samo)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sirotan
	for rows.Next() {
		var o Sirotan
		if err := rows.Scan(&o.Letva, &o.Izvor, &o.Velicina, &o.Vrsta, &o.Zapisa); err != nil {
			return nil, err
		}
		if uStablu[o.Letva+"|"+o.Izvor+"|"+o.Velicina+"|"+o.Vrsta] || nedostupni[o.Izvor] {
			continue
		}
		// Gradnja jedne letve ne vidi ostale, pa o njima ne smije ni suditi.
		if samo == "" && !letve[o.Letva] {
			continue
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		_ = db.QueryRow(`SELECT count(*) FROM spoj WHERE letva=? AND izvor=? AND velicina=?`,
			out[i].Letva, out[i].Izvor, out[i].Velicina).Scan(&out[i].USpoju)
	}
	return out, nil
}

// dodajIzVlastitihMapa dopunjuje popis nizovima iz stabala koja pojedini
// izvori drže za sebe. Mapa koje nema ne ruši gradnju: vanjski disk nije
// priključen ili je mapa preimenovana, a ostatak arhive s time nema veze —
// samo se zapiše da je preskočena.
func dodajIzVlastitihMapa(db *sql.DB, nizovi map[string]*niz, samo string, zapisi io.Writer) (map[string]bool, error) {
	nedostupni := map[string]bool{}
	izvori, err := Izvori(db)
	if err != nil {
		return nil, err
	}
	poMapi := map[string][]string{}
	for _, i := range izvori {
		if m := strings.TrimSpace(i.Mapa); m != "" {
			poMapi[m] = append(poMapi[m], i.Naziv)
		}
	}
	for mapa, imena := range poMapi {
		dodatni, err := popisi(mapa, samo)
		if err != nil {
			fmt.Fprintf(zapisi, "  preskačem mapu %s (%v): %s\n", mapa, err, strings.Join(imena, ", "))
			// Izvor čija mapa nije dostupna ne smije ispasti sirotan: disk
			// nije priključen, a podaci nisu nestali.
			for _, ime := range imena {
				nedostupni[ime] = true
			}
			continue
		}
		nadeno := 0
		for k, n := range dodatni {
			for _, ime := range imena {
				if n.izvor == ime {
					nizovi[k] = n
					nadeno++
					break
				}
			}
		}
		fmt.Fprintf(zapisi, "  vlastita mapa %s: %d %s za %s\n", mapa, nadeno,
			models.UzBrojHR(nadeno, "niz", "niza", "nizova"), strings.Join(imena, ", "))
	}
	return nedostupni, nil
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

// procitajNiz čita sve datoteke niza. Iz izvornog niza odmah odvaja u n.losi
// ono što je u popisu kvarova, a niz -kvar dobiva uz svoje datoteke i ono što
// mu je odvojeno iz izvornog.
//
// Kvarovi se odvajaju prije čišćenja ispada, ne poslije. Na Belišću ispad od
// 1550 cm počinje satom na putu gore — 497, pa 1258, pa 1550. Dok cijeli ispad
// stoji u nizu, taj sat izgleda kao njegov dio i ostaje; kad se ispad prvo
// makne, ostane sam usred 40 cm i čišćenje ga prepozna.
func procitajNiz(n *niz) (dobri []zapis, poDanu bool, err error) {
	for _, p := range n.datoteke {
		z, d, err := citaj(p, zonaIzvora(n.izvor), n.velicina)
		if err != nil {
			return nil, false, err
		}
		poDanu = poDanu || d
		z = odvojiKvarove(n, z)
		dobri = append(dobri, ocisti(n.velicina, z)...)
	}
	if len(n.odvojeni) > 0 {
		o := append([]zapis(nil), n.odvojeni...)
		sort.SliceStable(o, func(i, j int) bool { return o[i].t < o[j].t })
		dobri = append(dobri, ocisti(n.velicina, o)...)
	}
	return dobri, poDanu, nil
}

// makniPrazanKvar briše niz -kvar koji nema ni datoteku ni ijedan odvojeni
// zapis: popis kvarova u njemu više ništa ne pogađa, a takav niz nastaje samo
// iz popisa, pa s njim i nestaje.
func makniPrazanKvar(db *sql.DB, n *niz, zapisi io.Writer) error {
	res, err := db.Exec(`DELETE FROM nizovi WHERE letva=? AND izvor=? AND velicina=? AND vrsta=?`,
		n.letva, n.izvor, n.velicina, n.vrsta)
	if err != nil {
		return err
	}
	if b, _ := res.RowsAffected(); b > 0 {
		_, _ = fmt.Fprintf(zapisi, "%-8s %-16s %-22s %-12s %-10s maknut — popis kvarova u njemu više ništa ne pogađa\n",
			n.sliv, n.letva, n.izvor, n.velicina, n.vrsta)
	}
	return nil
}

func ubaci(db *sql.DB, n *niz, promjene []promjena) (int, string, string, string, error) {
	zapisi, poDanu, err := procitajNiz(n)
	if err != nil {
		return 0, "", "", "", err
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
	var upisi [][]any
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
		upisi = append(upisi, []any{letva, datum, cm, nth(r, 3), nth(r, 4)})
		out[letva] = append(out[letva], promjena{do: t.UTC().Unix(), pomak: float64(cm)})
	}
	if err := upisiPromjeneKote(db, upisi); err != nil {
		return nil, err
	}
	if len(out) > 0 {
		fmt.Fprintf(zapisi, "%-8s %-16s promjene kote nule: %d letvi\n", "", "", len(out))
	}
	return out, nil
}

// upisiPromjeneKote upisuje sve promjene kote nule iz datoteke u jednoj
// transakciji. Svaki redak su vrijednosti letva, datum, pomak u cm, izvor i
// napomena. Redak datoteke koji se ne da pročitati tako ne ostavi upisane
// one prije sebe.
func upisiPromjeneKote(db *sql.DB, upisi [][]any) error {
	return uTransakciji(db, func(q Izvrsitelj) error {
		for _, u := range upisi {
			if _, err := q.Exec(`INSERT OR REPLACE INTO promjene_kote (letva, datum, pomak_cm, izvor, napomena)
				VALUES (?,?,?,?,?)`, u...); err != nil {
				return err
			}
		}
		return nil
	})
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
	case "oborina", "snijeg":
		return v >= 0 && v < 1000 // mm u satu ili danu; najveća satna u nas je ispod 100
	case "visina-snijega":
		return v >= 0 && v < 2000 // cm
	case "temperatura-zraka":
		return v >= -50 && v <= 50 // °C
	case "kota":
		// apsolutna kota vodnog lica, cm n. m. — HEP tako vodi svoje letve na
		// Dravi; najniža je Donji Miholjac oko 90 m, najviša Formin oko 220 m
		return v > 5000 && v < 40000
	}
	return true
}

// citaj vraća retke datoteke kakvi jesu, bez nemogućih vrijednosti; ispade
// čisti procitajNiz, tek kad odvoji kvarove.
func citaj(put string, zona *time.Location, velicina string) ([]zapis, bool, error) {
	f, err := os.Open(put)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	cr := csvNiza(f)

	glava, err := cr.Read()
	if err != nil {
		return nil, false, err
	}
	poDanu := jeZaglavljeDana(glava)

	var out []zapis
	for i := 2; ; i++ {
		r, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("%s redak %d: %w", filepath.Base(put), i, err)
		}
		rn, ima, err := procitajRedak(r, zona, filepath.Base(put), i)
		if err != nil {
			return nil, false, err
		}
		if !ima || !mogucaVrijednost(velicina, rn.v) {
			continue
		}
		out = append(out, zapis{t: rn.t.UTC().Unix(), v: rn.v})
	}
	return out, poDanu, nil
}

// ocisti izbacuje ono što mjerilo nije moglo izmjeriti: kotu daleko od
// uobičajene i pojedinačne ispade telemetrije. Radi nad jednom datotekom,
// odnosno nad onim što je iz nje ostalo kad su kvarovi odvojeni.
func ocisti(velicina string, z []zapis) []zapis {
	return bezSiljaka(velicina, bezDalekih(velicina, z))
}

// csvNiza je čitač datoteke niza: točka-zarez, a redak smije imati i više
// stupaca od zaglavlja.
func csvNiza(r io.Reader) *csv.Reader {
	cr := csv.NewReader(r)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	return cr
}

// jeZaglavljeDana javlja je li niz po danu: prvi stupac zaglavlja je „datum”.
func jeZaglavljeDana(glava []string) bool {
	return len(glava) > 0 && strings.EqualFold(strings.TrimPrefix(glava[0], "\ufeff"), "datum")
}

// redakNiza je jedan pročitan redak datoteke niza.
type redakNiza struct {
	t      time.Time
	imaSat bool
	v      float64
}

// procitajRedak čita jedan redak datoteke niza. Isti je za gradnju i za
// dopunu (PostojeciRedci): dok su čitali svaki na svoj način, dopuna je tiho
// brisala retke koje je gradnja čitala bez problema. Redak bez vrijednosti
// nije greška nego se preskače (ima = false); vrijeme ili vrijednost koji se
// ne čitaju jesu, s imenom datoteke i brojem retka.
func procitajRedak(r []string, zona *time.Location, ime string, i int) (redakNiza, bool, error) {
	if len(r) < 2 || strings.TrimSpace(r[1]) == "" {
		return redakNiza{}, false, nil
	}
	s := strings.TrimSpace(r[0])
	t, imaSat, err := vrijemeNiza(s, zona)
	if err != nil {
		return redakNiza{}, false, fmt.Errorf("%s redak %d: vrijeme %q: %w", ime, i, s, err)
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r[1]), ",", "."), 64)
	if err != nil {
		return redakNiza{}, false, fmt.Errorf("%s redak %d: vrijednost %q: %w", ime, i, r[1], err)
	}
	return redakNiza{t: t, imaSat: imaSat, v: v}, true, nil
}

// vrijemeNiza čita prvi stupac retka niza i javlja ima li vrijeme sat. Sat se
// čita u zoni izvora, a sam datum je dan, bez obzira na zonu.
//
// Vrijeme bez sekundi („2024-03-01 07:00”) prije se čitalo kao sam datum, pa
// su se satne vrijednosti istog dana sažimale u jednu, a datum kraći od deset
// znakova („2024-3-1”) rušio je gradnju. U stablu su 13. 10. 2026. bila samo
// dva oblika, sa sekundama i sam datum; ostalo je ručno složeno.
func vrijemeNiza(s string, zona *time.Location) (time.Time, bool, error) {
	switch {
	case len(s) >= 19:
		t, err := time.ParseInLocation("2006-01-02 15:04:05", s[:19], zona)
		return t, true, err
	case len(s) > 10:
		t, err := time.ParseInLocation("2006-01-02 15:04", s, zona)
		return t, true, err
	}
	t, err := time.Parse("2006-01-02", s)
	return t, false, err
}

// bezDalekih izbacuje kote vodnog lica koje su predaleko od uobičajene
// razine letve. HEP-ov niz ima udvostručene i srezane vrijednosti — 368 m uz
// medijan od 190 m na repu akumulacije Varaždin, 111 m uz 149 m na gornjoj
// vodi HE Dubrava. Nijedna naša letva ne mijenja kotu za 15 posto od
// uobičajene; staro korito kod Svibovca, koje se pri preljevu diže i po
// devet metara, ostaje unutar toga.
func bezDalekih(velicina string, z []zapis) []zapis {
	if velicina != "kota" || len(z) < 100 {
		return z
	}
	v := make([]float64, len(z))
	for i := range z {
		v[i] = z[i].v
	}
	sort.Float64s(v)
	med := v[len(v)/2]
	out := z[:0:0]
	izbaceno := 0
	for _, x := range z {
		if x.v < med*0.85 || x.v > med*1.15 {
			izbaceno++
			continue
		}
		out = append(out, x)
	}
	if izbaceno > 0 {
		fmt.Printf("%-8s %-16s izbačeno kota daleko od uobičajene: %d\n", "", "", izbaceno)
	}
	return out
}

// bezSiljaka izbacuje pojedinačne ispade telemetrije. Prepoznaju se po tome
// što susjedi oko njih mirno stoje: 65, −640, 63 nije pad vodostaja nego kvar
// mjerila. Traže se oba susjeda, i to bliska u vremenu — vrijednost uz
// prazninu u nizu ne dira se, jer ondje ne znamo što je između.
// zbrajaSe javlja skuplja li se veličina po danu zbrojem, a ne srednjakom:
// oborina i snijeg su količine koje padnu, ne stanja koja se očitaju.
func zbrajaSe(velicina string) bool { return velicina == "oborina" || velicina == "snijeg" }

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
// Poravnanje je što datoteka kaže o jednoj snimci: koliko je pomaknuti i
// ulazi li uopće u crtež.
type Poravnanje struct {
	PomakM float64
	Crtaj  bool
}

func poravnanjaProfila(koren string) (map[string]Poravnanje, error) {
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
	out := map[string]Poravnanje{}
	for i, r := range sve {
		if i == 0 || len(r) < 3 {
			continue
		}
		letva := strings.TrimSpace(strings.TrimPrefix(r[0], "\ufeff"))
		v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r[2]), ",", "."), 64)
		if err != nil {
			return nil, fmt.Errorf("poravnanje profila, redak %d: pomak %q", i+1, r[2])
		}
		p := Poravnanje{PomakM: v, Crtaj: true}
		// Stupac „crtaj" je neobavezan; prazno znači da se snimka crta.
		if len(r) >= 4 {
			switch strings.ToLower(strings.TrimSpace(r[3])) {
			case "ne", "0", "false":
				p.Crtaj = false
			case "", "da", "1", "true":
			default:
				return nil, fmt.Errorf("poravnanje profila, redak %d: crtaj %q (očekivano da ili ne)", i+1, r[3])
			}
		}
		out[letva+"|"+strings.TrimSpace(r[1])] = p
	}
	return out, nil
}

func profili(db *sql.DB, koren, samo string, poravnanja map[string]Poravnanje) error {
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
		datum, vod, kota, sustavKote, tocke, err := citajProfil(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		if len(tocke) == 0 {
			continue
		}
		por, imaPor := poravnanja[letva+"|"+datum]
		if !imaPor {
			por = Poravnanje{Crtaj: true}
		}
		s := snimkaKorita{letva: letva, datum: datum, sustavKote: sustavKote, vodostaj: vod, kota: kota, por: por, tocke: tocke}
		if err := uTransakciji(db, s.upisi); err != nil {
			return err
		}
		oznaka := ""
		if !por.Crtaj {
			oznaka = "  — ne crta se"
		} else if por.PomakM != 0 {
			oznaka = fmt.Sprintf("  — pomak %+.1f m", por.PomakM)
		}
		fmt.Printf("%-8s %-16s profil korita %s   %d točaka, vodostaj %d cm%s\n", "", letva, datum, len(tocke), vod, oznaka)
	}
	return nil
}

// snimkaKorita je jedna snimka poprečnog profila iz datoteke, s točkama.
type snimkaKorita struct {
	letva, datum, sustavKote string
	vodostaj                 int
	kota                     float64
	por                      Poravnanje
	tocke                    [][2]float64
}

// upisi upisuje snimku i njezine točke. Profil ima i stotinjak točaka, pa
// ga gradnja upisuje u jednoj transakciji (uTransakciji), a ne točku po točku.
func (s snimkaKorita) upisi(q Izvrsitelj) error {
	crtaj := 1
	if !s.por.Crtaj {
		crtaj = 0
	}
	// Id se traži kroz RETURNING, ne kroz LastInsertId. Kad upsert ode u
	// UPDATE, SQLite ne dira last_insert_rowid nego vraća onaj od prošlog
	// upisa na toj vezi — a to je ovdje bila promjena kote nule, iz druge
	// tablice. Točke korita tako su odlazile pod tuđi broj: bez provjere
	// stranih ključeva tiho, u siročad, a s provjerom gradnja padne.
	var id int64
	if err := q.QueryRow(`INSERT INTO profili (letva, datum, vodostaj, kota_nule, pomak_m, crtaj, sustav_kote) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(letva, datum) DO UPDATE SET vodostaj=excluded.vodostaj, kota_nule=excluded.kota_nule,
		sustav_kote=excluded.sustav_kote,
			pomak_m=excluded.pomak_m, crtaj=excluded.crtaj
		RETURNING id`,
		s.letva, s.datum, s.vodostaj, s.kota, s.por.PomakM, crtaj, s.sustavKote).Scan(&id); err != nil {
		return fmt.Errorf("profil %s %s: %w", s.letva, s.datum, err)
	}
	if _, err := q.Exec(`DELETE FROM profil_tocke WHERE profil = ?`, id); err != nil {
		return err
	}
	for _, t := range s.tocke {
		if _, err := q.Exec(`INSERT OR REPLACE INTO profil_tocke (profil, stacionaza, visina) VALUES (?,?,?)`,
			id, t[0], t[1]); err != nil {
			return err
		}
	}
	return nil
}

func citajProfil(f io.Reader) (datum string, vodostaj int, kota float64, sustavKote string, tocke [][2]float64, err error) {
	cr := csv.NewReader(f)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	cr.Comment = 0
	sve, err := cr.ReadAll()
	if err != nil {
		return "", 0, 0, "", nil, err
	}
	for _, r := range sve {
		if len(r) == 0 {
			continue
		}
		if strings.HasPrefix(r[0], "#") {
			// # poprečni profil korita, mjereno 2010-03-22, vodostaj pri mjerenju 174 cm, kota nule 80.45
			// Broj koji se ne da pročitati je greška: 0 cm i 0 m su stvarne
			// vrijednosti, pa bi se nečitljivo zaglavlje upisalo kao mjereno.
			// Decimalni zarez (HIS piše kotu i kao 85,21) postaje točka prije
			// rezanja po zarezu, inače bi kota bila 85 m, a sustav bi nestao.
			for _, d := range strings.Split(decimalniZarez.ReplaceAllString(r[0], "$1.$2"), ",") {
				d = strings.TrimSpace(d)
				var nijeBroj error
				switch {
				case strings.HasPrefix(d, "mjereno "):
					datum = strings.TrimSpace(strings.TrimPrefix(d, "mjereno "))
				case strings.HasPrefix(d, "vodostaj pri mjerenju "):
					_, nijeBroj = fmt.Sscanf(strings.TrimPrefix(d, "vodostaj pri mjerenju "), "%d", &vodostaj)
				case strings.HasPrefix(d, "kota nule "):
					kota, sustavKote, nijeBroj = kotaNule(strings.TrimPrefix(d, "kota nule "))
				}
				if nijeBroj != nil {
					return "", 0, 0, "", nil, fmt.Errorf("u zaglavlju snimke nije broj: „%s”", d)
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
	return datum, vodostaj, kota, sustavKote, tocke, nil
}

// decimalniZarez je zarez između dviju znamenki; dijelove zaglavlja snimke
// odvaja zarez iza kojeg ne stoji znamenka
var decimalniZarez = regexp.MustCompile(`([0-9]),([0-9])`)

// kotaNule čita kotu nule iz zaglavlja snimke korita; iza broja zna stajati
// sustav: „121.55 TRST". Snimci bez kote goCOP uz odabrani sustav piše prazno
// mjesto za broj („kota nule  HVRS71”). Takva kota ostaje 0, kao i dosad, a
// sustav se pamti.
func kotaNule(ostatak string) (float64, string, error) {
	if dj := strings.Fields(ostatak); strings.HasPrefix(ostatak, " ") && len(dj) > 0 {
		return 0, dj[0], nil
	}
	var kota float64
	if _, err := fmt.Sscanf(ostatak, "%f", &kota); err != nil {
		return 0, "", err
	}
	sustav := ""
	if dj := strings.Fields(ostatak); len(dj) > 1 {
		sustav = dj[1]
	}
	return kota, sustav, nil
}

// krivulje učitava HQ krivulje s razdobljem valjanosti. Krivulja se povremeno
// iznova postavlja jer se korito mijenja, pa ih letva ima više.
//
// Krivulje letve prate njezinu datoteku: gradnja postavi točno krivulje iz
// datoteke, a one kojih u datoteci više nema makne i vrati, da ih izvještaj
// gradnje pokaže (Odluke (13. 10. 2026.)). Letvu bez datoteke krivulja gradnja
// ne dira. Letvi koja je stigla paketom s drugog čvora krivulje iz datoteke se
// upišu, ali se one kojih u datoteci nema ne miču, nego vraćaju kao zadržane,
// a krivulja iz datoteke ne zamjenjuje drukčiju krivulju istog početka
// valjanosti, nego se vraća kao neupisana.
func krivulje(db *sql.DB, koren, samo string) (krivuljeGradnje, error) {
	puts, err := filepath.Glob(filepath.Join(koren, "*", "*", "hq", "*_hq_krivulje.csv"))
	if err != nil {
		return krivuljeGradnje{}, err
	}
	var kg krivuljeGradnje
	uDatotekama := map[string]map[string]bool{}
	zatecene := zateceneKrivulje{}
	for _, p := range puts {
		letva := filepath.Base(filepath.Dir(filepath.Dir(p)))
		if samo != "" && letva != samo {
			continue
		}
		uDatoteci, neupisane, err := upisiDatotekuKrivulja(db, p, letva, zatecene)
		if err != nil {
			return krivuljeGradnje{}, err
		}
		uDatotekama[letva] = dodajKrivulje(uDatotekama[letva], uDatoteci)
		kg.neupisane = append(kg.neupisane, neupisane...)
	}
	kg.maknute, kg.zadrzane, err = makniIzvanDatoteka(db, uDatotekama)
	return kg, err
}

// procitanoIzKrivulja javlja grešku kad se iz datoteke krivulja nije dala
// pročitati nijedna krivulja, a datoteka nije samo zaglavlje: prazna datoteka,
// npr. nakon prekinutog prijenosa, ili ispod zaglavlja samo prazni redci.
// Redak s podacima koji se ne čita zaustavio je gradnju već prije
// (neispravanRedakKrivulja). Gradnja miče krivulje kojih u datoteci nema, pa
// bi takva datoteka iz arhive odnijela sve krivulje letve. Samo zaglavlje
// znači da letva krivulja nema.
func procitanoIzKrivulja(ime string, redaka, krivulja int) error {
	const (
		stala     = "gradnja je stala i nije maknula nijednu krivulju protoka, jer "
		ispravite = " Upišite krivulje u datoteku, ili samo zaglavlje ako letva krivulja nema, pa pokrenite gradnju ponovno."
	)
	switch {
	case redaka == 0:
		return fmt.Errorf(stala+"je datoteka krivulja %s prazna, bez ijednog retka."+ispravite, ime)
	case redaka > 1 && krivulja == 0:
		return fmt.Errorf(stala+"datoteka krivulja %s ispod zaglavlja ima samo prazne retke."+ispravite, ime)
	}
	return nil
}

// dodajKrivulje dodaje krivulje jedne datoteke onima koje su druge datoteke
// iste letve već dale.
func dodajKrivulje(dosad, vidjeno map[string]bool) map[string]bool {
	if dosad == nil {
		return vidjeno
	}
	maps.Copy(dosad, vidjeno)
	return dosad
}

// makniIzvanDatoteka miče krivulje kojih nema ni u jednoj datoteci krivulja
// letve i vraća ih, a uz njih i one koje su ostale jer je letva stigla
// paketom. Letva u hq/ zna imati i dvije datoteke; dok se sudilo po svakoj
// zasebno, gradnja je micala krivulje prve datoteke i javljala da ih u
// datoteci više nema, iako je datoteka bila ondje.
func makniIzvanDatoteka(db *sql.DB, uDatotekama map[string]map[string]bool) ([]MaknutaKrivulja, []MaknutaKrivulja, error) {
	var maknute, zadrzane []MaknutaKrivulja
	for _, letva := range slices.Sorted(maps.Keys(uDatotekama)) {
		izvan, primljena, err := izvanDatotekaLetve(db, letva, uDatotekama[letva])
		if err != nil {
			return nil, nil, err
		}
		if primljena {
			zadrzane = append(zadrzane, izvan...)
		} else {
			maknute = append(maknute, izvan...)
		}
	}
	return maknute, zadrzane, nil
}

// izvanDatotekaLetve miče krivulje letve kojih u njezinim datotekama nema i
// vraća ih. Letvu koja je stigla paketom s drugog čvora lokalna datoteka ne
// dira (Odluke (13. 10. 2026.), „primljeno paketom se ne dira”): laptop i
// kućni server znali su imati različite krivulje Donjeg Miholjca, pa bi starija
// lokalna datoteka odnijela krivulje koje je donio paket. Tada se krivulje ne
// miču, a vraćaju se s primljena true, da izvještaj gradnje kaže zašto su
// ostale.
func izvanDatotekaLetve(db *sql.DB, letva string, uDatoteci map[string]bool) (izvan []MaknutaKrivulja, primljena bool, err error) {
	p, err := Primljeno(db, letva)
	if err != nil {
		return nil, false, fmt.Errorf("je li letva %s stigla paketom: %w", letva, err)
	}
	if p != nil {
		izvan, err = krivuljeIzvanDatoteke(db, letva, uDatoteci)
		return izvan, true, err
	}
	izvan, err = makniKrivuljeIzvanDatoteke(db, letva, uDatoteci)
	return izvan, false, err
}

// MaknutaKrivulja je krivulja protoka koju je gradnja maknula iz arhive, jer
// je u datoteci krivulja letve više nema.
type MaknutaKrivulja struct {
	Letva     string
	VrijediOd string
	VrijediDo string // prazno: krivulja nije imala kraj valjanosti
}

// Opis je rečenica za izvještaj gradnje, s razdobljem valjanosti.
func (k MaknutaKrivulja) Opis() string {
	if k.VrijediDo == "" {
		return fmt.Sprintf("Krivulja protoka koja vrijedi od %s maknuta je iz arhive, jer je u datoteci krivulja više nema.",
			danHR(k.VrijediOd))
	}
	return fmt.Sprintf("Krivulja protoka koja je vrijedila od %s do %s maknuta je iz arhive, jer je u datoteci krivulja više nema.",
		danHR(k.VrijediOd), danHR(k.VrijediDo))
}

// danHR piše datum iz datoteke („2001-01-01”) hrvatski („1. 1. 2001.”). Datum
// koji se ne čita ostaje kako je zapisan, da se u datoteci može naći.
func danHR(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("2. 1. 2006.")
}

// OpisZadrzane je rečenica za izvještaj gradnje o krivulji koja nije maknuta,
// iako je u datoteci nema, jer je letva stigla paketom.
func (k MaknutaKrivulja) OpisZadrzane() string {
	if k.VrijediDo == "" {
		return fmt.Sprintf("Krivulja protoka koja vrijedi od %s nije maknuta iz arhive, iako je u datoteci krivulja nema, jer je letva stigla paketom s drugog čvora.",
			danHR(k.VrijediOd))
	}
	return fmt.Sprintf("Krivulja protoka koja je vrijedila od %s do %s nije maknuta iz arhive, iako je u datoteci krivulja nema, jer je letva stigla paketom s drugog čvora.",
		danHR(k.VrijediOd), danHR(k.VrijediDo))
}

// krivuljeLetve vraća krivulje koje letva ima u arhivi.
func krivuljeLetve(db *sql.DB, letva string) ([]MaknutaKrivulja, error) {
	rows, err := db.Query(`SELECT vrijedi_od, vrijedi_do FROM hq_krivulje WHERE letva = ? ORDER BY vrijedi_od`, letva)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MaknutaKrivulja
	for rows.Next() {
		k := MaknutaKrivulja{Letva: letva}
		if err := rows.Scan(&k.VrijediOd, &k.VrijediDo); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// krivuljeIzvanDatoteke vraća krivulje letve čiji se početak valjanosti ne
// pojavljuje u njezinim datotekama.
func krivuljeIzvanDatoteke(db *sql.DB, letva string, uDatoteci map[string]bool) ([]MaknutaKrivulja, error) {
	sve, err := krivuljeLetve(db, letva)
	if err != nil {
		return nil, err
	}
	var izvan []MaknutaKrivulja
	for _, k := range sve {
		if _, ima := uDatoteci[k.VrijediOd]; !ima {
			izvan = append(izvan, k)
		}
	}
	return izvan, nil
}

// makniKrivuljeIzvanDatoteke miče krivulje letve čiji se početak valjanosti
// više ne pojavljuje u njezinoj datoteci, s odsječcima, i vraća ih. Odsječci
// se brišu izrijekom, jer veza bez stranih ključeva ne briše djecu sama.
func makniKrivuljeIzvanDatoteke(db *sql.DB, letva string, uDatoteci map[string]bool) ([]MaknutaKrivulja, error) {
	maknute, err := krivuljeIzvanDatoteke(db, letva, uDatoteci)
	if err != nil {
		return nil, err
	}
	for _, k := range maknute {
		if _, err := db.Exec(`DELETE FROM hq_odsjecci WHERE krivulja IN
			(SELECT id FROM hq_krivulje WHERE letva = ? AND vrijedi_od = ?)`, letva, k.VrijediOd); err != nil {
			return nil, err
		}
		if _, err := db.Exec(`DELETE FROM hq_krivulje WHERE letva = ? AND vrijedi_od = ?`, letva, k.VrijediOd); err != nil {
			return nil, err
		}
	}
	return maknute, nil
}

// javiKrivuljeGradnje piše u izvještaj gradnje koje je krivulje gradnja
// maknula, koje je zadržala jer je letva stigla paketom, koje iz datoteke nije
// upisala, i kad su vrijedile.
func javiKrivuljeGradnje(zapisi io.Writer, kg krivuljeGradnje) {
	redci := make([][2]string, 0, len(kg.maknute)+len(kg.zadrzane)+len(kg.neupisane))
	for _, k := range kg.maknute {
		redci = append(redci, [2]string{k.Letva, k.Opis()})
	}
	for _, k := range kg.zadrzane {
		redci = append(redci, [2]string{k.Letva, k.OpisZadrzane()})
	}
	for _, k := range kg.neupisane {
		redci = append(redci, [2]string{k.Letva, k.OpisNeupisane()})
	}
	for _, r := range redci {
		fmt.Fprintf(zapisi, "%-8s %-16s %s\n", "", r[0], r[1])
	}
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
	// Mapa je stablo iz kojeg se čitaju datoteke ovog izvora. Prazno znači
	// zajedničko stablo arhive. Novi izvor često stiže sa svoje strane — s
	// vanjskog diska, iz sinkronizirane mape — i ne mora se preseliti da bi
	// ušao u arhivu.
	Mapa     string
	Napomena string
}

// zadaniIzvori je ono što je do sada stajalo u kodu, prepisano u tablicu.
// Točnosti su izmjerene usporedbom sa službeno ovjerenim nizom, na stotinama
// tisuća sati kroz devet letava — osim ondje gdje napomena kaže drukčije.
// Redovi su sravnjeni na tri skupine. Prije ih je bilo osam, a izmjereno je da
// fini poredak među dojavama ne odlučuje gotovo ništa: gdje god se dvije
// posvađaju, ovjereni niz je ondje i pobjeđuje. Unutar skupine odlučuje
// izmjerena točnost, koja ostaje jer je mjerena i jer se ispisuje kao ±.
var zadaniIzvori = []Izvor{
	// ovjereno — službeni, revidirani nizovi
	{"his2000", 0, 10, true, "", "referenca — po njoj su ostali izmjereni"},
	{"vituki", 5, 10, true, "", "mađarska služba, na tamošnjim letvama jedini izvor; točnost proglašena, ne izmjerena"},
	// datoteke ovog izvora u stablu su u lokalnom vremenu, pa ne smije na
	// popis izvora u UTC-u u zonaIzvora
	{"vizugy-ddvizig", 5, 10, true, "", "Mađarska vodoprivreda (DDVIZIG), satni nizovi i krivulje protoka iz paketa za obranu od poplava; točnost proglašena, ne izmjerena"},
	{"arso", 5, 10, true, "", "slovenska služba, javno objavljene dnevne vrijednosti; točnost proglašena, ne izmjerena"},
	{"arso-borl", 5, 10, true, "", "ARSO, stari Borl do 1981.; odvojeno od Borla I zbog HE Formin"},
	{"arso-borl-i", 5, 10, true, "", "ARSO, Borl I od 1989.; odvojeno od starog Borla zbog HE Formin"},
	{"ehyd", 5, 10, true, "", "austrijska hidrološka služba, javno objavljeni srednjaci; točnost proglašena, ne izmjerena"},
	// kvazi-kišomjeri — reanalize s Open-Meteo, po točkama iz registra slivova.
	// ERA5 (25 km) ide do danas, pa spojeni niz stoji na njemu; CERRA (5,5 km)
	// staje sredinom 2021. i bolje vidi planine, pa ostaje zaseban niz za
	// usporedbu — na 1350 m u Alpama daje 60 % više oborine od ERA5
	{"openmeteo-era5", 0, 10, true, "", "ECMWF ERA5 reanaliza, 25 km, od 1940.; oborina i snijeg"},
	{"openmeteo-era5land", 0, 10, true, "", "ECMWF ERA5-Land reanaliza, 11 km, od 1950.; temperatura zraka i visina snijega"},
	{"openmeteo-cerra", 0, 20, true, "", "Copernicus CERRA reanaliza, 5,5 km, 1985.–6/2021.; zaseban niz, ne ulazi u spoj ispred ERA5"},
	// stvarni kišomjeri DHMZ-a, preuzeti preko letva.voda.hr; sat je lokalni,
	// dan traje od 06 do 06, a rupe koje stranica pokaže kao nule izbačene su
	// pri pripremi datoteka
	{"kisomjer-dhmz", 0, 10, true, "", "DHMZ, izmjerena oborina (satna od 2016./2017., dnevna od 2009.); podaci Državnog hidrometeorološkog zavoda"},
	// hibridna popuna rupa stvarnih kišomjera: ERA5 s najbliže izvedene
	// točke pomnožen omjerom postaje i ERA5 oko rupe, samo za rupe do 31 dana;
	// stoji iza svega pa ulazi samo gdje mjerenja nema
	// amaterska mreža pljusak.com, skupljeno čitanjem svaki krug: sat je
	// kiša u zadnjem satu pred javljanjem, dan zbroj od ponoći do ponoći
	{"kisomjer-pljusak", 5, 20, true, "", "pljusak.com, amaterske postaje; skuplja se od 9/2026, točnost proglašena, ne izmjerena"},
	// kišomjeri nacionalnih službi Austrije, Mađarske, Slovenije i Slovačke s
	// ICPDR-ova DanubeHIS-a: dnevni zbroj od 9/2024 odnosno 1/2025; mađarski i
	// slovenski dan pomaknut je pri pripremi datoteka da stoji kao austrijski
	{"kisomjer-danubehis", 0, 10, true, "", "kišomjeri nacionalnih službi (AT, HU, SI, SK) s ICPDR DanubeHIS-a, dnevni zbroj od 9/2024; licenca CC BY-NC-SA 4.0, navesti izvor"},
	{"hibrid-era5", 20, 30, true, "", "popuna kratkih rupa stvarnih kišomjera iz ERA5, umjerena na postaju; nije mjerenje — isključiti ako popuna smeta"},
	{"gkd", 5, 10, true, "", "bavarska hidrološka služba, provjereni dnevni protoci; točnost proglašena, ne izmjerena"},
	// ICPDR DanubeHIS: podaci država dunavskog sliva, pod licencom CC BY-NC-SA
	// 4.0. Na mađarskim letvama iste su brojke kao vituki (medijan razlike 0 cm,
	// isti UTC sat), ali nose i protoke te satne vrijednosti koje vituki nema;
	// stoje iza vitukija, pa ulaze samo ondje gdje njega nema.
	//
	// Datumi dnevnih vrijednosti na DanubeHIS-u su točni. Srpske i češke
	// označava s 23:00 UTC prethodnog dana, a to je ponoć po UTC+1; mađarske
	// s 00:00 UTC. Mi smo datum uzimali iz UTC oznake, pa su srpski dani
	// ispadali dan raniji. Pravilo: datum dnevnog vodostaja i protoka s
	// DanubeHIS-a uzima se iz oznake pomaknute na UTC+1. Kišomjeri imaju svoje
	// pravilo (vidi kisomjer-danubehis). DanubeHIS-ove dnevne datoteke Bezdana
	// i Bogojeva već su pomaknute za dan, što daje isto. Austrijske, slovenske
	// i češke dnevne datoteke s DanubeHIS-a još nisu provjerene po tom pravilu.
	{"danubehis", 5, 20, true, "", "ICPDR DanubeHIS: ovjereni dnevni vodostaji i protoci (1961.–2023.) i sirovi satni od 2020.; licenca CC BY-NC-SA 4.0, navesti izvor"},
	// Republički hidrometeorološki zavod Republike Srbije poslao je izravno
	// ovjerene dnevne vodostaje Bezdana, Apatina, Bogojeva i Bačke Palanke
	// 1960.–2025. te protoke Bezdana, Bogojeva i Bačke Palanke (Bačke Palanke
	// 2023.–2025.). Na Bezdanu i Bogojevu 2016.–2022. brojevi su isti kao na
	// DanubeHIS-u, koji ih ima tek od 2016.; zato RHMZ stoji ispred njega.
	{"rhmz", 1, 10, true, "", "Republički hidrometeorološki zavod Republike Srbije (RHMZ): ovjereni dnevni vodostaji Bezdana, Apatina, Bogojeva i Bačke Palanke 1960.–2025. i protoci Bezdana i Bogojeva, a od 2023. i Bačke Palanke, poslani izravno"},
	// Batina 1960.–2001. iz Bezdana, 740 m uzvodno na suprotnoj obali: odnos
	// izmjeren 2001.–2022. drži na ±5 cm, dok preračun iz Mohácsa griješi
	// ±13 cm; zato stoji ispred njega (manja točnost u cm znači prednost).
	// Kao svaki preračun ne ulazi u red spajanja nego na kraj, samo gdje
	// mjerenja nema — zato isključen; točnost mu se ipak čita.
	{"preracun-bezdan", 5, 30, false, "", "Batina 1960.–2001. preračunata iz Bezdana (RHMZ): Batina = Bezdan + 20,2 − 0,0099·Bezdan; protok isti kao Bezdanov"},
	{"his2000-cs", 0, 10, false, "", "Donji Miholjac — odlučuje se kad dođe Drava"},
	{"his2000-spojeno", 0, 10, false, "", "Donji Miholjac — odlučuje se kad dođe Drava"},
	{"his2000-ukinuta-nizv", 0, 10, false, "", "Donji Miholjac — odlučuje se kad dođe Drava"},
	{"his2000-ukinuto", 0, 10, false, "", "Donji Miholjac — odlučuje se kad dođe Drava"},
	// s letve — ono što je čovjek očitao; ovjereni niz je zaglađen i takve
	// trenutke zna izgubiti, pa stoji ispred dojava
	{"cop-rucno", 1, 15, true, "", "očitanje s letve, upisano u programu"},
	// operativno — dojave automatike
	{"letva-dhmz", 1, 20, true, "", ""},
	{"cop", 3, 20, true, "", ""},
	{"letva-hv", 5, 20, true, "", "dobra većinu vremena; u zamrznutim razdobljima javlja istu vrijednost danima"},
	{"geolux-seba", 5, 20, true, "", "AVS Geolux/SEBA; točnost je zasad proglašena, ne izmjerena"},
	{"geolux", 5, 20, true, "", "zapisivač na Geolux HydroViewu kojem se iz naziva instrumenata ne vidi je li tlačni ili radarski"},
	// Gotovo svaka postaja ima dva zapisivača — tlačni i radarski — pa se
	// radarski vodi kao zaseban izvor iza tlačnog. Kad tlačni zašuti, a to se
	// događa, praznina se popunjava radarom umjesto da ostane rupa.
	{"geolux-radar", 5, 21, false, "", "rezervni radarski zapisivač iste postaje; kod Tikveša je prvu godinu pratio tlačni unutar 2 cm, a onda odlutao do 22 cm i u svibnju 2026. utihnuo — zato stoji isključen dok se ne provjeri po razdobljima"},
	{"pegelonline", 5, 20, true, "", "javni operativni vodostaji; u arhivu se uzimaju stvarna očitanja na punom satu"},
	{"viadonau", 5, 20, true, "", "austrijski Dunav od Achleitena do Thebnerstraßla, javni satni vodostaji od 2012. (od 2024. svakih 15 minuta, u arhivu puni sat); lokalno vrijeme, sat po sat isto što i PegelOnline; točnost proglašena, ne izmjerena"},
	// Hidroelektrana mjeri vlastito istjecanje i taj broj nitko drugi ne
	// mjeri. Kad takav niz stoji uz letvu, u imenu izvora piše koja ga je
	// elektrana dala: vrijednost nije očitana na letvi nego na elektrani, a
	// to se iz same brojke više ne vidi.
	{"hep-he-dubrava", 5, 20, true, "", "istjecanje HE Dubrave, kako ga HEP objavljuje; mjereno na elektrani, ne na letvi — preljev se vodi zasebno, točnost proglašena, ne izmjerena"},
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
func upisiZadaneIzvore(db Izvrsitelj) error {
	for _, i := range zadaniIzvori {
		if _, err := db.Exec(`INSERT OR IGNORE INTO izvori (naziv, tocnost, red, ukljucen, mapa, napomena)
			VALUES (?,?,?,?,?,?)`, i.Naziv, i.Tocnost, i.Red, i.Ukljucen, i.Mapa, i.Napomena); err != nil {
			return fmt.Errorf("upis izvora %s: %w", i.Naziv, err)
		}
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO izvori (naziv, tocnost, red, ukljucen, napomena)
		SELECT DISTINCT izvor, ?, 20, 0, 'novi izvor — uključiti ručno'
		FROM nizovi WHERE izvor NOT LIKE 'preracun-%'`, zadanaTocnost(""))
	return err
}

// Izvori vraća sve izvore koje arhiva poznaje, po redu povjerenja. Služi
// stranici na kojoj se uređuju; gradnja koristi citajIzvore.
func Izvori(db *sql.DB) ([]Izvor, error) {
	rows, err := db.Query(`SELECT naziv, tocnost, red, ukljucen, ` + stupacMape(db) + `, napomena
		FROM izvori ORDER BY red, tocnost, naziv`)
	if err != nil {
		return nil, fmt.Errorf("čitanje izvora: %w", err)
	}
	defer rows.Close()
	var out []Izvor
	for rows.Next() {
		var i Izvor
		if err := rows.Scan(&i.Naziv, &i.Tocnost, &i.Red, &i.Ukljucen, &i.Mapa, &i.Napomena); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// stupacMape javlja može li se mapa čitati. Arhiva se otvara samo za čitanje,
// pa se u njoj ne može ni dopuniti — a čvor koji je preuzeo starije izdanje
// nema taj stupac. Čitanje mora raditi i ondje, inače bi mu popis izvora
// nestao zbog stupca koji nema.
func stupacMape(db *sql.DB) string {
	var ima int
	if err := db.QueryRow(
		`SELECT count(*) FROM pragma_table_info('izvori') WHERE name = 'mapa'`).Scan(&ima); err != nil || ima == 0 {
		return `''`
	}
	return "mapa"
}

// PostaviIzvor mijenja jedan izvor i javlja koje letve zbog toga treba ponovno
// spojiti — one koje od njega imaju ijedan niz. Spajanje se ne pokreće ovdje:
// na velikoj letvi traje sekundama, pa je odluka kad ga pokrenuti na pozivatelju.
func PostaviIzvor(db *sql.DB, i Izvor) ([]string, error) {
	var prije Izvor
	err := db.QueryRow(`SELECT tocnost, red, ukljucen, `+stupacMape(db)+`, napomena FROM izvori WHERE naziv=?`,
		i.Naziv).Scan(&prije.Tocnost, &prije.Red, &prije.Ukljucen, &prije.Mapa, &prije.Napomena)
	if err != nil {
		return nil, fmt.Errorf("izvor %q: %w", i.Naziv, err)
	}
	if _, err := db.Exec(`UPDATE izvori SET tocnost=?, red=?, ukljucen=?, mapa=?, napomena=? WHERE naziv=?`,
		i.Tocnost, i.Red, i.Ukljucen, i.Mapa, i.Napomena, i.Naziv); err != nil {
		return nil, fmt.Errorf("upis izvora %q: %w", i.Naziv, err)
	}
	// Napomena ne mijenja nijedan broj, a mapa ga ne mijenja dok se arhiva
	// ponovno ne izgradi — spoj se računa iz onoga što je već u bazi.
	if prije.Tocnost == i.Tocnost && prije.Red == i.Red && prije.Ukljucen == i.Ukljucen {
		return nil, nil
	}
	rows, err := db.Query(`SELECT DISTINCT letva FROM nizovi WHERE izvor=? ORDER BY letva`, i.Naziv)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var letve []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		letve = append(letve, l)
	}
	return letve, rows.Err()
}

// Spoji ponovno gradi spojeni niz jedne letve iz onoga što je već u arhivi.
// Ne dira izvorne nizove i ne traži mapu s datotekama — čvor koji je arhivu
// dobio paketom nema odakle graditi, ali spajati mora moći.
func Spoji(db *sql.DB, letva string) (int, error) { return spoji(db, letva) }

// citajIzvore vraća uključene izvore po redu povjerenja i točnost svih.
func citajIzvore(db Izvrsitelj) (red []string, tocnosti map[string]float64, err error) {
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
// Izvrsitelj je ono što zna izvršiti upit: baza ili transakcija.
//
// Postoji da spajanje može teći UNUTAR tuđe transakcije. Ugradnja paketa briše
// letvu, upiše njezine dijelove i mora složiti spoj — ako to zadnje teče izvan
// transakcije, kvar ostavlja obrisan stari spoj i upisane nove nizove, dakle
// pola arhive.
type Izvrsitelj interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// SpojiU slaže spojeni niz jedne letve unutar zadane transakcije.
func SpojiU(q Izvrsitelj, letva string) (int, error) {
	return spojiSve(q, letva, nil)
}

func spoji(db *sql.DB, samo string) (int, error) {
	// Svaka letva i veličina dobiva svoju transakciju: puna gradnja spaja
	// preko deset milijuna vrijednosti, a to u jednoj transakciji napuhne
	// dnevnik pisanja preko svake mjere.
	return spojiSve(db, samo, func(f func(Izvrsitelj) error) error { return uTransakciji(db, f) })
}

// uTransakciji izvodi f u jednoj transakciji: upiše se sve ili ništa, i to
// jednim spuštanjem na disk. Gradnja piše s trajnim upisom (TrajniUpis), pa
// bi svaki upis izvan transakcije bio po jedno spuštanje na disk, na Linuxu
// i Windowsu po nekoliko milisekundi.
func uTransakciji(db *sql.DB, f func(Izvrsitelj) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// spojiSve spaja letvu ili sve letve. Kad je zadan omotac, svaki par letva i
// veličina prolazi kroz njega; bez njega sve teče u zatečenoj transakciji.
func spojiSve(q Izvrsitelj, samo string, omotac func(func(Izvrsitelj) error) error) (int, error) {
	// Red povjerenja i točnosti čitaju se jednom, iz arhive. Isti paket mora
	// na svakom čvoru dati iste brojeve, pa postavke ne smiju ovisiti o tome
	// koju je verziju programa tko pokrenuo.
	redSpajanja, tocnosti, err := citajIzvore(q)
	if err != nil {
		return 0, err
	}
	upit := `SELECT DISTINCT letva, velicina FROM nizovi`
	var args []any
	if samo != "" {
		upit += ` WHERE letva = ?`
		args = append(args, samo)
	}
	rows, err := q.Query(upit, args...)
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
		var n int
		posao := func(izv Izvrsitelj) error {
			var err error
			n, err = spojiJedan(izv, p.letva, p.velicina, redSpajanja, tocnosti)
			return err
		}
		var err error
		if omotac != nil {
			err = omotac(posao)
		} else {
			err = posao(q)
		}
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

func spojiJedan(db Izvrsitelj, letva, velicina string, redSpajanja []string, tocnosti map[string]float64) (int, error) {
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
			v, vrsta := zbroj[d]/float64(n), "srednjak"
			if zbrajaSe(velicina) {
				// oborina i snijeg se po danu zbrajaju, a zbroj nepotpunog dana
				// bio bi premalen, pa mora biti svih 24 sata
				if n < 24 {
					continue
				}
				v, vrsta = zbroj[d], "zbroj"
			}
			t := najgora[d]
			if prije, ima := dnevni[d]; ima && prije.tocnost <= t {
				continue
			}
			dnevni[d] = spojena{v: v, izvor: izvorDana[d], vrsta: vrsta, tocnost: t}
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

	// Transakciju vodi pozivatelj: puna gradnja otvara svoju po letvi i
	// veličini, a ugradnja paketa spaja unutar svoje, da se kvar ne zaustavi
	// između obrisanog starog spoja i upisanih novih nizova.
	if _, err := db.Exec(`DELETE FROM spoj WHERE letva=? AND velicina=?`, letva, velicina); err != nil {
		return 0, err
	}
	n := 0
	for korak, m := range map[string]map[int64]spojena{"satni": satni, "dnevni": dnevni} {
		for kad, s := range m {
			if _, err := db.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost)
				VALUES (?,?,?,?,?,?,?,?)`, letva, velicina, korak, kad, s.v, s.izvor, s.vrsta, s.tocnost); err != nil {
				return n, err
			}
			n++
		}
	}
	if n > 0 {
		fmt.Printf("%-8s %-16s %-22s %-12s spojeno %8d  (satnih %d, dnevnih %d)\n",
			"", letva, "→ spojeni niz", velicina, n, len(satni), len(dnevni))
	}
	return n, nil
}

// MakniNiz briše niz iz arhive, s očitanjima i onim što je od njega ušlo u
// spojeni niz. Zove se samo kad čovjek to zatraži — gradnja sama nikad ne
// briše niz, jer izvor s nedostupnom mapom izgleda isto kao izvor kojeg više
// nema.
//
// Arhiva se poslije ovoga i dalje da izgraditi iznova iz stabla: ondje te
// datoteke nema, pa je i nema što vratiti.
func MakniNiz(db *sql.DB, baza, letva, izvor, velicina, vrsta string) (int, error) {
	// Micanje niza završava ponovnim spajanjem, što je isti posao kao gradnja,
	// pa ide pod istu bravu.
	brava, err := Uzmi(baza, "micanje niza "+letva+" · "+izvor, "")
	if err != nil {
		return 0, err
	}
	defer brava.Pusti()

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(`SELECT id FROM nizovi WHERE letva=? AND izvor=? AND velicina=? AND vrsta=?`,
		letva, izvor, velicina, vrsta).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("niz %s/%s/%s/%s nije u arhivi", letva, izvor, velicina, vrsta)
	}
	if err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM ocitanja WHERE niz = ?`, id)
	if err != nil {
		return 0, err
	}
	obrisano, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM nizovi WHERE id = ?`, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	// Spojeni niz se slaže iznova, za cijelu letvu.
	//
	// Prva izvedba je iz spoja brisala po letvi, izvoru i veličini — a to su
	// redovi i sestrinskih nizova istog izvora. Micanje suvišnog niza
	// "cop-rucno · jutarnji" na Batini tako je odnijelo i svih deset ručnih
	// očitanja iz spojenog niza, među njima i zabilježeni minimum, iako im je
	// niz ostao netaknut. Sljedeća gradnja bi to popravila, ali dotad arhiva
	// stoji kriva a nitko nema razloga graditi.
	if _, err := spoji(db, letva); err != nil {
		return int(obrisano), fmt.Errorf("niz je maknut, ali spojeni niz se nije složio iznova: %w", err)
	}
	return int(obrisano), nil
}

// Sirotani javlja koji nizovi stoje u arhivi bez datoteke u stablu, bez
// gradnje. Stranica ih tako može pokazati kad god se otvori, a ne samo odmah
// nakon što se nešto gradilo.
//
// Nizovi se traže samo po nazivima datoteka. Snimke korita čitaju se cijele,
// jer dan snimke piše u datoteci, a ne u imenu; na laptopu je to 13. 10. 2026.
// bilo 245 malih datoteka, oko 1 MB, pa je i to dovoljno jeftino za svako
// otvaranje stranice. Snimka koja se ne da pročitati javlja se zasebnim
// retkom (snimkeBezDatoteke).
func Sirotani(db *sql.DB, koren string) ([]Sirotan, error) {
	nizovi, err := popisiSKvarovima(koren)
	if err != nil {
		return nil, err
	}
	izvori, err := Izvori(db)
	if err != nil {
		return nil, err
	}
	nedostupni := map[string]bool{}
	for _, i := range izvori {
		mapa := strings.TrimSpace(i.Mapa)
		if mapa == "" {
			continue
		}
		dodatni, err := popisi(mapa, "")
		if err != nil {
			// Vanjski disk nije priključen; izvor nije nestao.
			nedostupni[i.Naziv] = true
			continue
		}
		for k, n := range dodatni {
			if n.izvor == i.Naziv {
				nizovi[k] = n
			}
		}
	}
	return bezDatoteke(db, koren, nizovi, "", nedostupni)
}

// MakniKrivulje briše iz arhive krivulje protoka letve kojoj datoteke krivulja
// u stablu više nema i vraća koliko je krivulja maknuto. Kao ni snimke, ni
// krivulje bez datoteke gradnja ne miče sama (Odluke (13. 10. 2026.)):
// odlučuje čovjek, na popisu bez datoteke. Datoteke u stablu se ne diraju.
func MakniKrivulje(db *sql.DB, baza, koren, letva string) (int, error) {
	// Ista brava kao gradnja i ugradnja paketa: one istodobno brišu i upisuju
	// krivulje.
	brava, err := Uzmi(baza, "micanje krivulja protoka "+letva, "")
	if err != nil {
		return 0, err
	}
	defer brava.Pusti()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := krivuljeJosBezDatoteke(tx, koren, letva); err != nil {
		return 0, err
	}
	// Odsječci se brišu izrijekom: veza bez stranih ključeva ne briše djecu sama.
	if _, err := tx.Exec(`DELETE FROM hq_odsjecci WHERE krivulja IN
		(SELECT id FROM hq_krivulje WHERE letva = ?)`, letva); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM hq_krivulje WHERE letva = ?`, letva)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), tx.Commit()
}

// krivuljeJosBezDatoteke provjerava, pod bravom, da je letva i dalje na popisu
// krivulja bez datoteke. Stranica s gumbom Makni zna biti otvorena dugo: letva
// je u međuvremenu mogla stići paketom ili dobiti datoteku krivulja, a klik
// bi tada odnio sve krivulje koje je donio paket ili koje datoteka opet drži
// (Odluke (13. 10. 2026.), „primljeno paketom se ne dira”).
func krivuljeJosBezDatoteke(tx Izvrsitelj, koren, letva string) error {
	bez, err := krivuljeBezDatoteke(tx, koren, letva)
	if err != nil {
		return fmt.Errorf("ne da se provjeriti jesu li krivulje letve %s još bez datoteke: %w", letva, err)
	}
	if len(bez) == 0 {
		return fmt.Errorf("letva %s više nije na popisu krivulja bez datoteke, jer je u međuvremenu dobila "+
			"datoteku krivulja, stigla je paketom s drugog čvora ili su joj krivulje već maknute", letva)
	}
	return nil
}

// MakniSnimku briše iz arhive snimku korita kojoj datoteke u stablu više nema
// i vraća koliko je točaka maknuto. Kao ni niz, ni snimku gradnja ne miče sama
// (Odluke (13. 10. 2026.)): odlučuje čovjek, na popisu bez datoteke. Datoteke
// u stablu se ne diraju, pa se arhiva i dalje da izgraditi iznova.
func MakniSnimku(db *sql.DB, baza, letva, datum string) (int, error) {
	// Ista brava kao gradnja: gradnja istodobno briše i upisuje točke snimki.
	brava, err := Uzmi(baza, "micanje snimke korita "+letva+" · "+datum, "")
	if err != nil {
		return 0, err
	}
	defer brava.Pusti()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Točke se brišu izrijekom: veza bez stranih ključeva ne briše djecu sama.
	res, err := tx.Exec(`DELETE FROM profil_tocke WHERE profil IN
		(SELECT id FROM profili WHERE letva = ? AND datum = ?)`, letva, datum)
	if err != nil {
		return 0, err
	}
	tocaka, _ := res.RowsAffected()
	if res, err = tx.Exec(`DELETE FROM profili WHERE letva = ? AND datum = ?`, letva, datum); err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, fmt.Errorf("snimka korita %s od %s nije u arhivi", letva, datum)
	}
	return int(tocaka), tx.Commit()
}
