package prognoza

import (
	"math"
	"path/filepath"
	"testing"
)

// Rješavač ne smije dirati ono što mu se preda. Kad je radio na mjestu,
// ocjena je poslije posegnula za desnom stranom, dobila prerađene brojke i
// vratila negativan R² — a traženje kašnjenja je onda uzimalo najveći broj
// koji mu je dopušten.
func TestRjesavacNeDiraUlaz(t *testing.T) {
	A := [][]float64{{4, 1}, {1, 3}}
	b := []float64{1, 2}
	prijeA := [][]float64{{4, 1}, {1, 3}}
	prijeB := []float64{1, 2}
	k := rijesiSRezervom(A, b)
	for i := range A {
		for j := range A[i] {
			if A[i][j] != prijeA[i][j] {
				t.Errorf("A[%d][%d] promijenjen: %g umjesto %g", i, j, A[i][j], prijeA[i][j])
			}
		}
		if b[i] != prijeB[i] {
			t.Errorf("b[%d] promijenjen: %g umjesto %g", i, b[i], prijeB[i])
		}
	}
	// 4k0 + k1 = 1, k0 + 3k1 = 2  →  k = (1/11, 7/11)
	if math.Abs(k[0]-1.0/11) > 1e-9 || math.Abs(k[1]-7.0/11) > 1e-9 {
		t.Errorf("rješenje %v", k)
	}
}

// R² ne može biti negativan kad model ima slobodni član: najgore što se može
// dogoditi jest da pogodi prosjek, a to je nula.
func TestOcjenaNijeNegativna(t *testing.T) {
	cilj, u := izmisljen(5, 2, 3)
	ulazi := []ulazNiz{u}
	sati := sviSati(cilj)
	for pom := 0; pom <= 20; pom++ {
		r2, ok := ocjena(cilj, ulazi, []int{pom}, []int{1}, sati)
		if !ok {
			t.Fatalf("pomak %d: nema ocjene", pom)
		}
		if r2 < -1e-9 {
			t.Errorf("pomak %d: R² %.4f", pom, r2)
		}
	}
}

// Na nizu u kojem veza stoji točno, traženje mora naći upravo to kašnjenje i
// R² blizu jedinice.
func TestOcjenaNadeTocnoKasnjenje(t *testing.T) {
	cilj, u := izmisljen(7, 2, 3)
	ulazi := []ulazNiz{u}
	sati := sviSati(cilj)

	r2, _ := ocjena(cilj, ulazi, []int{7}, []int{1}, sati)
	if r2 < 0.999 {
		t.Errorf("na točnom kašnjenju R² %.4f", r2)
	}
	lagovi, sirine := najboljiLagovi(cilj, ulazi, sati, []int{0}, []int{1}, []int{0})
	if lagovi[0] != 7 {
		t.Errorf("našao kašnjenje %d umjesto 7", lagovi[0])
	}
	// Veza je čista, bez prigušenja, pa prozor ne smije biti širok.
	if sirine[0] > 3 {
		t.Errorf("zagladio prozorom od %d sati na nizu bez prigušenja", sirine[0])
	}
}

// izmisljen gradi ulaz i cilj u kojem je cilj[t] = nagib·ulaz[t-pomak] + odsjecak.
func izmisljen(pomak int, nagib, odsjecak float64) (map[int64]float64, ulazNiz) {
	niz := map[int64]float64{}
	cilj := map[int64]float64{}
	for t := int64(0); t < 12000; t++ {
		niz[t] = 100 + 40*math.Sin(float64(t)/37) + 15*math.Sin(float64(t)/211)
	}
	for t := int64(pomak); t < 12000; t++ {
		cilj[t] = nagib*niz[t-int64(pomak)] + odsjecak
	}
	return cilj, noviUlazNiz("proba", "protok", niz, 20)
}

func sviSati(cilj map[int64]float64) []int64 {
	out := make([]int64, 0, len(cilj))
	for t := range cilj {
		out = append(out, t)
	}
	return out
}

// Letva se vodi u točno jednoj veličini. Kad je promijeni, stari pojasi moraju
// otići — inače ostanu visjeti uz nove i račun posegne za krivima.
func TestSpremanjeMicePojaseStareVelicine(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stari := []Pojas{{Letva: "aljmas", Velicina: "protok", Od: 0, Do: 100,
		Ulazi: []Ulaz{{Letva: "batina", Velicina: "protok", Nagib: 1}}}}
	if err := Spremi(db, stari, "jučer"); err != nil {
		t.Fatal(err)
	}
	novi := []Pojas{{Letva: "aljmas", Velicina: "vodostaj", Od: 0, Do: 100,
		Ulazi: []Ulaz{{Letva: "batina", Velicina: "vodostaj", Nagib: 1}}}}
	if err := Spremi(db, novi, "danas"); err != nil {
		t.Fatal(err)
	}
	p, err := ZaLetvu(db, "aljmas")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 {
		t.Fatalf("%d pojasa umjesto 1: %+v", len(p), p)
	}
	if p[0].Velicina != "vodostaj" {
		t.Errorf("ostala je %s", p[0].Velicina)
	}
}
