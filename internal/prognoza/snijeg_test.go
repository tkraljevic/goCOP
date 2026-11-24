package prognoza

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

// Zaliha snijega: ispod praga snijeg ide u zalihu, iznad praga se topi
// faktor po °C, ali najviše koliko je u zalihi; snijeg ne može biti veći od
// oborine ni manji od nule, a dan bez snijega ili temperature zalihu ne dira.
func TestKorakSnijega(t *testing.T) {
	for _, s := range []struct {
		ime                  string
		zaliha               float64
		dan                  DanOborine
		nova, palo, topi, ul float64
	}{
		{"snijeg na mrazu", 0, DanOborine{Oborina: 10, Snijeg: 10, Temperatura: -5, ImaSnijega: true}, 10, 10, 0, 0},
		{"kiša i snijeg", 4, DanOborine{Oborina: 10, Snijeg: 6, Temperatura: -1, ImaSnijega: true}, 10, 6, 0, 4},
		{"točno na pragu", 10, DanOborine{Temperatura: 0, ImaSnijega: true}, 10, 0, 0, 0},
		{"otapanje", 10, DanOborine{Temperatura: 2, ImaSnijega: true}, 4, 0, 6, 6},
		{"otopi se sve, ne više", 4, DanOborine{Oborina: 1, Temperatura: 10, ImaSnijega: true}, 0, 0, 4, 5},
		{"snijeg veći od oborine", 0, DanOborine{Oborina: 3, Snijeg: 8, Temperatura: -1, ImaSnijega: true}, 3, 3, 0, 0},
		{"snijeg ispod nule", 2, DanOborine{Oborina: 3, Snijeg: -2, Temperatura: -1, ImaSnijega: true}, 2, 0, 0, 3},
		{"bez snijega i temperature", 7, DanOborine{Oborina: 5, Snijeg: 5, Temperatura: 20}, 7, 0, 0, 5},
	} {
		nova, palo, topi := KorakSnijega(s.zaliha, s.dan)
		if nova != s.nova || palo != s.palo || topi != s.topi || s.dan.Oborina-palo+topi != s.ul {
			t.Errorf("%s: zaliha %v, palo %v, topi %v; želim %v, %v, %v (ulaz %v)", s.ime, nova, palo, topi, s.nova, s.palo, s.topi, s.ul)
		}
		if nova < 0 {
			t.Errorf("%s: zaliha ispod nule: %v", s.ime, nova)
		}
	}

	postavi(t, &TopljenjeSnijega, 5)
	postavi(t, &PragTopljenja, 1)
	if nova, _, topi := KorakSnijega(20, DanOborine{Temperatura: 3, ImaSnijega: true}); nova != 10 || topi != 10 {
		t.Errorf("faktor 5 iznad 1 °C: zaliha %v, topi %v; želim 10, 10", nova, topi)
	}
}

// Sezona snijega počinje 1. kolovoza UTC.
func TestDanUSezoni(t *testing.T) {
	dan := func(g int, m time.Month, d int) int64 { return time.Date(g, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400 }
	for _, s := range []struct{ dan, want int64 }{
		{dan(2026, time.July, 31), dan(2025, time.August, 1)},
		{dan(2026, time.August, 1), dan(2026, time.August, 1)},
		{dan(2027, time.January, 15), dan(2026, time.August, 1)},
	} {
		if got := danUSezoni(s.dan); got != s.want {
			t.Errorf("danUSezoni(%d) = %d, želim %d", s.dan, got, s.want)
		}
	}
}

// U učenju snijeg hladnih dana čeka u zalihi i otječe tek kad zatopli; dan
// bez snijega ili temperature ostaje kakav je pao, a točka bez nizova snijega
// daje isto što i ukupna oborina. Ukupna oborina (za pragove kartice „Jače
// oborine”) ne gleda snijeg.
func TestDnevneOborineSaSnijegom(t *testing.T) {
	arhiva, _ := arhivaUDatoteci(t)
	ob := []float64{10, 0, 0, 5, 4, 2}
	sn := []float64{10, 0, 0, 0, 4, 0}
	te := []float64{-5, -1, 2, 10, -3, 1}
	upisiDnevne(t, arhiva, "tocka-a", "oborina", 6, func(d int64) float64 { return ob[d] })
	upisiDnevne(t, arhiva, "tocka-a", "snijeg", 6, func(d int64) float64 { return sn[d] })
	// petom danu (indeks 4) temperatura fali: snijeg tog dana ostaje u oborini
	upisiDnevne(t, arhiva, "tocka-a", "temperatura-zraka", 4, func(d int64) float64 { return te[d] })
	if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('tocka-a', 'temperatura-zraka', 'dnevni', 5*86400, 1)`); err != nil {
		t.Fatal(err)
	}
	upisiDnevne(t, arhiva, "tocka-b", "oborina", 6, func(d int64) float64 { return ob[d] })
	tocke := []OborinskaTocka{{Code: "tocka-a", Sliv: "A", Tezina: 1}, {Code: "tocka-b", Sliv: "B", Tezina: 1}}

	got, err := DnevneOborine(arhiva, tocke)
	if err != nil {
		t.Fatal(err)
	}
	// zaliha: 10, 10, 4 (otopi 6), 0 (otopi 4), 0 (dan bez temperature), 0
	want := map[string]DnevniNiz{
		OborinaKljuc("A"): {0: 0, 1: 0, 2: 6, 3: 9, 4: 4, 5: 2},
		OborinaKljuc("B"): {0: 10, 1: 0, 2: 0, 3: 5, 4: 4, 5: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("s modelom snijega %v, želim %v", got, want)
	}
	ukupne, err := UkupneDnevneOborine(arhiva, tocke)
	wantUk := map[string]DnevniNiz{OborinaKljuc("A"): want[OborinaKljuc("B")], OborinaKljuc("B"): want[OborinaKljuc("B")]}
	if err != nil || !reflect.DeepEqual(ukupne, wantUk) {
		t.Errorf("ukupna oborina %v, %v; želim %v", ukupne, err, wantUk)
	}

	for _, velicina := range []string{"snijeg", "temperatura-zraka"} {
		t.Run(velicina+" se ne da pročitati", func(t *testing.T) {
			db, _ := arhivaUDatoteci(t)
			for _, v := range []string{"oborina", "snijeg", "temperatura-zraka"} {
				upisiDnevne(t, db, "tocka-a", v, 3, func(int64) float64 { return 1 })
			}
			zamijeniStupac(t, db, "spoj", "vrijednost", pucaNa("velicina = '"+velicina+"'", "vrijednost"), "letva", "vrijeme")
			if got, err := DnevneOborine(db, tocke[:1]); err == nil {
				t.Errorf("bez greške: %v", got)
			}
		})
	}
}

// satiDana je 24 sata dana d s istom vrijednošću, bez sati iz bez.
func satiDana(d int64, v float64, bez ...int64) map[int64]float64 {
	n := map[int64]float64{}
	for h := d * 24; h < d*24+24; h++ {
		n[h] = v
	}
	for _, h := range bez {
		delete(n, d*24+h)
	}
	return n
}

// spoji slaže satne nizove više dana u jedan.
func spoji(nizovi ...map[int64]float64) map[int64]float64 {
	out := map[int64]float64{}
	for _, n := range nizovi {
		for h, v := range n {
			out[h] = v
		}
	}
	return out
}

// Uživo snijeg hladnog dana ne ide u model nego čeka u zalihi, a otopljeno
// toplog dana raspodijeli se jednako na njegova 24 sata. Bez izvora snijega
// (BezSnijega) kiša je oborina kakva je pala, kao i dosad.
func TestOborineOkoSadaSaSnijegom(t *testing.T) {
	const dan = int64(20000)
	const sada = dan*24 + 23 // dan 0 je cijeli dan UTC
	var trazeno [][2]int64
	var zalihaZa []int64
	iz := &OborinskiIzvor{
		Tocke: []OborinskaTocka{{Code: "a", Sliv: "S", Tezina: 1}},
		Satne: func(od, do int64) (map[string]map[int64]float64, error) {
			trazeno = append(trazeno, [2]int64{od, do})
			return map[string]map[int64]float64{"a": spoji(satiDana(dan-1, 1), satiDana(dan, 0), satiDana(dan+1, 0.5))}, nil
		},
		Snijeg: func(od, do int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			return map[string]map[int64]float64{"a": spoji(satiDana(dan-1, 1), satiDana(dan, 0), satiDana(dan+1, 0))},
				map[string]map[int64]float64{"a": spoji(satiDana(dan-1, -5), satiDana(dan, 3), satiDana(dan+1, 10))}, nil
		},
		ZalihaSnijega: func(tocka string, d int64) (float64, error) {
			zalihaZa = append(zalihaZa, d)
			return 2, nil
		},
	}
	got, err := OborineOkoSada(iz, sada, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	// zaliha 2 + 24 = 26; drugi dan otopi 9 (ostaje 17), treći svih 17 uz 12 mm kiše
	want := map[string]DnevniNiz{OborinaKljuc("S"): {-1: 0, 0: 9, 1: 29}}
	if !reflect.DeepEqual(zaokruzeno(got), want) {
		t.Errorf("s modelom snijega %v, želim %v", got, want)
	}
	if !reflect.DeepEqual(zalihaZa, []int64{dan - 1}) {
		t.Errorf("zaliha tražena za dane %v, želim %d", zalihaZa, dan-1)
	}
	bez, err := OborineOkoSada(iz.BezSnijega(), sada, 2, 1)
	if err != nil || !reflect.DeepEqual(bez, map[string]DnevniNiz{OborinaKljuc("S"): {-1: 24, 0: 0, 1: 12}}) {
		t.Errorf("bez modela snijega %v, %v", bez, err)
	}
	if iz.Snijeg == nil || iz.ZalihaSnijega == nil {
		t.Error("BezSnijega je promijenio izvor")
	}
	if (*OborinskiIzvor)(nil).BezSnijega() != nil {
		t.Error("BezSnijega bez izvora nije nil")
	}

	t.Run("sat usred dana traži cijele dane", func(t *testing.T) {
		trazeno = nil
		if _, err := OborineOkoSada(iz, sada-12, 2, 1); err != nil {
			t.Fatal(err)
		}
		if want := [][2]int64{{(dan - 2) * 24, (dan+1)*24 + 23}}; !reflect.DeepEqual(trazeno, want) {
			t.Errorf("satni nizovi traženi za %v, želim %v", trazeno, want)
		}
	})
	t.Run("bez zalihe i nepotpuni dani", func(t *testing.T) {
		bezZalihe := *iz
		bezZalihe.ZalihaSnijega = nil
		bezZalihe.Snijeg = func(od, do int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			// prvi dan sat snijega fali, drugi samo 19 sati temperature: oba ostaju
			return map[string]map[int64]float64{"a": spoji(satiDana(dan-1, 1, 5), satiDana(dan, 0), satiDana(dan+1, 0))},
				map[string]map[int64]float64{"a": spoji(satiDana(dan-1, -5), satiDana(dan, 3, 0, 1, 2, 3, 4), satiDana(dan+1, 10))}, nil
		}
		got, err := OborineOkoSada(&bezZalihe, sada, 2, 1)
		if err != nil || !reflect.DeepEqual(got, map[string]DnevniNiz{OborinaKljuc("S"): {-1: 24, 0: 0, 1: 12}}) {
			t.Errorf("%v, %v", got, err)
		}
	})
	ne := errors.New("ne radi")
	// bez snijega ili zalihe model dobiva oborinu kakva je pala, a ne ostaje bez kiše
	kakvaJePala := map[string]DnevniNiz{OborinaKljuc("S"): {-1: 24, 0: 0, 1: 12}}
	t.Run("snijeg se ne da pročitati", func(t *testing.T) {
		pokvaren := *iz
		pokvaren.Snijeg = func(int64, int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			return nil, nil, ne
		}
		if got, err := OborineOkoSada(&pokvaren, sada, 2, 1); err != nil || !reflect.DeepEqual(got, kakvaJePala) {
			t.Errorf("%v, %v", got, err)
		}
	})
	t.Run("satni nizovi se ne daju pročitati", func(t *testing.T) {
		pokvaren := *iz
		pokvaren.Satne = func(int64, int64) (map[string]map[int64]float64, error) { return nil, ne }
		if got, err := OborineOkoSada(&pokvaren, sada, 2, 1); !errors.Is(err, ne) || got != nil {
			t.Errorf("%v, %v", got, err)
		}
	})
	t.Run("zaliha se ne da izračunati", func(t *testing.T) {
		pokvaren := *iz
		pokvaren.ZalihaSnijega = func(string, int64) (float64, error) { return 0, ne }
		if got, err := OborineOkoSada(&pokvaren, sada, 2, 1); err != nil || !reflect.DeepEqual(got, kakvaJePala) {
			t.Errorf("%v, %v", got, err)
		}
	})
}

// zaokruzeno je kiša na tisućinku milimetra, da zbroj 24 dijela ne smeta
// usporedbi.
func zaokruzeno(m map[string]DnevniNiz) map[string]DnevniNiz {
	out := map[string]DnevniNiz{}
	for k, n := range m {
		out[k] = DnevniNiz{}
		for d, v := range n {
			out[k][d] = float64(int64(v*1000+0.5)) / 1000
		}
	}
	return out
}

// Zaliha na početku dana: od 1. kolovoza dan po dan, iz arhive gdje je ima,
// inače iz satnih nizova baze oborina; dan bez ijednog zalihu ne mijenja.
// Baza oborina čita se jednom na dan, a zaliha točke računa jednom.
func TestZalihaSnijegaNaPocetku(t *testing.T) {
	pocetak := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC).Unix() / 86400
	arhiva, _ := arhivaUDatoteci(t)
	upis := func(velicina string, d int64, v float64) {
		t.Helper()
		if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('a', ?, 'dnevni', ?, ?)`, velicina, d*86400, v); err != nil {
			t.Fatal(err)
		}
	}
	// arhiva: dan 0 snijeg 20 na −3 °C, dan 1 kiša 5 na −1 °C, dan 2 bez
	// snijega na 2 °C (otopi 6), i dan prije sezone koji se ne smije brojati
	for d, x := range [][3]float64{{20, 20, -3}, {5, 0, -1}, {0, 0, 2}} {
		upis("oborina", pocetak+int64(d), x[0])
		upis("snijeg", pocetak+int64(d), x[1])
		upis("temperatura-zraka", pocetak+int64(d), x[2])
	}
	upis("oborina", pocetak-1, 50)
	upis("snijeg", pocetak-1, 50)
	upis("temperatura-zraka", pocetak-1, -10)
	// baza oborina: dan 2 (arhiva ima prednost), dan 3 snijeg 12 na −4 °C,
	// dan 4 topi na 1 °C sa 20 sati temperature, dan 5 bez 24 sata oborine
	ob := spoji(satiDana(pocetak+2, 0), satiDana(pocetak+3, 0.5), satiDana(pocetak+4, 0), satiDana(pocetak+5, 1, 7))
	sn := spoji(satiDana(pocetak+2, 0), satiDana(pocetak+3, 0.5), satiDana(pocetak+4, 0), satiDana(pocetak+5, 1))
	te := spoji(satiDana(pocetak+2, 30), satiDana(pocetak+3, -4), satiDana(pocetak+4, 1, 0, 1, 2, 3), satiDana(pocetak+5, -9))
	citanja := 0
	z := &ZalihaSnijega{Arhiva: arhiva,
		Satne: func(od, do int64) (map[string]map[int64]float64, error) {
			citanja++
			return map[string]map[int64]float64{"a": ob, "b": ob}, nil
		},
		Snijeg: func(od, do int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			return map[string]map[int64]float64{"a": sn, "b": sn}, map[string]map[int64]float64{"a": te, "b": te}, nil
		},
	}
	for _, s := range []struct {
		tocka string
		dan   int64
		want  float64
	}{
		{"a", pocetak, 0},
		{"a", pocetak + 3, 14},
		{"a", pocetak + 7, 23}, // 14 + 12 − 3, dan 5 i 6 su rupe
		{"a", pocetak + 7, 23},
		{"b", pocetak + 7, 9}, // samo baza oborina: 0, 0, +12, −3
		{"c", pocetak + 7, 0},
	} {
		got, err := z.NaPocetku(s.tocka, s.dan)
		if err != nil || got != s.want {
			t.Errorf("%s na početku dana %d: %v, %v; želim %v", s.tocka, s.dan-pocetak, got, err, s.want)
		}
	}
	if citanja != 2 {
		t.Errorf("baza oborina čitana %d puta, želim 2 (jednom za svaki dan)", citanja)
	}
	// dan daleko naprijed istisne zapamćene, pa se raniji dan računa iznova
	if _, err := z.NaPocetku("a", pocetak+30); err != nil {
		t.Fatal(err)
	}
	if got, err := z.NaPocetku("a", pocetak+3); err != nil || got != 14 || citanja != 4 {
		t.Errorf("nakon istiskivanja: %v, %v, čitanja %d; želim 14 i 4 čitanja", got, err, citanja)
	}

	t.Run("samo arhiva", func(t *testing.T) {
		z := &ZalihaSnijega{Arhiva: arhiva}
		if got, err := z.NaPocetku("a", pocetak+7); err != nil || got != 14 {
			t.Errorf("%v, %v; želim 14", got, err)
		}
	})
	t.Run("samo baza oborina", func(t *testing.T) {
		z := &ZalihaSnijega{Satne: z.Satne, Snijeg: z.Snijeg}
		if got, err := z.NaPocetku("a", pocetak+7); err != nil || got != 9 {
			t.Errorf("%v, %v; želim 9", got, err)
		}
	})
	ne := errors.New("ne radi")
	for ime, pokvaren := range map[string]*ZalihaSnijega{
		"satne": {Satne: func(int64, int64) (map[string]map[int64]float64, error) { return nil, ne }, Snijeg: z.Snijeg},
		"snijeg": {Satne: z.Satne, Snijeg: func(int64, int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			return nil, nil, ne
		}},
		"arhiva": {Arhiva: zatvorenaArhiva(t)},
	} {
		if got, err := pokvaren.NaPocetku("a", pocetak+3); err == nil {
			t.Errorf("%s se ne da pročitati, a bez greške: %v", ime, got)
		}
	}
}

// zatvorenaArhiva je arhiva koja se ne da pročitati.
func zatvorenaArhiva(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := arhivaUDatoteci(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return db
}

// Zaliha snijega međusliva je težinski srednjak točaka s težinom, na početku
// dana u kojem je sat; bez izvora zalihe prazna je.
func TestZalihaSnijegaSlivova(t *testing.T) {
	const sat = int64(20000*24 + 15)
	zalihe := map[string]float64{"a": 10, "b": 30, "c": 1000, "d": 4}
	var dani []int64
	iz := &OborinskiIzvor{
		Tocke: []OborinskaTocka{{Code: "a", Sliv: "S", Tezina: 1}, {Code: "b", Sliv: "S", Tezina: 3},
			{Code: "c", Sliv: "S", Tezina: 0}, {Code: "d", Sliv: "T", Tezina: 2}},
		ZalihaSnijega: func(tocka string, dan int64) (float64, error) {
			dani = append(dani, dan)
			return zalihe[tocka], nil
		},
	}
	got, err := ZalihaSnijegaSlivova(iz, sat)
	if err != nil || !reflect.DeepEqual(got, map[string]float64{"S": 25, "T": 4}) {
		t.Errorf("zaliha %v, %v; želim S 25, T 4", got, err)
	}
	if !reflect.DeepEqual(dani, []int64{20000, 20000, 20000}) {
		t.Errorf("zaliha tražena za dane %v", dani)
	}
	for _, bez := range []*OborinskiIzvor{nil, iz.BezSnijega()} {
		if got, err := ZalihaSnijegaSlivova(bez, sat); err != nil || len(got) != 0 {
			t.Errorf("bez izvora zalihe: %v, %v", got, err)
		}
	}
	ne := errors.New("ne radi")
	iz.ZalihaSnijega = func(string, int64) (float64, error) { return 0, ne }
	if got, err := ZalihaSnijegaSlivova(iz, sat); !errors.Is(err, ne) || got != nil {
		t.Errorf("greška zalihe: %v, %v", got, err)
	}
}

// Izdanje nosi kišu kakva je pala, a model računa s kišom i otopljenim
// snijegom; bez izvora snijega to je ista kiša.
func TestOborineIzdanja(t *testing.T) {
	const dan = int64(20000)
	iz := &OborinskiIzvor{
		Tocke: []OborinskaTocka{{Code: "a", Sliv: "S", Tezina: 1}},
		Satne: func(od, do int64) (map[string]map[int64]float64, error) {
			return map[string]map[int64]float64{"a": satiDana(dan, 1)}, nil
		},
		Snijeg: func(od, do int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			return map[string]map[int64]float64{"a": satiDana(dan, 1)}, map[string]map[int64]float64{"a": satiDana(dan, -2)}, nil
		},
	}
	o := &Osvjezivac{Oborine: iz}
	model, kisa, err := o.oborineIzdanja(dan*24+23, 1, 0)
	if err != nil || !reflect.DeepEqual(model, map[string]DnevniNiz{OborinaKljuc("S"): {0: 0}}) ||
		!reflect.DeepEqual(kisa, map[string]DnevniNiz{OborinaKljuc("S"): {0: 24}}) {
		t.Errorf("model %v, kiša %v, %v", model, kisa, err)
	}
	o.Oborine = iz.BezSnijega()
	if model, kisa, err := o.oborineIzdanja(dan*24+23, 1, 0); err != nil || !reflect.DeepEqual(model, kisa) || model[OborinaKljuc("S")][0] != 24 {
		t.Errorf("bez snijega: model %v, kiša %v, %v", model, kisa, err)
	}
	ne := errors.New("ne radi")
	pokvaren := *iz
	pokvaren.Snijeg = func(int64, int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
		return nil, nil, ne
	}
	o.Oborine = &pokvaren
	if model, kisa, err := o.oborineIzdanja(dan*24+23, 1, 0); err != nil || !reflect.DeepEqual(model, kisa) || model[OborinaKljuc("S")][0] != 24 {
		t.Errorf("snijeg se ne da pročitati: model %v, kiša %v, %v", model, kisa, err)
	}
	// ukupna kiša se ne da pročitati iako je model prošao: Satne pukne drugi put
	poziva := 0
	pokvaren = *iz
	pokvaren.Satne = func(od, do int64) (map[string]map[int64]float64, error) {
		if poziva++; poziva > 1 {
			return nil, ne
		}
		return iz.Satne(od, do)
	}
	o.Oborine = &pokvaren
	if model, kisa, err := o.oborineIzdanja(dan*24+23, 1, 0); !errors.Is(err, ne) || model != nil || kisa != nil {
		t.Errorf("greška ukupne kiše: model %v, kiša %v, %v", model, kisa, err)
	}
}
