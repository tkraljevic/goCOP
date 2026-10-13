package prognoza

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

// podmetniPrijenose stavlja naučene odnose kao današnje i vraća zatečene kad
// test završi. Zaliha vrijedi za dan po satu računala, kao i u programu.
func podmetniPrijenose(t *testing.T, odnosi map[string]Prijenos) {
	t.Helper()
	prijenosi.Lock()
	dan, stari := prijenosi.dan, prijenosi.odnosi
	prijenosi.dan, prijenosi.odnosi = time.Now().UTC().Format("2006-01-02"), odnosi
	prijenosi.Unlock()
	t.Cleanup(func() {
		prijenosi.Lock()
		prijenosi.dan, prijenosi.odnosi = dan, stari
		prijenosi.Unlock()
	})
}

// Prenesena prognoza srpskih letvi iz izdane prognoze naših: satno gdje svi
// izvori imaju prognozu, dnevno gdje svi imaju dnevnu, uz pomak prema
// stvarnom očitanju letve kad on nije veći od četiri rasapa odnosa.
func TestPrenesenaPrognozaIzIzdaneNase(t *testing.T) {
	const sat = int64(493000)
	sada := time.Unix(sat*3600, 0).UTC()
	od := sada.Add(-48 * time.Hour)
	ocitanja := praznaOcitanjaRacuna(t)
	stalno := func(v int) func(int) int { return func(int) int { return v } }
	dodajSatnaOcitanja(t, ocitanja, "batina", od, sada, stalno(200))
	dodajSatnaOcitanja(t, ocitanja, "bezdan", od, sada, stalno(180))
	dodajSatnaOcitanja(t, ocitanja, "aljmas", od, sada, stalno(150))
	dodajSatnaOcitanja(t, ocitanja, "ilok", od, sada, stalno(100))
	dodajSatnaOcitanja(t, ocitanja, "backa-palanka", od, sada, stalno(150))
	// Apatin ima samo nečitljivo očitanje: prognoza se prenosi bez pomaka i
	// bez sidra.
	if _, err := ocitanja.Exec(`INSERT INTO stations VALUES ('apatin', 'apatin');
		INSERT INTO readings (station_id, measured_at, level_cm) VALUES ('apatin', 'nije vrijeme', 170)`); err != nil {
		t.Fatal(err)
	}

	podmetniPrijenose(t, map[string]Prijenos{
		"bezdan":        {Letva: "bezdan", Izvori: []string{"batina"}, Koef: []float64{-21, 1}, Rasap: 4, Dana: 300},
		"apatin":        {Letva: "apatin", Izvori: []string{"batina", "aljmas"}, Koef: []float64{0, 0.5, 0.5}, Rasap: 7, Dana: 365},
		"backa-palanka": {Letva: "backa-palanka", Izvori: []string{"ilok"}, Koef: []float64{-1, 1}, Rasap: 2, Dana: 49},
		// Bogojevo odnosa nema, pa se ni ne prenosi.
	})
	satna := func(letva string, ciljni int64, v, h float64) Izdana {
		return Izdana{Letva: letva, Velicina: "vodostaj", Izdano: sat, Ciljni: ciljni, Vrijednost: v, Dolje: v - h, Gore: v + h, Model: ModelLanac}
	}
	dnevna := func(letva string, dan int, v, h float64) DnevnaIzdana {
		return DnevnaIzdana{Letva: letva, Izdano: sat, Dan: dan, Ciljni: sat + int64(24*dan), Vrijednost: v, Dolje: v - h, Gore: v + h, Model: ModelDnevni}
	}
	ishod := &Ishod{Sada: sat,
		Izdane: []Izdana{
			satna("batina", sat, 200, 0), // sat izdavanja nije prognoza
			satna("batina", sat+1, 201, 2), satna("batina", sat+2, 202, 2), satna("batina", sat+3, 203, 2),
			{Letva: "batina", Velicina: "protok", Izdano: sat, Ciljni: sat + 1, Vrijednost: 900, Dolje: 880, Gore: 920},
			satna("aljmas", sat+1, 151, 4), satna("aljmas", sat+2, 152, 4),
			satna("ilok", sat+1, 101, 1), satna("ilok", sat+2, 102, 1),
		},
		Dnevne: []DnevnaIzdana{dnevna("batina", 1, 210, 10), dnevna("batina", 2, 220, 10), dnevna("aljmas", 1, 160, 6)},
	}
	o := &Osvjezivac{Ocitanja: ocitanja, Model: "proba"}
	izdane, dnevne, izbor := o.prenesene(context.Background(), ishod, od)

	z := math.Sqrt2 * math.Erfinv(UdioURasponu)
	type kljuc struct {
		letva  string
		ciljni int64
	}
	po := map[kljuc]Izdana{}
	for _, i := range izdane {
		if i.Velicina != "vodostaj" || i.Izdano != sat || i.Model != "proba" {
			t.Errorf("izdana %+v", i)
		}
		po[kljuc{i.Letva, i.Ciljni}] = i
	}
	if len(izdane) != 9 || len(po) != 9 {
		t.Fatalf("%d izdanih (%d različitih), želim 9: %+v", len(izdane), len(po), izdane)
	}
	provjeri := func(letva string, ciljni int64, v, w float64) {
		t.Helper()
		i, ima := po[kljuc{letva, ciljni}]
		if !ima || math.Abs(i.Vrijednost-v) > 1e-9 || math.Abs(i.Dolje-(v-w)) > 1e-9 || math.Abs(i.Gore-(v+w)) > 1e-9 {
			t.Errorf("%s u %+d h: %+v, želim %v ± %v", letva, ciljni-sat, i, v, w)
		}
	}
	// Bezdan: mjeren 180, odnos daje 179, pa se nosi pomak od 1 cm.
	provjeri("bezdan", sat, 180, 0)
	for k, v := range map[int64]float64{1: 201, 2: 202, 3: 203} {
		provjeri("bezdan", sat+k, v-21+1, math.Sqrt(2*2+z*z*4*4))
	}
	// Apatin: bez čitljivog očitanja nema ni sidra ni pomaka; treći sat
	// Aljmaš nema.
	for k, v := range map[int64]float64{1: 0.5*201 + 0.5*151, 2: 0.5*202 + 0.5*152} {
		provjeri("apatin", sat+k, v, math.Sqrt(math.Pow(0.5*2+0.5*4, 2)+z*z*7*7))
	}
	// Bačka Palanka stoji 51 cm iznad odnosa, više od četiri rasapa: pomak
	// se ne uzima, a mjerenje ipak stoji u satu izdavanja.
	provjeri("backa-palanka", sat, 150, 0)
	for k, v := range map[int64]float64{1: 101, 2: 102} {
		provjeri("backa-palanka", sat+k, v-1, math.Sqrt(1+z*z*2*2))
	}

	if len(dnevne) != 3 {
		t.Fatalf("%d dnevnih, želim 3: %+v", len(dnevne), dnevne)
	}
	for _, d := range dnevne {
		var v, w float64
		switch {
		case d.Letva == "bezdan" && d.Dan == 1:
			v, w = 210-21+1, math.Sqrt(10*10+z*z*4*4)
		case d.Letva == "bezdan" && d.Dan == 2:
			v, w = 220-21+1, math.Sqrt(10*10+z*z*4*4)
		case d.Letva == "apatin" && d.Dan == 1:
			v, w = 0.5*210+0.5*160, math.Sqrt(math.Pow(0.5*10+0.5*6, 2)+z*z*7*7)
		default:
			t.Errorf("neočekivana dnevna %+v", d)
			continue
		}
		if math.Abs(d.Vrijednost-v) > 1e-9 || math.Abs(d.Gore-d.Vrijednost-w) > 1e-9 || d.Ciljni != sat+int64(24*d.Dan) ||
			d.Izdano != sat || d.Model != ModelDnevni {
			t.Errorf("dnevna %+v, želim %v ± %v", d, v, w)
		}
	}

	want := map[string]string{
		"preneseno:bezdan":        "batina|4.0|300|1",
		"preneseno:apatin":        "batina,aljmas|7.0|365|0",
		"preneseno:backa-palanka": "ilok|2.0|49|0",
	}
	if len(izbor) != len(want) {
		t.Errorf("izbor %v", izbor)
	}
	for k, opis := range want {
		if iz := izbor[k]; iz.Inacica != 1 || iz.Opis != opis {
			t.Errorf("%s: %+v, želim inačicu 1 i %q", k, iz, opis)
		}
	}
}

// Odnosi se uče jednom na dan: letva čiji odnos prođe ulazi u zalihu, a
// odnos s dva izvora na kratkom nizu ne prolazi. Drugi poziv istog dana ne
// uči iznova.
func TestPrijenosiZaDanasUceJednomNaDan(t *testing.T) {
	podmetniPrijenose(t, nil)
	prijenosi.Lock()
	prijenosi.dan = ""
	prijenosi.Unlock()
	arhiva := arhivaDnevnih(t, map[string]func(int) float64{
		"batina": func(d int) float64 { return 200 + float64(d%5) },
		"bezdan": func(d int) float64 { return 178 + float64(d%5) },
		"aljmas": func(d int) float64 { return 150 },
	}, 30)
	o := &Osvjezivac{Arhiva: arhiva}
	odnosi := o.prijenosiZaDanas(context.Background())
	b, ima := odnosi["bezdan"]
	if !ima || len(odnosi) != 1 || b.Koef[0] != -22 || b.Koef[1] != 1 || b.Rasap != 2 || b.Dana != 30 {
		t.Fatalf("odnosi %+v; želim samo Bezdan iz Batine, −22 cm, rasap 2, 30 dana", odnosi)
	}
	if got := PrijenosiZaDanas(); len(got) != 1 || got["bezdan"].Koef[0] != -22 {
		t.Errorf("za stranicu: %+v", got)
	}
	// Drugi poziv uzima zalihu, iako arhive više nema.
	o.Arhiva = nil
	if again := o.prijenosiZaDanas(context.Background()); len(again) != 1 {
		t.Errorf("drugi poziv istog dana učio je iznova: %+v", again)
	}
}

// Dva ista izvora na dugom nizu daju sustav bez rješenja: odnos se ne uči, a
// greška imenuje letvu.
func TestNamjestiPrijenosLinearnoOvisniIzvori(t *testing.T) {
	a := func(d int) float64 { return 100 + 50*math.Sin(float64(d)/7) }
	db := arhivaDnevnih(t, map[string]func(int) float64{
		"a": a,
		"c": func(d int) float64 { return 10 + a(d) },
	}, 200)
	_, err := NamjestiPrijenos(context.Background(), db, nil, "c", []string{"a", "a"}, time.Now())
	if err == nil || err.Error() != "c: izvori su linearno ovisni" {
		t.Errorf("dva ista izvora: %v", err)
	}
	// premalo zajedničkih dana
	_, err = NamjestiPrijenos(context.Background(), db, nil, "c", []string{"nema"}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "premalo zajedničkih dana s nema (0)") {
		t.Errorf("izvor bez podataka: %v", err)
	}
}
