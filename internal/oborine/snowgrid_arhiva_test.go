package oborine

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
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
			VALUES ('k-a', 'snijeg-na-tlu', 'dnevni', ?, ?, 'snowgrid', 'dnevni', 0)`, d*86400, v); err != nil {
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

// Ulaganje u stablo: prvi put sva povijest točke, poslije od devet dana
// prije zadnjeg uloženog; točka bez dana se preskače. Gradnja iz stabla
// stavlja niz u spojeni dnevni niz na ponoć UTC, i dan prije 1970.
func TestUloziSnijegUStablo(t *testing.T) {
	dir := t.TempDir()
	d, err := Otvori(filepath.Join(dir, "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	koren := filepath.Join(dir, "stablo")
	s := &SnijegNaTlu{DB: d, Tocke: func() ([]Tocka, error) { return []Tocka{{"k-a", 47, 13}, {"k-b", 47, 14}}, nil }}
	prvi := dan(1961, 1, 1)
	if _, err := d.Exec(`INSERT INTO snijeg_na_tlu (kisomjer, dan, swe) VALUES ('k-a', ?, 0), ('k-a', 20000, 55.5), ('k-a', 20010, 60)`, prvi); err != nil {
		t.Fatal(err)
	}
	tocke, err := s.Ulozi(koren, "drava")
	if err != nil || len(tocke) != 1 || tocke[0] != "k-a" {
		t.Fatalf("uložene točke %v (%v)", tocke, err)
	}
	redci, err := arhiva.PostojeciRedci(koren, "drava", "k-a", IzvorSnowgrida, VelicinaSnijega, "dnevni")
	if err != nil || len(redci) != 3 {
		t.Fatalf("u stablu %d redaka (%v)", len(redci), err)
	}
	// drugi put: samo od 20001, a stari dio ostaje
	if _, err := d.Exec(`INSERT INTO snijeg_na_tlu (kisomjer, dan, swe) VALUES ('k-a', 20011, 61)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ulozi(koren, "drava"); err != nil {
		t.Fatal(err)
	}
	if redci, err := arhiva.PostojeciRedci(koren, "drava", "k-a", IzvorSnowgrida, VelicinaSnijega, "dnevni"); err != nil || len(redci) != 4 {
		t.Errorf("nakon dopune %d redaka (%v)", len(redci), err)
	}
	baza := filepath.Join(dir, "vodostaji.db")
	if _, err := arhiva.Izgradi(koren, baza, "k-a", nil); err != nil {
		t.Fatal(err)
	}
	a, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	niz, err := (&SnijegNaTlu{DB: d, Arhiva: a}).izArhive("k-a", prvi, 30000)
	if err != nil || len(niz) != 4 || niz[prvi] != 0 || niz[20000] != 55.5 || niz[20011] != 61 {
		t.Errorf("iz arhive %v (%v)", niz, err)
	}
	ime := filepath.Join(koren, "drava", "k-a")
	if dat, err := os.ReadDir(ime); err != nil || len(dat) == 0 || !strings.HasPrefix(dat[0].Name(), "k-a_snowgrid_snijeg-na-tlu_dnevni_") {
		t.Errorf("datoteke %v (%v)", dat, err)
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
	if _, err := s.Ulozi(t.TempDir(), "drava"); err == nil {
		t.Error("točke bez greške")
	}
	s.Tocke = func() ([]Tocka, error) { return []Tocka{{"k-a", 47, 13}}, nil }
	d.Close()
	if _, err := s.Ulozi(t.TempDir(), "drava"); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
