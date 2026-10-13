package prognoza

import (
	"database/sql"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Pravac kroz čvorove mora se u čvoru sastati sam sa sobom: ono što izlazi iz
// jednog pojasa mora biti ono što ulazi u sljedeći. Inače bi val koji raste
// dobio skok u prognozi kakvog u rijeci nema.
func TestSpojeniPravacNemaSkokaNaGranici(t *testing.T) {
	cvorovi := []float64{0, 100, 200, 300}
	var x, y []float64
	for v := 0.0; v <= 300; v += 0.5 {
		x = append(x, v)
		y = append(y, 2*v+10+math.Sin(v)*3) // zakrivljenost i šum
	}
	uCvoru := SpojeniPravac(cvorovi, x, y)

	for i := 0; i+2 < len(cvorovi); i++ {
		g := cvorovi[i+1]
		lijevo := pravacOdsjecka(cvorovi, uCvoru, i)(g)
		desno := pravacOdsjecka(cvorovi, uCvoru, i+1)(g)
		if d := math.Abs(lijevo - desno); d > 1e-9 {
			t.Errorf("u čvoru %.0f pravac skače za %g", g, d)
		}
	}
}

// Na ravnom nizu izlomljeni pravac mora pogoditi upravo taj pravac.
func TestSpojeniPravacPogadaRavanNiz(t *testing.T) {
	cvorovi := []float64{0, 50, 100}
	var x, y []float64
	for v := 0.0; v <= 100; v++ {
		x = append(x, v)
		y = append(y, 1.5*v-20)
	}
	uCvoru := SpojeniPravac(cvorovi, x, y)
	for i, c := range cvorovi {
		if d := math.Abs(uCvoru[i] - (1.5*c - 20)); d > 1e-6 {
			t.Errorf("čvor %.0f: %g umjesto %g", c, uCvoru[i], 1.5*c-20)
		}
	}
}

// Uzak pojas s puno šuma ne smije dati besmislen nagib. Odvojena regresija
// ovdje je davala nagibe preko dva; vezan pravac drži se susjeda.
func TestUzakPojasNeRazvaliNagib(t *testing.T) {
	cvorovi := []float64{0, 100, 115, 300}
	var x, y []float64
	for v := 0.0; v <= 300; v += 0.25 {
		x = append(x, v)
		y = append(y, v+math.Mod(v*7919, 40)-20) // nagib 1, rasap ±20
	}
	uCvoru := SpojeniPravac(cvorovi, x, y)
	nagib := (uCvoru[2] - uCvoru[1]) / (cvorovi[2] - cvorovi[1])
	if nagib < 0.5 || nagib > 1.5 {
		t.Errorf("nagib u uskom pojasu %.2f, očekivan oko 1", nagib)
	}
}

// Cvorovi ispuštaju granice koje se poklapaju: pojas bez širine nema nagiba.
func TestCvoroviIspustajuPoklopljeneGranice(t *testing.T) {
	poredani := make([]float64, 1000)
	for i := range poredani {
		poredani[i] = 100 // letva koja cijelo vrijeme stoji na istom
	}
	if c := Cvorovi(poredani); len(c) != 1 {
		t.Errorf("ravan niz dao %d čvorova: %v", len(c), c)
	}
}

func pravacOdsjecka(cvorovi, uCvoru []float64, i int) func(float64) float64 {
	nagib := (uCvoru[i+1] - uCvoru[i]) / (cvorovi[i+1] - cvorovi[i])
	odsjecak := uCvoru[i] - nagib*cvorovi[i]
	return func(v float64) float64 { return nagib*v + odsjecak }
}

// korelacija je Pearsonov r: ±1 na pravcu, nula kad je niz prekratak ili
// ravan — ravan niz ne kaže ništa o vezi, pa ne smije izgledati kao veza.
func TestKorelacija(t *testing.T) {
	for _, s := range []struct {
		ime  string
		x, y []float64
		want float64
	}{
		{"prazno", nil, nil, 0},
		{"jedna točka", []float64{1}, []float64{2}, 0},
		{"ravan ulaz", []float64{5, 5, 5}, []float64{1, 2, 3}, 0},
		{"ravan cilj", []float64{1, 2, 3}, []float64{7, 7, 7}, 0},
		{"rastući pravac", []float64{1, 2, 3, 4}, []float64{3, 5, 7, 9}, 1},
		{"padajući pravac", []float64{1, 2, 3}, []float64{30, 20, 10}, -1},
		{"pola veze", []float64{1, 2, 3}, []float64{1, 3, 2}, 0.5},
	} {
		if got := korelacija(s.x, s.y); math.Abs(got-s.want) > 1e-12 {
			t.Errorf("%s: r = %g, želim %g", s.ime, got, s.want)
		}
	}
}

// kakoDrzi mjeri namješteni pojas na satima koji imaju i cilj i sve ulaze:
// točan račun daje r 1 i rasap nula, račun pomaknut za 4 cm rasap 4 cm, a
// sati bez cilja ili ulaza ne ulaze u broj.
func TestKakoDrziMjeriPojas(t *testing.T) {
	cilj, u := izmisljen(5, 2, 3)
	ulazi := []ulazNiz{u}
	sati := append(sviSati(cilj), -100, 20000) // bez cilja i bez ulaza
	pojas := func(odsjecak float64) Pojas {
		return Pojas{Letva: "proba", Odsjecak: odsjecak, Ulazi: []Ulaz{{Letva: "proba", Nagib: 2}}}
	}
	r, rasap, n := kakoDrzi(cilj, ulazi, []int{5}, []int{1}, sati, pojas(3))
	if math.Abs(r-1) > 1e-9 || rasap > 1e-9 || n != len(cilj) {
		t.Errorf("točan račun: r %g, rasap %g, sati %d (želim 1, 0, %d)", r, rasap, n, len(cilj))
	}
	r, rasap, n = kakoDrzi(cilj, ulazi, []int{5}, []int{1}, sati, pojas(7))
	if math.Abs(r-1) > 1e-9 || math.Abs(rasap-4) > 1e-9 || n != len(cilj) {
		t.Errorf("račun viši za 4: r %g, rasap %g, sati %d", r, rasap, n)
	}
	// Pojas s drugim brojem ulaza ne može računati ni na jednom satu.
	krivi := Pojas{Letva: "proba", Ulazi: []Ulaz{{Nagib: 1}, {Nagib: 1}}}
	if r, rasap, n := kakoDrzi(cilj, ulazi, []int{5}, []int{1}, sati, krivi); r != 0 || rasap != 0 || n != 0 {
		t.Errorf("pojas koji ne računa: r %g, rasap %g, sati %d — želim nule", r, rasap, n)
	}
	if r, rasap, n := kakoDrzi(cilj, ulazi, []int{5}, []int{1}, nil, pojas(3)); r != 0 || rasap != 0 || n != 0 {
		t.Errorf("bez sati: r %g, rasap %g, sati %d — želim nule", r, rasap, n)
	}
}

// najboljiSam nalazi kašnjenje na kojem se ulaz i cilj slažu, a kad nijedno
// kašnjenje nema dovoljno zajedničkih sati, ostaje na nuli.
func TestNajboljiSamNalaziKasnjenje(t *testing.T) {
	cilj, u := izmisljen(7, 2, 3)
	if got := najboljiSam(cilj, u, sviSati(cilj)); got != 7 {
		t.Errorf("kašnjenje %d, želim 7", got)
	}
	malo := sviSati(cilj)[:NajmanjeSatiPojasa-1]
	if got := najboljiSam(cilj, u, malo); got != 0 {
		t.Errorf("s %d sati kašnjenje %d, želim 0", len(malo), got)
	}
}

// Prazan niz nema prozora: prosjek ga ne nalazi ni za jedan sat.
func TestPrazanUlazNemaProsjeka(t *testing.T) {
	u := noviUlazNiz("proba", "vodostaj", nil, 10)
	if u.ime != "proba" || u.velicina != "vodostaj" || u.najdulje != 10 {
		t.Errorf("prazan niz izgubio je opis: %+v", u)
	}
	if _, ima := u.prosjek(0, 1); ima {
		t.Error("prazan niz dao je prosjek")
	}
}

// arhivaSatnih je arhiva sa satnim nizovima zadanih letvi (letva → sat →
// vrijednost), u veličini zadanoj uz ime: "letva:velicina".
func arhivaSatnih(t *testing.T, nizovi map[string]map[int64]float64) *sql.DB {
	t.Helper()
	db, _ := arhivaUDatoteci(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	upis, err := tx.Prepare(`INSERT INTO spoj VALUES (?, ?, 'satni', ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer upis.Close()
	for kljuc, niz := range nizovi {
		letva, velicina, _ := strings.Cut(kljuc, ":")
		for sat, v := range niz {
			if _, err := upis.Exec(letva, velicina, sat*3600, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db
}

// koliko broji samo satne vrijednosti letve u zadanoj veličini; arhiva bez
// spoja daje nulu.
func TestKolikoBrojiSatne(t *testing.T) {
	db := arhivaSatnih(t, map[string]map[int64]float64{
		"batina:vodostaj": {1: 100, 2: 101, 3: 102},
		"batina:protok":   {1: 900},
	})
	if _, err := db.Exec(`INSERT INTO spoj VALUES ('batina', 'vodostaj', 'dnevni', 0, 100)`); err != nil {
		t.Fatal(err)
	}
	if n := koliko(db, "batina", "vodostaj"); n != 3 {
		t.Errorf("satnih vodostaja %d, želim 3", n)
	}
	if n := koliko(db, "aljmas", "vodostaj"); n != 0 {
		t.Errorf("letva bez niza: %d", n)
	}
	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prazna.Close()
	if n := koliko(prazna, "batina", "vodostaj"); n != 0 {
		t.Errorf("arhiva bez spoja: %d", n)
	}
}
