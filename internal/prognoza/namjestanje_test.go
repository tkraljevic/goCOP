package prognoza

import (
	"math"
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
