package prognoza

import (
	"bytes"
	"math"
	"strconv"
	"testing"
	"time"
)

// Rupa u satu izdavanja ne smije se premostiti očitanjem koje dolazi iza
// njega. Uživo tog očitanja još nema, pa račun koji ga vidi pri provjeri
// unatrag mjeri prognozu koja se nikad ne bi izdala. Gornja letva stoji na
// 100 cm do sata 998, sati 999 i 1000 nema, a od 1002 stoji na 160: premošćena
// rupa dala bi u satu izdavanja 130, a uživo se zna samo 100.
func TestRacunNePremoscujeRupuKasnijimOcitanjem(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	sada := int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	m := map[int64]float64{}
	for s := int64(900); s <= 998; s++ {
		m[s] = 100
	}
	for s := int64(1002); s <= 1100; s++ {
		m[s] = 160
	}
	r := NovoRacunalo(lanac(1, 0, 5, 3), map[Izvor]Niz{gornja: NoviNiz(m)}, sada)
	izdane, err := r.Prognoziraj("donja", 12, "proba")
	if err != nil || len(izdane) != 13 {
		t.Fatalf("doseg %d, %v", len(izdane), err)
	}
	for _, i := range izdane {
		if math.Abs(i.Vrijednost-100) > 1e-9 {
			t.Errorf("sat %d: %g, a u satu izdavanja zna se samo 100", i.Ciljni, i.Vrijednost)
		}
	}
}

// Isto pravilo vrijedi i u provjeri unatrag: izdanje u satu t vidi samo
// očitanja do t. Gornja letva skoči sa 100 na 200 cm, a tri sata prije skoka
// nema očitanja. Svako izdanje prije prvog sata na 200 mora za donju letvu
// reći 100 (u probnoj bazi donja je gornja tri sata ranije); premošćena rupa
// dala bi 125, 150 ili 175 i ušla u zapisani promašaj.
func TestProvjeraUnatragNeVidiOcitanjeIzaIzdanja(t *testing.T) {
	bezIspravka(t)
	postavi(t, &DoseziProvjere, []int{1, 2, 3})
	var dnevnik, tablica bytes.Buffer
	o := opcijeStalne(&dnevnik)
	o.Zapisi, o.CSV = false, &tablica
	skok := o.Od.Unix()/3600 + 48 // prvi sat na 200
	od, do := o.Od.Unix()/3600-24, o.Do.Unix()/3600+24
	arhiva := satnaArhivaRacuna(t, "vodostaj", od, do, map[string]func(int64) float64{
		"gornja": func(sat int64) float64 {
			if sat < skok {
				return 100
			}
			return 200
		},
		"donja": func(int64) float64 { return 105 },
	})
	if _, err := arhiva.Exec(`DELETE FROM spoj WHERE letva = 'gornja' AND vrijeme BETWEEN ? AND ?`,
		(skok-3)*3600, (skok-1)*3600); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvjeriUnatrag(arhiva, probnaBazaPrognoza(t), o); err != nil {
		t.Fatal(err)
	}
	provjereno := 0
	for _, red := range redoviCSV(t, &tablica) {
		izdano, err := time.Parse("2006-01-02 15:04", red[0])
		if err != nil {
			t.Fatal(err)
		}
		if izdano.Unix()/3600 >= skok {
			continue
		}
		if v, err := strconv.ParseFloat(red[5], 64); err != nil || v != 100 {
			t.Errorf("izdano %s, doseg %s h: %s, a do sata izdavanja gornja je stajala na 100", red[0], red[4], red[5])
		}
		provjereno++
	}
	if provjereno == 0 {
		t.Fatal("nijedno izdanje prije skoka nije provjereno")
	}
}
