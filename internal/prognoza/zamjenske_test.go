package prognoza

import (
	"database/sql"
	"testing"
)

func cmU(v int64) sirovoOcitanje { return sirovoOcitanje{cm: sql.NullInt64{Int64: v, Valid: true}} }

func TestPopuniSirovoRupeIKraj(t *testing.T) {
	komarom := map[int64]sirovoOcitanje{}
	komarno := map[int64]sirovoOcitanje{}
	for h := int64(0); h < 60; h++ {
		komarno[h] = cmU(20 - h/10)
		// Komárom je 46 cm niži, s rupom od 30. do 34. sata i bez zadnjih 10 sati.
		if (h < 30 || h > 34) && h < 50 {
			komarom[h] = cmU(20 - h/10 - 46)
		}
	}
	komarom[10] = cmU(-25) // jedan sat odstupa, medijan ga ne vidi
	n, z := PopuniSirovo(komarom, komarno)
	if z.Pomak != -46 {
		t.Fatalf("pomak %d, očekivano -46", z.Pomak)
	}
	if n != 15 {
		t.Fatalf("popunjeno %d sati, očekivano 15 (5 rupa + 10 na kraju)", n)
	}
	if v := komarom[32].cm.Int64; v != 20-3-46 {
		t.Fatalf("sat 32 = %d", v)
	}
	if v := komarom[59].cm.Int64; v != 20-5-46 {
		t.Fatalf("sat 59 = %d", v)
	}
	if komarom[10].cm.Int64 != -25 {
		t.Fatal("vlastito mjerenje se ne smije prepisati")
	}
}

func TestPopuniSirovoPremaloZajednickih(t *testing.T) {
	a := map[int64]sirovoOcitanje{}
	b := map[int64]sirovoOcitanje{}
	for h := int64(0); h < ObalaNajmanjeSati-1; h++ {
		a[h], b[h] = cmU(10), cmU(20)
	}
	b[100] = cmU(20)
	if n, _ := PopuniSirovo(a, b); n != 0 {
		t.Fatalf("s %d zajedničkih sati ne smije popunjavati, popunjeno %d", ObalaNajmanjeSati-1, n)
	}
}

func TestDrugaObalaUObaSmjera(t *testing.T) {
	for _, p := range ParoviObala {
		if d, ok := DrugaObala(p[0]); !ok || d != p[1] {
			t.Errorf("%s → %s", p[0], d)
		}
		if d, ok := DrugaObala(p[1]); !ok || d != p[0] {
			t.Errorf("%s → %s", p[1], d)
		}
	}
	if _, ok := DrugaObala("osijek"); ok {
		t.Error("Osijek nema para")
	}
}

// Sat koji druga obala ima bez vodostaja ne popunjava ništa, a sat u kojem
// letva ima barem protok ostaje njezin.
func TestPopuniSirovoSamoVodostajemDrugeObale(t *testing.T) {
	letva := map[int64]sirovoOcitanje{}
	druga := map[int64]sirovoOcitanje{}
	for h := int64(0); h < ObalaNajmanjeSati; h++ {
		letva[h], druga[h] = cmU(100), cmU(90)
	}
	druga[30] = sirovoOcitanje{q: sql.NullFloat64{Float64: 500, Valid: true}}
	letva[31] = sirovoOcitanje{q: sql.NullFloat64{Float64: 480, Valid: true}}
	druga[31] = cmU(95)
	druga[32] = cmU(96)
	n, z := PopuniSirovo(letva, druga)
	if n != 1 || z.Pomak != 10 || z.Zajed != ObalaNajmanjeSati {
		t.Fatalf("popunjeno %d, zamjena %+v; želim 1 sat uz pomak 10 na %d sati", n, z, ObalaNajmanjeSati)
	}
	if _, ima := letva[30]; ima {
		t.Error("sat bez vodostaja druge obale je popunjen")
	}
	if s := letva[31]; s.cm.Valid || s.q.Float64 != 480 {
		t.Errorf("izmjereni protok letve prepisan: %+v", s)
	}
	if s := letva[32]; s.cm.Int64 != 106 {
		t.Errorf("sat 32 = %d, želim 96 + 10", s.cm.Int64)
	}
}

// Opis zamjene kaže koliko mjerenje kasni, s koje letve su sati i uz koji pomak.
func TestOpisZamjene(t *testing.T) {
	z := Zamjena{Iz: "komarno", Pomak: -46, Sati: 3, Zajed: 120}
	want := "zamjena: mjerenje kasni 3 h; ti su sati uzeti s druge obale, s letve komarno uz pomak -46 cm (razlika nula na 120 zajedničkih sati)"
	if got := z.Opis(); got != want {
		t.Errorf("%q\nželim %q", got, want)
	}
}
