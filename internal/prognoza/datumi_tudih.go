package prognoza

// Datumi u zaglavlju tuđe prognoze. Tablica kaže kojem danu pripada koji
// stupac, i to se ne pogađa. Kad datumi ne idu dan za danom, ne zna se
// kojem danu pripadaju stupci od prvog neurednog datuma nadalje: oni se ne
// spremaju i ne premještaju na druge dane. Ostaje uredni početak, a izdanje
// naše prognoze nosi upozorenje dok tablica ne bude ispravljena.
//
// Jedna iznimka je potvrđena. RHMZ Srbije datume zaglavlja računa kao ponoć
// dana izdanja plus n × 24 h, pa uz jesenski pomak sata dan napiše dvaput:
// 24. 10. 2026. stajalo je subota 24., nedjelja 25., nedjelja 25.,
// ponedjeljak 26. i utorak 27. 10., a to je prognoza za 25.–28. 10.
// (Odluke (24. 10. 2026.)). Zaglavlje koje se točno poklapa s tim računom
// čita se redom po danima od dana izdanja, uz napomenu.
//
// Što je s tablicom bilo, pamti se u bazi prognoza (tude_neuredne) uz
// izdanje tuđe prognoze, pa upozorenje ostaje i nakon ponovnog pokretanja,
// dok ga uredno čitanje istog izvora ne obriše ili izdanje ne postane
// starije od TudeUnatragSati.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
)

// NeuredniDatumi je tuđa prognoza čije zaglavlje ne ide dan za danom. Čitač
// je vraća uz letve skraćene na uredni početak.
type NeuredniDatumi struct {
	Izvor  string      // podrijetlo prognoze, npr. PodrijetloHidmet
	Izdano time.Time   // izdanje tuđe prognoze
	Datumi []time.Time // zaglavlje kako je pisalo
	// Urednih je koliko datuma od početka ide dan za danom; samo su ti
	// stupci spremljeni.
	Urednih int
	// PrviJeMjerenje kaže da je prvi stupac jutarnje mjerenje, a ne dan
	// prognoze (srpska tablica).
	PrviJeMjerenje bool
	// PoRedu kaže da je zaglavlje greška pomaka sata (pomakSataRHMZ), pa su
	// svi stupci spremljeni redom po danima od prvog.
	PoRedu bool
	// Tablica je koja je tablica izvora neuredna, kad ih izvor ima više
	// (mađarska: dunavska i dravska); prazno kad je jedna.
	Tablica string
}

// Error kaže što ne valja i što je zbog toga spremljeno, bez izvora: izvor
// dodaje onaj tko javlja.
func (e *NeuredniDatumi) Error() string {
	if e.PoRedu {
		return "datumi u tablici pogrešni su zbog pomaka sata (" + daniRedom(e.Datumi) + "); njihova prognoza stoji redom za dane od " +
			danTude(e.Datumi[0].AddDate(0, 0, 1)) + " do " + danTude(e.Datumi[0].AddDate(0, 0, len(e.Datumi)-1))
	}
	s := "datumi u tablici ne idu dan za danom (" + daniRedom(e.Datumi) + "); "
	if e.PrviJeMjerenje && e.Urednih == 1 {
		return s + "uz našu prognozu ne stoji nijedan dan njihove, jer se ne zna kojem danu pripadaju"
	}
	return s + "uz našu prognozu njihova stoji samo do " + danTude(e.Datumi[e.Urednih-1]) +
		", jer se za ostale stupce ne zna kojem danu pripadaju"
}

// uz je e izdanja izdano kao greška čitača; nil ostaje nil, a ne sučelje
// error s praznim pokazivačem.
func (e *NeuredniDatumi) uz(izdano time.Time) error {
	if e == nil {
		return nil
	}
	e.Izdano = izdano
	return e
}

// NeuredneIz su neuredni datumi iz greške čitača: jedna, omotana ili više
// spojenih (errors.Join); nil kad ih nema.
func NeuredneIz(err error) []*NeuredniDatumi {
	switch e := err.(type) {
	case *NeuredniDatumi:
		return []*NeuredniDatumi{e}
	case interface{ Unwrap() []error }:
		var out []*NeuredniDatumi
		for _, x := range e.Unwrap() {
			out = append(out, NeuredneIz(x)...)
		}
		return out
	case interface{ Unwrap() error }:
		return NeuredneIz(e.Unwrap())
	}
	return nil
}

// odvojiNeuredne vrati grešku čitanja tablice, osim neurednih datuma: njih
// doda u popis s imenom tablice, jer uz njih letve vrijede.
func odvojiNeuredne(err error, tablica string, neuredne *[]error) error {
	var nd *NeuredniDatumi
	if !errors.As(err, &nd) {
		return err
	}
	nd.Tablica = tablica
	*neuredne = append(*neuredne, nd)
	return nil
}

// nazivTude je kako se tuđa prognoza zove u upozorenju.
var nazivTude = map[string]string{
	Podrijetlo:       "mađarska prognoza",
	PodrijetloHidmet: "srpska prognoza",
}

// upozorenje je upozorenje izdanja o ovoj tablici, npr. „srpska prognoza
// (hidmet.gov.rs) od 24. 10.: datumi u tablici ne idu dan za danom (…)”.
func (e *NeuredniDatumi) upozorenje() Upozorenje {
	ime := e.Izvor
	if n := nazivTude[e.Izvor]; n != "" {
		ime = n + " (" + e.Izvor + ")"
	}
	if e.Tablica != "" {
		ime += ", " + e.Tablica + ","
	}
	tezina := TezinaUpozorenje
	if e.PoRedu {
		tezina = TezinaNapomena // vrijednosti stoje pod pravim danom
	}
	return Upozorenje{Vrsta: VrstaTudaDatumi, Tezina: tezina,
		Opis: ime + " od " + danTude(e.Izdano) + ": " + e.Error()}
}

// danTude je dan u hrvatskom obliku, npr. „25. 10.”.
func danTude(t time.Time) string { return t.In(models.Zagreb).Format("2. 1.") }

func daniRedom(datumi []time.Time) string {
	d := make([]string, len(datumi))
	for i, t := range datumi {
		d[i] = danTude(t)
	}
	return strings.Join(d, ", ")
}

// urednihDana je koliko datuma od početka ide dan za danom po kalendaru.
// Uspoređuje se s prvim datumom, ne u satima: dan pomaka sata ima 23 ili
// 25 sati.
func urednihDana(datumi []time.Time) int {
	if len(datumi) == 0 {
		return 0
	}
	prvi := datumi[0].In(models.Zagreb)
	for i, d := range datumi {
		g, m, dan := d.In(models.Zagreb).Date()
		if !time.Date(g, m, dan, 0, 0, 0, 0, models.Zagreb).Equal(time.Date(prvi.Year(), prvi.Month(), prvi.Day()+i, 0, 0, 0, 0, models.Zagreb)) {
			return i
		}
	}
	return len(datumi)
}

// urednoZaglavlje skrati zaglavlje na uredni početak; uz skraćeno vrati i
// opis neurednog, inače nil.
func urednoZaglavlje(datumi []time.Time, izvor string, prviJeMjerenje bool) ([]time.Time, *NeuredniDatumi) {
	n := urednihDana(datumi)
	if n == len(datumi) {
		return datumi, nil
	}
	return datumi[:n], &NeuredniDatumi{Izvor: izvor, Datumi: datumi, Urednih: n, PrviJeMjerenje: prviJeMjerenje}
}

// zaglavljeHidmeta je urednoZaglavlje srpske tablice, osim kad je
// zaglavlje greška pomaka sata: tada su stupci redom dani od prvog.
func zaglavljeHidmeta(datumi []time.Time) ([]time.Time, *NeuredniDatumi) {
	if !pomakSataRHMZ(datumi) {
		return urednoZaglavlje(datumi, PodrijetloHidmet, true)
	}
	redom := make([]time.Time, len(datumi))
	for i := range datumi {
		redom[i] = datumi[0].AddDate(0, 0, i)
	}
	return redom, &NeuredniDatumi{Izvor: PodrijetloHidmet, Datumi: datumi, Urednih: len(datumi), PrviJeMjerenje: true, PoRedu: true}
}

// pomakSataRHMZ kaže je li neuredno zaglavlje točno ono što daje ponoć
// prvog dana plus n × 24 h: preko jesenskog pomaka sata taj račun jedan dan
// napiše dvaput, a preko proljetnog daje prave datume.
func pomakSataRHMZ(datumi []time.Time) bool {
	if urednihDana(datumi) == len(datumi) {
		return false
	}
	g, m, d := datumi[0].In(models.Zagreb).Date()
	ponoc := time.Date(g, m, d, 0, 0, 0, 0, models.Zagreb)
	for i, t := range datumi {
		if danTude(t) != danTude(ponoc.Add(time.Duration(i)*24*time.Hour)) {
			return false
		}
	}
	return true
}

// ZapamtiNeuredne upiše ishod uredno spremljenog čitanja izvora: neuredne
// tablice iz greške čitača (NeuredneIz) zamijene ono što je za taj izvor
// bilo zapamćeno, a čitanje bez njih ga briše. Čitanje koje nije uspjelo ne
// zove ovo, jer se iz njega ne zna je li tablica ispravljena.
func ZapamtiNeuredne(db *sql.DB, izvor string, citanje error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tude_neuredne WHERE izvor = ?`, izvor); err != nil {
		return err
	}
	for _, nd := range NeuredneIz(citanje) {
		u := nd.upozorenje()
		if _, err := tx.Exec(`INSERT OR REPLACE INTO tude_neuredne (izvor, tablica, izdano, tezina, opis) VALUES (?,?,?,?,?)`,
			izvor, nd.Tablica, nd.Izdano.Unix()/3600, u.Tezina, u.Opis); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// upozorenjaTudih su upozorenja zapamćenih neurednih tablica tuđih
// prognoza izdanih u zadnjih TudeUnatragSati do sata sada. Zapis koji se ne
// da pročitati ne zaustavlja izdanje, nego postaje napomena.
func upozorenjaTudih(db *sql.DB, sada int64) []Upozorenje {
	out, err := citajNeuredne(db, sada)
	if err != nil {
		return []Upozorenje{{Vrsta: VrstaTudaDatumi, Tezina: TezinaNapomena,
			Opis: "zapis o tuđim tablicama s neurednim datumima nije pročitan: " + err.Error()}}
	}
	return out
}

func citajNeuredne(db *sql.DB, sada int64) ([]Upozorenje, error) {
	r, err := db.Query(`SELECT tezina, opis FROM tude_neuredne WHERE izdano >= ? ORDER BY izvor, tablica`, sada-TudeUnatragSati)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []Upozorenje
	for r.Next() {
		u := Upozorenje{Vrsta: VrstaTudaDatumi}
		if err := r.Scan(&u.Tezina, &u.Opis); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, r.Err()
}

// shemaNeurednih je tablica tuđih tablica s neurednim datumima
// (ZapamtiNeuredne): upozorenje koje izdanje nosi dok uredno čitanje istog
// izvora ne stigne.
const shemaNeurednih = `CREATE TABLE tude_neuredne (
	izvor   TEXT NOT NULL,
	tablica TEXT NOT NULL,
	izdano  INTEGER NOT NULL, -- izdanje tuđe prognoze, UTC sati od epohe
	tezina  INTEGER NOT NULL,
	opis    TEXT NOT NULL,
	PRIMARY KEY (izvor, tablica)
) WITHOUT ROWID`

// uvediNeuredne stvori tablicu tude_neuredne. Baza koja je nema još nije
// otvorena izdanjem koje srpsku prognozu čita po stupcima i uz pomak sata
// redom, pa se u istoj transakciji ispravljaju stari srpski retci:
// preslažu se dani uz pomak sata (ispraviPomakSataRHMZ) i brišu granice
// obrane spremljene kao prognoza (brisanjeGranicaObrane). Ispravak tako ide
// točno jednom.
// Stari čitač srpske tablice preskakao je ćelije „-”, pa su granice
// redovne i izvanredne obrane moravskih postaja (npr. Varvarin 400 i 500)
// stajale kao prognoza za zadnje dane. Od jutarnjeg mjerenja istog izdanja
// odstupaju stotinama centimetara, a sve ostale vrijednosti od 26. 9. do
// 24. 10. 2026. najviše 36 cm; prognoze za te dane RHMZ nije objavio, pa
// redak samo nestane (Odluke (24. 10. 2026.)).
const (
	odstupanjeGranica     = 150 // cm od jutarnjeg mjerenja
	brisanjeGranicaObrane = `DELETE FROM tude WHERE izvor = ?1 AND ciljni > izdano AND abs(vrijednost -
		(SELECT j.vrijednost FROM tude j WHERE j.izvor = ?1 AND j.letva = tude.letva AND j.izdano = tude.izdano AND j.ciljni < j.izdano)) > ?2`
)

func uvediNeuredne(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ima int
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'tude_neuredne'`).Scan(&ima); err != nil || ima > 0 {
		return err
	}
	if err := ispraviPomakSataRHMZ(tx); err != nil {
		return fmt.Errorf("ispravak srpske prognoze uz pomak sata: %w", err)
	}
	if _, err := tx.Exec(brisanjeGranicaObrane, PodrijetloHidmet, odstupanjeGranica); err != nil {
		return fmt.Errorf("brisanje granica obrane iz srpske prognoze: %w", err)
	}
	if _, err := tx.Exec(shemaNeurednih); err != nil {
		return err
	}
	return tx.Commit()
}

// ispraviPomakSataRHMZ preslože srpske prognoze koje je starija inačica
// spremila pod datume zaglavlja s greškom pomaka sata (pomakSataRHMZ): redak
// spremljen pod datumom koji se u zaglavlju prvi put javlja u stupcu k
// pripada danu izdanja plus k (Odluke (24. 10. 2026.)). Kad je pravi dan već
// zapisan, jer ga je razmjenom donio ispravljeni izdavač, stari redak se
// briše.
func ispraviPomakSataRHMZ(tx *sql.Tx) error {
	izdanja, err := izdanjaHidmeta(tx)
	if err != nil {
		return err
	}
	for _, izdano := range izdanja {
		for _, p := range preslagivanjePomaka(izdano) {
			if err := preseli(tx, izdano, p[0], p[1]); err != nil {
				return err
			}
		}
	}
	return nil
}

// izdanjaHidmeta su sati svih zapisanih srpskih izdanja.
func izdanjaHidmeta(tx *sql.Tx) ([]int64, error) {
	r, err := tx.Query(`SELECT DISTINCT izdano FROM tude WHERE izvor = ?`, PodrijetloHidmet)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []int64
	for r.Next() {
		var i int64
		if err := r.Scan(&i); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, r.Err()
}

// preseli premjesti retke srpskog izdanja sa sata s na sat na; postaja koja
// za na već ima redak gubi stari.
func preseli(tx *sql.Tx, izdano, s, na int64) error {
	if _, err := tx.Exec(`DELETE FROM tude WHERE izvor = ?1 AND izdano = ?2 AND ciljni = ?3
		AND letva IN (SELECT letva FROM tude WHERE izvor = ?1 AND izdano = ?2 AND ciljni = ?4)`,
		PodrijetloHidmet, izdano, s, na); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE tude SET ciljni = ? WHERE izvor = ? AND izdano = ? AND ciljni = ?`, na, PodrijetloHidmet, izdano, s)
	return err
}

// preslagivanjePomaka su parovi ciljnih sati (s, na) srpskog izdanja
// izdanog u satu izdano kad je zaglavlje tog dana greška pomaka sata, od
// kasnijih prema ranijima, da pravi dan bude slobodan prije nego što se na
// njega preseli raniji; nil kad zaglavlje nije neuredno.
func preslagivanjePomaka(izdano int64) [][2]int64 {
	g, m, d := time.Unix(izdano*3600, 0).In(models.Zagreb).Date()
	ponoc := time.Date(g, m, d, 0, 0, 0, 0, models.Zagreb)
	zaglavlje := make([]time.Time, 5)
	for i := range zaglavlje {
		zg, zm, zd := ponoc.Add(time.Duration(i) * 24 * time.Hour).In(models.Zagreb).Date()
		zaglavlje[i] = time.Date(zg, zm, zd, 7, 0, 0, 0, models.Zagreb)
	}
	if !pomakSataRHMZ(zaglavlje) {
		return nil
	}
	var out [][2]int64
	for k := len(zaglavlje) - 1; k >= 1; k-- {
		na := time.Date(g, m, d+k, 7, 0, 0, 0, models.Zagreb)
		if zaglavlje[k].Equal(zaglavlje[k-1]) || zaglavlje[k].Equal(na) {
			continue // nije prvo javljanje datuma, ili je već pravi dan
		}
		out = append(out, [2]int64{zaglavlje[k].Unix() / 3600, na.Unix() / 3600})
	}
	return out
}
