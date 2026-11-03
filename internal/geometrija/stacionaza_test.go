package geometrija

import (
	"math"
	"testing"
)

func TestProjektirajNaLiniju(t *testing.T) {
	linija := [][2]float64{{18, 45}, {18.1, 45}, {18.2, 45}}
	p, err := Projektiraj(linija, [2]float64{18.15, 45.01})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(p.UzduzKM-p.UkupnoKM*0.75) > 0.02 {
		t.Errorf("položaj %.3f km, očekivano oko %.3f km", p.UzduzKM, p.UkupnoKM*0.75)
	}
	if p.UdaljenostKM < 1.10 || p.UdaljenostKM > 1.12 {
		t.Errorf("udaljenost od linije %.3f km, očekivano oko 1,11 km", p.UdaljenostKM)
	}
}

func TestProjektirajOdbijaPraznuLiniju(t *testing.T) {
	if _, err := Projektiraj(nil, [2]float64{}); err == nil {
		t.Fatal("očekivana je greška za praznu liniju")
	}
}

func TestKalibracijaInterpoliraIzmeduSusjednihSidara(t *testing.T) {
	linija := [][2]float64{{18, 45}, {18.1, 45}, {18.2, 45}}
	k, err := NovaKalibracija(linija, []Sidro{
		{Rkm: 100, Tocka: [2]float64{18, 45}},
		{Rkm: 90, Tocka: [2]float64{18.1, 45}},
		{Rkm: 80, Tocka: [2]float64{18.2, 45}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Projektiraj(linija, [2]float64{18.15, 45})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := k.RKM(p.UzduzKM)
	if !ok || math.Abs(got-85) > 0.02 {
		t.Fatalf("kalibrirani rkm %.3f (%v), očekivano 85", got, ok)
	}
	uzduz, ok := k.Uzduz(85)
	if !ok || math.Abs(uzduz-p.UzduzKM) > 0.02 {
		t.Fatalf("obrnuta kalibracija %.3f (%v), očekivano %.3f", uzduz, ok, p.UzduzKM)
	}
	if _, ok := k.RKM(-1); ok {
		t.Fatal("kalibracija ne smije nagađati izvan prvog sidra")
	}
}

func TestTockaNaUzduz(t *testing.T) {
	linija := [][2]float64{{18, 45}, {18.1, 45}}
	p := TockaNaUzduz(linija, DuljinaKM(linija)/2)
	if math.Abs(p[0]-18.05) > 0.001 || math.Abs(p[1]-45) > 0.001 {
		t.Fatalf("točka %.6f, %.6f nije sredina linije", p[0], p[1])
	}
}

func TestRasponKalibracije(t *testing.T) {
	linija := [][2]float64{{18, 45}, {18.1, 45}, {18.2, 45}}
	k, err := NovaKalibracija(linija, []Sidro{{Rkm: 100, Tocka: linija[0]}, {Rkm: 80, Tocka: linija[2]}})
	if err != nil {
		t.Fatal(err)
	}
	if lo, hi, ok := k.Raspon(); !ok || lo != 80 || hi != 100 {
		t.Fatalf("raspon %v–%v (%v), očekivano 80–100", lo, hi, ok)
	}
	if _, _, ok := (Kalibracija{}).Raspon(); ok {
		t.Fatal("kalibracija bez sidara nema raspon")
	}
}

func TestDioLinije(t *testing.T) {
	linija := [][2]float64{{18, 45}, {18.1, 45}, {18.2, 45}, {18.3, 45}}
	ukupno := DuljinaKM(linija)
	dio := DioLinije(linija, ukupno/6, ukupno/2)
	if len(dio) != 3 || math.Abs(dio[0][0]-18.05) > 0.001 || dio[1] != linija[1] || math.Abs(dio[2][0]-18.15) > 0.001 {
		t.Fatalf("dio od 18,05 do 18,15 preko vrha 18,1, a dobiven %v", dio)
	}
	unatrag := DioLinije(linija, ukupno/2, ukupno/6)
	if len(unatrag) != 3 || math.Abs(unatrag[0][0]-18.15) > 0.001 || math.Abs(unatrag[2][0]-18.05) > 0.001 {
		t.Fatalf("dio unatrag treba ići od 18,15 do 18,05, a dobiven %v", unatrag)
	}
	if cijela := DioLinije(linija, -1, ukupno+1); len(cijela) != 4 || cijela[0] != linija[0] || cijela[3] != linija[3] {
		t.Fatalf("cijela linija: %v", cijela)
	}
	if DioLinije(linija[:1], 0, 1) != nil {
		t.Fatal("linija s jednom točkom nema dio")
	}
}
