package plato

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// godisnji je izmišljeni satni niz kroz cijelu godinu (UTC): dnevni val
// amplitude a cm oko razine r, zaokružen na cm, s nizom iste vrijednosti
// od n sati svakih 500 sati.
func godisnji(g int, r, a float64, n int) []Tocka { return godisnjiSvakih(g, r, a, n, 500) }

// godisnjiSvakih je godisnji s nizom iste vrijednosti svakih p sati.
func godisnjiSvakih(g int, r, a float64, n, p int) []Tocka {
	t0 := time.Date(g, 1, 1, 0, 0, 0, 0, time.UTC)
	var out []Tocka
	for h := 0; h < 8760; h++ {
		v := math.Round(r + a*math.Sin(2*math.Pi*float64(h)/24))
		if h%p < n {
			v = r
		}
		out = append(out, Tocka{Kad: t0.Add(time.Duration(h) * time.Hour), Cm: v})
	}
	return out
}

// Trajanje: medijan godišnjih 99,9. percentila trajanja nizova iste
// vrijednosti puta 1,5, a godina u kvaru (niz od 200 h) ne pomakne ga.
func TestTrajanjeIzNiza(t *testing.T) {
	var t3 []Tocka
	t3 = append(t3, godisnji(2020, 100, 30, 12)...)
	t3 = append(t3, godisnji(2021, 100, 30, 12)...)
	t3 = append(t3, godisnji(2022, 100, 30, 200)...)
	got := TrajanjeIzNiza(t3)
	if got < 12*time.Hour || got > 20*time.Hour {
		t.Errorf("trajanje %v", got)
	}
	if got := TrajanjeIzNiza(nil); got != ZadanoTrajanje {
		t.Errorf("bez arhive %v", got)
	}
	// mirna letva bez ijednog niza dužeg od sata: najmanje šest sati
	if got := TrajanjeIzNiza(godisnji(2020, 100, 30, 0)); got != NajkraceTrajanje {
		t.Errorf("najkraće %v", got)
	}
}

// Raspon: godišnji percentili, medijan, i još jedna širina; najmanje
// metar. Najniža je najniža vrijednost niza.
func TestRasponIzNiza(t *testing.T) {
	r := RasponIzNiza(godisnji(2020, 100, 30, 0))
	if r == nil || r.Od != 100-30-100 || r.Do != 100+30+100 || r.Najniza != 70 {
		t.Errorf("raspon %+v", r)
	}
	r = RasponIzNiza(godisnji(2020, 100, 300, 0))
	if r.Od != -200-600 || r.Do != 400+600 {
		t.Errorf("široki raspon %+v", r)
	}
	if RasponIzNiza(godisnji(2020, 100, 30, 0)[:100]) != nil {
		t.Error("raspon iz premalo vrijednosti")
	}
	var r0 *Raspon
	if r0.Daleko(5000) {
		t.Error("letva bez raspona ima daleko")
	}
}

// Prag susjeda: koliko se susjed pomakne dok letva miruje, puta tri,
// najmanje 20 cm; bez dovoljno zajedničkih mirnih nizova susjed ne
// potvrđuje.
func TestPragSusjeda(t *testing.T) {
	letva := godisnjiSvakih(2020, 100, 30, 8, 100)
	mirni := godisnji(2020, 50, 1, 0)
	if p := PragSusjeda(letva, mirni); p != NajmanjiPomakSusjeda {
		t.Errorf("mirni susjed %v", p)
	}
	ziv := godisnji(2020, 50, 20, 0)
	if p := PragSusjeda(letva, ziv); p < 3*30 || p > 3*41 {
		t.Errorf("živ susjed %v", p)
	}
	if p := PragSusjeda(letva[:1000], ziv); !math.IsInf(p, 1) {
		t.Errorf("premalo mirnih %v", p)
	}
}

// probnaArhiva je arhiva s tablicom spoj i satnim nizom letava.
func probnaArhiva(t *testing.T, nizovi map[string][]Tocka) *sql.DB {
	t.Helper()
	baza, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "arhiva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL, izvor TEXT, vrsta TEXT, tocnost REAL)`); err != nil {
		t.Fatal(err)
	}
	tx, err := baza.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for l, n := range nizovi {
		for _, x := range n {
			if _, err := tx.Exec(`INSERT INTO spoj VALUES (?, 'vodostaj', 'satni', ?, ?, 'probni', 'trenutna', 1)`, l, x.Kad.Unix(), x.Cm); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return baza
}

// Niz iz arhive: samo puni sati od 2012., po vremenu.
func TestNizIzArhive(t *testing.T) {
	stari := Tocka{Kad: time.Date(2011, 5, 1, 0, 0, 0, 0, time.UTC), Cm: 1}
	pola := Tocka{Kad: time.Date(2020, 5, 1, 0, 30, 0, 0, time.UTC), Cm: 2}
	dobar := Tocka{Kad: time.Date(2020, 5, 1, 1, 0, 0, 0, time.UTC), Cm: 3}
	a := probnaArhiva(t, map[string][]Tocka{"donji-brod": {dobar, stari, pola}})
	n, err := NizIzArhive(context.Background(), a, "donji-brod")
	if err != nil || len(n) != 1 || n[0].Cm != 3 || !n[0].Kad.Equal(dobar.Kad) {
		t.Errorf("niz %+v %v", n, err)
	}
	if _, err := a.Exec(`DROP TABLE spoj`); err != nil {
		t.Fatal(err)
	}
	if _, err := NizIzArhive(context.Background(), a, "donji-brod"); err == nil {
		t.Error("arhiva bez tablice bez greške")
	}
}
