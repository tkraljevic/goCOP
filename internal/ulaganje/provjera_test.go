package ulaganje

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/arhiva"
	_ "modernc.org/sqlite"
)

// arhivaSNizom slaže arhivu s jednim nizom i zadanim vrijednostima, te spojeni
// niz koji na istim trenucima drži nešto drugo.
func arhivaSNizom(t *testing.T, letva, izvor, vrsta string, redci []arhiva.Redak,
	uSpoju []arhiva.Redak) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE nizovi (id INTEGER PRIMARY KEY, sliv TEXT, letva TEXT, izvor TEXT,
			velicina TEXT, vrsta TEXT, zapisa INTEGER DEFAULT 0);
		CREATE TABLE ocitanja (niz INTEGER, vrijeme INTEGER, vrijednost REAL);
		CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER,
			vrijednost REAL, izvor TEXT, vrsta TEXT, tocnost REAL);`); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta)
		VALUES ('dunav',?,?,'vodostaj',?)`, letva, izvor, vrsta)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	for _, r := range redci {
		if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,?)`,
			id, r.Vrijeme.Unix(), r.Vrijednost); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range uSpoju {
		if _, err := db.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost)
			VALUES (?, 'vodostaj', 'satni', ?, ?, 'letva-dhmz', 'trenutna', 5)`,
			letva, r.Vrijeme.Unix(), r.Vrijednost); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// Provjera prije brisanja mora dokazati da je baš NAŠA vrijednost u arhivi.
//
// Prva izvedba je gledala samo postoji li u spojenom nizu bilo kakav zapis iste
// letve i trenutka. Spoj po trenutku drži jednu vrijednost, onu najtočnijeg
// izvora, pa naše očitanje može izgubiti sudar a provjera svejedno prođe — i
// original se obriše iz operative iako ga u arhivi nema.
func TestProvjeraNePrihvacaTudjuVrijednostNaIstomTrenutku(t *testing.T) {
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	nase := arhiva.Redak{Vrijeme: kad, Vrijednost: -160}
	tudje := arhiva.Redak{Vrijeme: kad, Vrijednost: -153}

	// U arhivi nema našeg niza, ali spoj na tom trenutku ima tuđu vrijednost.
	put := arhivaSNizom(t, "batina", "letva-dhmz", "satni",
		[]arhiva.Redak{tudje}, []arhiva.Redak{tudje})

	nedostaje, err := ProvjeriUArhivi(put, "batina", []UNizu{
		{Izvor: "cop-rucno", Velicina: "vodostaj", Vrsta: "satni", Redci: []arhiva.Redak{nase}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if nedostaje != 1 {
		t.Errorf("provjera javlja %d nedostajućih, a naše vrijednosti nema u arhivi", nedostaje)
	}
}

// Ista vrijednost u pravom nizu prolazi.
func TestProvjeraPrihvacaVrijednostUSvomNizu(t *testing.T) {
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	nase := arhiva.Redak{Vrijeme: kad, Vrijednost: -160}
	put := arhivaSNizom(t, "batina", "cop-rucno", "satni", []arhiva.Redak{nase}, nil)

	nedostaje, err := ProvjeriUArhivi(put, "batina", []UNizu{
		{Izvor: "cop-rucno", Velicina: "vodostaj", Vrsta: "satni", Redci: []arhiva.Redak{nase}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if nedostaje != 0 {
		t.Errorf("provjera javlja %d nedostajućih, a vrijednost je u svom nizu", nedostaje)
	}
}

// Vrijednost koja se promijenila ne prolazi, ni na istom trenutku u istom nizu.
func TestProvjeraTraziIstuVrijednost(t *testing.T) {
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	put := arhivaSNizom(t, "batina", "cop-rucno", "satni",
		[]arhiva.Redak{{Vrijeme: kad, Vrijednost: -159}}, nil)

	nedostaje, err := ProvjeriUArhivi(put, "batina", []UNizu{
		{Izvor: "cop-rucno", Velicina: "vodostaj", Vrsta: "satni",
			Redci: []arhiva.Redak{{Vrijeme: kad, Vrijednost: -160}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if nedostaje != 1 {
		t.Errorf("provjera je prihvatila -159 umjesto -160")
	}
}

// Rekonstruirano mora biti u provjeri kao i ostalo. Prva izvedba ga je
// izostavila iz provjere, a ostavila u skupu koji se označava kao uloženo.
func TestRekonstruiranoUlaziUProvjeru(t *testing.T) {
	kad := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	p := &Pregled{
		Izvor: IzvorDojave, IzvorRucnog: IzvorRucnog, Vrsta: "satni", VrstaRucnog: "satni",
	}
	p.preracunato = []arhiva.Redak{{Vrijeme: kad, Vrijednost: -140}}

	var nadeno bool
	for _, n := range p.nizoviZaProvjeru() {
		if n.Izvor == "preracun-"+IzvorDojave && len(n.Redci) == 1 {
			nadeno = true
		}
	}
	if !nadeno {
		t.Error("rekonstruirano nije u skupu koji se provjerava")
	}
}

// Zaboravljanje ne briše ništa kad vrijednost u arhivi ne odgovara.
func TestZaboravljanjeStajeKadSeVrijednostRazlikuje(t *testing.T) {
	baza := probnaBaza(t)
	defer baza.Close()
	kad := time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC)
	// U arhivi stoji -149, a u operativi -150.
	put := arhivaSNizom(t, "batina", IzvorDojave, "satni",
		[]arhiva.Redak{{Vrijeme: kad, Vrijednost: -149}}, nil)

	if _, err := Zaboravi(context.Background(), baza, put, "st-1", nil); err == nil {
		t.Fatal("zaboravljanje je prošlo iako se vrijednost razlikuje")
	}
	var n int
	if err := baza.QueryRow(`SELECT count(*) FROM readings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("nakon pale provjere ostalo %d očitanja, a mora ostati svih 3", n)
	}
}
