package prognoza

import (
	"context"
	"database/sql"
	"math"
	"reflect"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// arhivaDnevnih je arhiva sa spojem dnevnih vodostaja za zadane letve.
func arhivaDnevnih(t *testing.T, nizovi map[string]func(d int) float64, dana int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL, izvor TEXT, vrsta TEXT, tocnost REAL)`); err != nil {
		t.Fatal(err)
	}
	pocetak := time.Now().UTC().AddDate(0, 0, -dana).Truncate(24 * time.Hour)
	for letva, f := range nizovi {
		for d := 0; d < dana; d++ {
			db.Exec(`INSERT INTO spoj VALUES (?, 'vodostaj', 'dnevni', ?, ?, 'his2000', 'srednjak', 0)`,
				letva, pocetak.AddDate(0, 0, d).Unix(), f(d))
		}
	}
	return db
}

// Odnos s dva izvora uči se najmanjim kvadratima; s kratkim nizom i jednim
// izvorom samo pomak, uz nagib 1 — nagib naučen na tjedan male vode pri
// velikoj bi odlutao.
func TestNamjestiPrijenos(t *testing.T) {
	a := func(d int) float64 { return 100 + 80*math.Sin(float64(d)/9) }
	b := func(d int) float64 { return 50 + 60*math.Cos(float64(d)/13) }
	db := arhivaDnevnih(t, map[string]func(int) float64{
		"a": a, "b": b,
		"c": func(d int) float64 { return 20 + 0.6*a(d) + 0.3*b(d) },
	}, 400)
	p, err := NamjestiPrijenos(context.Background(), db, nil, "c", []string{"a", "b"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(p.Koef[0]-20) > 0.01 || math.Abs(p.Koef[1]-0.6) > 1e-4 || math.Abs(p.Koef[2]-0.3) > 1e-4 || p.Rasap > 0.01 {
		t.Errorf("odnos %+v, želim 20 + 0,6·a + 0,3·b", p)
	}
	if v := p.U([]float64{100, 50}); math.Abs(v-95) > 0.01 {
		t.Errorf("U = %v, želim 95", v)
	}

	kratka := arhivaDnevnih(t, map[string]func(int) float64{
		"ilok": func(d int) float64 { return 30 + float64(d%3) },
		"bp":   func(d int) float64 { return 31 + 1.2*float64(d%3) },
	}, 11)
	p, err = NamjestiPrijenos(context.Background(), kratka, nil, "bp", []string{"ilok"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if p.Koef[1] != 1 || p.Rasap < 2 {
		t.Errorf("kratki niz: %+v — treba nagib 1 i rasap barem 2 cm", p)
	}
	if _, err := NamjestiPrijenos(context.Background(), kratka, nil, "bp", []string{"ilok", "ilok"}, time.Now()); err == nil {
		t.Errorf("dva izvora na kratkom nizu ne smiju proći")
	}
}

// Opis prenesene prognoze imenuje izvore imenom kad ga ima, inače šifrom, i
// kaže koliko odnos drži i na koliko dana.
func TestOpisPrijenosa(t *testing.T) {
	p := Prijenos{Letva: "apatin", Izvori: []string{"batina", "aljmas"}, Rasap: 6.6, Dana: 3650}
	got := p.Opis(map[string]string{"batina": "Batina"})
	if want := "prognoza prenesena s naše (Batina i aljmas), odnos ±7 cm na 3650 dana"; got != want {
		t.Errorf("%q\nželim %q", got, want)
	}
}

// PrijenosiZaDanas daje naučene odnose kao presliku: ono što stranica s njom
// radi ne dira odnose iz kojih se računa.
func TestPrijenosiZaDanasJePreslika(t *testing.T) {
	want := map[string]Prijenos{"bezdan": {Letva: "bezdan", Izvori: []string{"batina"}, Koef: []float64{2, 1}, Rasap: 4, Dana: 300}}
	prijenosi.Lock()
	dan, odnosi := prijenosi.dan, prijenosi.odnosi
	prijenosi.odnosi = want
	prijenosi.Unlock()
	t.Cleanup(func() {
		prijenosi.Lock()
		prijenosi.dan, prijenosi.odnosi = dan, odnosi
		prijenosi.Unlock()
	})

	got := PrijenosiZaDanas()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v, želim %+v", got, want)
	}
	delete(got, "bezdan")
	if _, ima := PrijenosiZaDanas()["bezdan"]; !ima {
		t.Error("brisanje iz preslike izbrisalo je naučeni odnos")
	}
}
