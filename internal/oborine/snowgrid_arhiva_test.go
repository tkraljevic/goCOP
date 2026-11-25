package oborine

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/arhiva"
)

// brojiloPrijenosa je HTTP prijenos koji samo broji pozive i ne ide nikamo.
type brojiloPrijenosa struct{ n atomic.Int32 }

func (b *brojiloPrijenosa) RoundTrip(*http.Request) (*http.Response, error) {
	b.n.Add(1)
	return nil, errors.New("izvor se ne smije zvati")
}

// Čvor koji ne izdaje prognozu SNOWGRID ne zove nijednom: krug samo
// pričeka deset minuta i ponovno pogleda ulogu. Kad postane izdavač, krug
// ide na izvor.
func TestSnijegNaTluSamoIzdavac(t *testing.T) {
	d, err := Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	prijenos := &brojiloPrijenosa{}
	izdaje := false
	s := &SnijegNaTlu{DB: d, Klijent: &http.Client{Transport: prijenos}, Smije: func() bool { return izdaje },
		Tocke: func() ([]Tocka, error) { return []Tocka{{"k-a", 47, 13}}, nil }}
	var pauze []time.Duration
	s.Cekaj = func(_ context.Context, p time.Duration) error {
		pauze = append(pauze, p)
		if len(pauze) < 3 {
			return nil
		}
		return context.Canceled
	}
	s.Vrti(context.Background())
	if n := prijenos.n.Load(); n != 0 {
		t.Fatalf("čvor koji ne izdaje zvao je izvor %d puta", n)
	}
	if len(pauze) != 3 || pauze[0] != provjeraUloge {
		t.Errorf("pauze %v", pauze)
	}
	izdaje = true
	if p := s.krug(context.Background()); p != ponovnoNakonGreske || prijenos.n.Load() == 0 {
		t.Errorf("izdavač: pauza %v, poziva %d", p, prijenos.n.Load())
	}
}

// probnaArhiva je arhiva sa spojenim nizom snijega na tlu za točku k-a
// (izmišljeni brojevi).
func probnaArhiva(t *testing.T, dani map[int64]float64) *sql.DB {
	t.Helper()
	a, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "vodostaji.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if err := arhiva.PripremiPraznu(a); err != nil {
		t.Fatal(err)
	}
	for d, v := range dani {
		if _, err := a.Exec(`INSERT INTO spoj (letva, velicina, korak, vrijeme, vrijednost, izvor, vrsta, tocnost)
			VALUES ('snowgrid-k-a', 'snijeg-na-tlu', 'dnevni', ?, ?, 'snowgrid', 'dnevni', 0)`, d*86400, v); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

// Nizovi: povijest iz arhive, a preko nje dani iz baze oborina; arhiva bez
// tablica (paketi još nisu stigli) nije greška.
func TestNizoviIzArhiveIBaze(t *testing.T) {
	d, err := Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &SnijegNaTlu{DB: d, Arhiva: probnaArhiva(t, map[int64]float64{-3000: 40, 100: 10, 101: 11})}
	if _, err := d.Exec(`INSERT INTO snijeg_na_tlu (kisomjer, dan, swe) VALUES ('k-a', 101, 12), ('k-a', 102, 13)`); err != nil {
		t.Fatal(err)
	}
	n, err := s.Nizovi([]string{"k-a"}, -4000, 200)
	if err != nil {
		t.Fatal(err)
	}
	if a := n["k-a"]; len(a) != 4 || a[-3000] != 40 || a[100] != 10 || a[101] != 12 || a[102] != 13 {
		t.Errorf("niz %v", a)
	}

	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prazna.Close()
	s.Arhiva = prazna
	if n, err := s.Nizovi([]string{"k-a"}, 0, 200); err != nil || len(n["k-a"]) != 2 {
		t.Errorf("bez arhive: %v %v", n, err)
	}
	prazna.Close()
	if _, err := s.Nizovi([]string{"k-a"}, 0, 200); err == nil {
		t.Error("zatvorena arhiva bez greške")
	}
}

// Ulaganje u stablo: zasebna letva snowgrid-<točka>, prvi put sva povijest
// točke (i ona koju arhiva već ima), poslije najviše jednom tjedno od devet
// dana prije zadnjeg uloženog, a odmah kad stigne starija povijest. Točka
// bez dana se preskače. Gradnja iz stabla stavlja niz u spojeni dnevni niz
// na ponoć UTC, i dan prije 1970.
func TestUloziSnijegUStablo(t *testing.T) {
	dir := t.TempDir()
	d, err := Otvori(filepath.Join(dir, "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	koren := filepath.Join(dir, "stablo")
	s := &SnijegNaTlu{DB: d, Arhiva: probnaArhiva(t, map[int64]float64{19000: 7}),
		Tocke: func() ([]Tocka, error) { return []Tocka{{"k-a", 47, 13}, {"k-b", 47, 14}}, nil }}
	prvi := dan(1961, 1, 1)
	if _, err := d.Exec(`INSERT INTO snijeg_na_tlu (kisomjer, dan, swe) VALUES ('k-a', 20000, 55.5), ('k-a', 20010, 60)`); err != nil {
		t.Fatal(err)
	}
	sada := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	letve, err := s.Ulozi(koren, "drava", sada)
	if err != nil || len(letve) != 1 || letve[0] != "snowgrid-k-a" {
		t.Fatalf("uložene letve %v (%v)", letve, err)
	}
	redciStabla := func() int {
		t.Helper()
		redci, err := arhiva.PostojeciRedci(koren, "drava", "snowgrid-k-a", IzvorSnowgrida, VelicinaSnijega, "dnevni")
		if err != nil {
			t.Fatal(err)
		}
		return len(redci)
	}
	if n := redciStabla(); n != 3 {
		t.Fatalf("u stablu %d redaka, želim 3 (s danom iz arhive)", n)
	}
	// novi dan unutar tjedna ne ide u arhivu, a nakon tjedna ide
	if _, err := d.Exec(`INSERT INTO snijeg_na_tlu (kisomjer, dan, swe) VALUES ('k-a', 20011, 61)`); err != nil {
		t.Fatal(err)
	}
	if letve, err := s.Ulozi(koren, "drava", sada.Add(6*24*time.Hour)); err != nil || len(letve) != 0 || redciStabla() != 3 {
		t.Errorf("unutar tjedna: %v (%v)", letve, err)
	}
	if letve, err := s.Ulozi(koren, "drava", sada.Add(IzdajSnijegSvakih)); err != nil || len(letve) != 1 || redciStabla() != 4 {
		t.Errorf("nakon tjedna: %v (%v)", letve, err)
	}
	// starija povijest (preuzimanje desetljeća unatrag) ide odmah
	if _, err := d.Exec(`INSERT INTO snijeg_na_tlu (kisomjer, dan, swe) VALUES ('k-a', ?, 0)`, prvi); err != nil {
		t.Fatal(err)
	}
	if letve, err := s.Ulozi(koren, "drava", sada.Add(IzdajSnijegSvakih+time.Hour)); err != nil || len(letve) != 1 || redciStabla() != 5 {
		t.Errorf("starija povijest: %v (%v)", letve, err)
	}
	baza := filepath.Join(dir, "vodostaji.db")
	if _, err := arhiva.Izgradi(koren, baza, "snowgrid-k-a", nil); err != nil {
		t.Fatal(err)
	}
	a, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	niz, err := (&SnijegNaTlu{DB: d, Arhiva: a}).izArhive("k-a", prvi, 30000)
	if err != nil || len(niz) != 5 || niz[prvi] != 0 || niz[19000] != 7 || niz[20000] != 55.5 || niz[20011] != 61 {
		t.Errorf("iz arhive %v (%v)", niz, err)
	}
	ime := filepath.Join(koren, "drava", "snowgrid-k-a")
	if dat, err := os.ReadDir(ime); err != nil || len(dat) == 0 || !strings.HasPrefix(dat[0].Name(), "snowgrid-k-a_snowgrid_snijeg-na-tlu_dnevni_") {
		t.Errorf("datoteke %v (%v)", dat, err)
	}
}

// Čvor koji postane izdavač, ili ima povijest iz paketa, preuzima samo dane
// koje nema: točka s poviješću u arhivi od 1961. do prije tri dana traži
// samo zadnje desetljeće od devet dana prije toga, a točka bez ičega cijelu
// povijest.
func TestNadopuniNastavljaOdPoznatog(t *testing.T) {
	kraj := dan(1975, 6, 30)
	izvor := &laziIzvor{kraj: kraj, v: func(int64) *float64 { return broj(3) }}
	s, _ := probniSnijeg(t, izvor, Tocka{"k-a", 47, 13}, Tocka{"k-b", 47.2, 14.3})
	povijest := map[int64]float64{}
	for d := dan(1961, 1, 1); d <= kraj-3; d++ {
		povijest[d] = 1
	}
	s.Arhiva = probnaArhiva(t, povijest)
	n, err := s.Nadopuni(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("%d zahtjeva (%v), želim 3", n, err)
	}
	want := []string{"1975-06-18…1975-06-30", "1971-01-01…1975-06-30", "1961-01-01…1970-12-31"}
	if !reflect.DeepEqual(izvor.trazeno, want) {
		t.Errorf("traženo %v, želim %v", izvor.trazeno, want)
	}
	// i dani dobiveni uz izdanje prognoze vrijede kao poznati
	izvor.trazeno, izvor.kraj = nil, kraj+1
	if n, err := s.Nadopuni(context.Background()); err != nil || n != 2 ||
		izvor.trazeno[0] != "1975-06-21…1975-07-01" {
		t.Errorf("novi dan: %d %v (%v)", n, izvor.trazeno, err)
	}
}

// Greške ulaganja: točke se ne daju pročitati, ili baza oborina nije
// otvorena.
func TestUloziSnijegGreske(t *testing.T) {
	d, err := Otvori(filepath.Join(t.TempDir(), "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &SnijegNaTlu{DB: d, Tocke: func() ([]Tocka, error) { return nil, errors.New("registar") }}
	if _, err := s.Ulozi(t.TempDir(), "drava", time.Now()); err == nil {
		t.Error("točke bez greške")
	}
	s.Tocke = func() ([]Tocka, error) { return []Tocka{{"k-a", 47, 13}}, nil }
	d.Close()
	if _, err := s.Ulozi(t.TempDir(), "drava", time.Now()); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Arhiva koja se ne da pročitati zaustavi preuzimanje prije ijednog
// zahtjeva za dane; raspon dana bez tablice je greška.
func TestNadopuniArhivaSGreskom(t *testing.T) {
	izvor := &laziIzvor{kraj: dan(1962, 1, 5), v: func(int64) *float64 { return broj(1) }}
	s, _ := probniSnijeg(t, izvor, Tocka{"k-a", 47, 13})
	pokvarena, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "vodostaji.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pokvarena.Exec(`CREATE TABLE spoj (letva TEXT)`); err != nil {
		t.Fatal(err)
	}
	defer pokvarena.Close()
	s.Arhiva = pokvarena
	if n, err := s.Nadopuni(context.Background()); err == nil || n != 0 || len(izvor.trazeno) != 0 {
		t.Errorf("pokvarena arhiva: %d zahtjeva (%v)", n, err)
	}
	if _, _, _, err := rasponDana(s.DB, `SELECT min(dan), max(dan) FROM nema_je`); err == nil {
		t.Error("raspon bez tablice bez greške")
	}
}

// Poznati dani vrijede samo za razdoblje čiji početak pokrivaju, i ne
// skraćuju ono što je već zapisano kao preuzeto; prazan niz nema redaka.
func TestPoznatoURazdoblju(t *testing.T) {
	r := razdobljeSnowgrida{100, 200}
	preuzeto := map[int64]int64{}
	poznatoURazdoblju(preuzeto, [2]int64{150, 300}, r) // samo sezona iz izdanja
	if len(preuzeto) != 0 {
		t.Errorf("razdoblje bez početka uzeto kao preuzeto: %v", preuzeto)
	}
	poznatoURazdoblju(preuzeto, [2]int64{50, 300}, r)
	if preuzeto[100] != 200 {
		t.Errorf("pokriveno razdoblje: %v", preuzeto)
	}
	preuzeto[100] = 250
	poznatoURazdoblju(preuzeto, [2]int64{50, 120}, r)
	if preuzeto[100] != 250 {
		t.Errorf("skraćeno preuzeto: %v", preuzeto)
	}
	if redci, od, do := redciPoDanu(nil); len(redci) != 0 || od != 0 || do != 0 {
		t.Errorf("prazan niz: %v %d %d", redci, od, do)
	}
}
