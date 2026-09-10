package arhiva

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func arhivaSNizom(t *testing.T, izvor string) (*sql.DB, string) {
	t.Helper()
	put := filepath.Join(t.TempDir(), "a.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO nizovi (zona, sliv, letva, izvor, velicina, vrsta, zapisa, od, do_)
		VALUES ('UTC','dunav','vukovar',?,'vodostaj','satni',2,'2013-06-14','2013-06-14')`, izvor)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	for i, v := range []float64{776, 774} {
		if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,?)`,
			id, 1371186000+int64(i)*3600, v); err != nil {
			t.Fatal(err)
		}
	}
	return db, put
}

// Ugradi na čvoru primatelju ponovno gradi spojeni niz. Bez postavki izvora
// isti je paket na čvoru s drukčijim postavkama davao druge brojeve, a nitko
// to nije mogao ni primijetiti.
func TestPaketNosiPostavkeIzvora(t *testing.T) {
	db, _ := arhivaSNizom(t, "letva-hv")
	if _, err := db.Exec(`UPDATE izvori SET tocnost=5, red=40, ukljucen=1 WHERE naziv='letva-hv'`); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	m, err := Izvezi(db, "vukovar", 1, "cop-osijek", &b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Inacica != 2 {
		t.Errorf("inačica %d", m.Inacica)
	}
	s, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Izvori) != 1 || s.Izvori[0].Naziv != "letva-hv" || s.Izvori[0].Tocnost != 5 {
		t.Fatalf("postavke u paketu: %+v", s.Izvori)
	}
	// Samo izvori koje letva koristi; tuđe postavke nisu izjava o njoj.
	for _, i := range s.Izvori {
		if i.Naziv == "his2000" {
			t.Error("u paketu su i izvori koje letva ne koristi")
		}
	}
}

// Nepoznat izvor iz paketa ulazi s postavkama iz paketa, i uključen ako je
// ondje bio: podaci tog izvora upravo stižu, nikoga drugoga ne diraju, a čvor
// koji je paket izdao za njih jamči.
func TestNepoznatIzvorIzPaketaUlaziSPostavkama(t *testing.T) {
	izvor, _ := arhivaSNizom(t, "nova-telemetrija")
	if _, err := izvor.Exec(`INSERT INTO izvori (naziv, tocnost, red, ukljucen, napomena)
		VALUES ('nova-telemetrija', 3, 25, 1, 'mjereno usporedbom')`); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if _, err := Izvezi(izvor, "vukovar", 1, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	s, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}

	prima, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prima.Close()
	if err := PripremiPraznu(prima); err != nil {
		t.Fatal(err)
	}
	if err := Ugradi(prima, s); err != nil {
		t.Fatal(err)
	}
	var tocnost float64
	var red int
	var ukljucen bool
	var napomena string
	if err := prima.QueryRow(`SELECT tocnost, red, ukljucen, napomena FROM izvori
		WHERE naziv='nova-telemetrija'`).Scan(&tocnost, &red, &ukljucen, &napomena); err != nil {
		t.Fatal(err)
	}
	if tocnost != 3 || red != 25 || !ukljucen || napomena != "mjereno usporedbom" {
		t.Errorf("postavke nakon ugradnje: %v %v %v %q", tocnost, red, ukljucen, napomena)
	}
	var n int
	prima.QueryRow(`SELECT count(*) FROM spoj WHERE letva='vukovar'`).Scan(&n)
	if n != 2 {
		t.Errorf("spojenih vrijednosti %d — izvor nije ušao u spoj", n)
	}
}

// Postavke koje čvor već ima ne mijenjaju se, jer su izvori zajednički svim
// letvama; razlika se javlja čovjeku umjesto da se tiho primijeni.
func TestPostojeceMPostavkeSeNeMijenjajuNegoJavljaju(t *testing.T) {
	izvor, _ := arhivaSNizom(t, "letva-hv")
	if _, err := izvor.Exec(`UPDATE izvori SET tocnost=5 WHERE naziv='letva-hv'`); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if _, err := Izvezi(izvor, "vukovar", 1, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	s, _ := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))

	prima, _ := arhivaSNizom(t, "letva-hv")
	if _, err := prima.Exec(`UPDATE izvori SET tocnost=8 WHERE naziv='letva-hv'`); err != nil {
		t.Fatal(err)
	}
	razlike, err := RazlikeIzvora(prima, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(razlike) != 1 || !razlike[0].Tocnost || razlike[0].Paket.Tocnost != 5 || razlike[0].Nas.Tocnost != 8 {
		t.Fatalf("razlike: %+v", razlike)
	}
	if err := Ugradi(prima, s); err != nil {
		t.Fatal(err)
	}
	var tocnost float64
	prima.QueryRow(`SELECT tocnost FROM izvori WHERE naziv='letva-hv'`).Scan(&tocnost)
	if tocnost != 8 {
		t.Errorf("paket je pregazio postavku čvora: %v", tocnost)
	}
}

// Paket inačice 1 nema izvori.json, pa mu se otisak računa po starom popisu —
// inače bi svaki stariji paket ispao pokvaren.
func TestStarijiPaketSeIDaljeProvjerava(t *testing.T) {
	if len(dijeloviZa(1)) != 5 || len(dijeloviZa(2)) != 6 {
		t.Fatalf("popisi dijelova: v1 %d, v2 %d", len(dijeloviZa(1)), len(dijeloviZa(2)))
	}
	if dijeloviZa(2)[5] != "izvori.json" {
		t.Errorf("zadnji dio inačice 2 je %q", dijeloviZa(2)[5])
	}
}

// Izvor koji se prvi put pojavio u datotekama mora ući u tablicu pri ISTOJ
// gradnji. Prije je prvi prolaz bio prije nego su nizovi upisani, pa novi
// izvor nije bio ni na popisu — čekao bi sljedeću gradnju da se uopće vidi.
func TestNoviIzvorUlaziUPopisPriIstojGradnji(t *testing.T) {
	koren := t.TempDir()
	mapa := filepath.Join(koren, "dunav", "vukovar")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapa, "vukovar_bas-nov_vodostaj_satni_2013.csv"),
		[]byte("vrijeme_utc;vodostaj_cm\n2013-06-14 05:00:00;776\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	baza := filepath.Join(t.TempDir(), "a.db")
	if _, err := Izgradi(koren, baza, "vukovar", io.Discard); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ukljucen bool
	if err := db.QueryRow(`SELECT ukljucen FROM izvori WHERE naziv='bas-nov'`).Scan(&ukljucen); err != nil {
		t.Fatalf("novi izvor nije na popisu nakon prve gradnje: %v", err)
	}
	if ukljucen {
		t.Error("novi izvor je sam ušao u spoj")
	}
}
