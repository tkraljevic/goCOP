package snijeg

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"gocop/internal/prognoza"
)

// Brojevi u testovima su izmišljeni.

func dan(g int, m time.Month, d int) int64 {
	return time.Date(g, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// Povratna razina: k = n/T, između k-tog i (k+1)-og najvećeg maksimuma.
func TestPovratnaRazina(t *testing.T) {
	maks := make([]float64, 30) // 1…30
	for i := range maks {
		maks[i] = float64(i + 1)
	}
	for _, z := range []struct {
		t, want float64
	}{
		{3, 21},   // k = 10: 10. najveći je 21, 11. je 20 → 21
		{4, 23.5}, // k = 7,5: između 24 i 23
		{10, 28},  // k = 3
		{60, 30},  // k < 1: najveći
		{0.5, 1},  // k > n: najmanji
	} {
		if got := PovratnaRazina(maks, z.t); math.Abs(got-z.want) > 1e-9 {
			t.Errorf("T=%v: %v, želim %v", z.t, got, z.want)
		}
	}
	if PovratnaRazina(nil, 3) != 0 || PovratnaRazina(maks, 0) != 0 {
		t.Error("bez maksimuma ili s T=0 nije 0")
	}
}

// Zimski maksimumi: samo pune zime (1. 8.–31. 7.) prije tekuće.
func TestZimskiMaksimumiIPragovi(t *testing.T) {
	niz := map[int64]float64{}
	for g := 1961; g < 1990; g++ {
		for d := dan(g, 8, 1); d < dan(g+1, 8, 1); d++ {
			niz[d] = float64(g - 1960) // zima g/g+1 ima najviše g-1960
		}
	}
	// zima 1990/91 ima samo 100 dana; tekuća zima 1991/92 ne ulazi
	for d := dan(1990, 8, 1); d < dan(1990, 8, 1)+100; d++ {
		niz[d] = 500
	}
	niz[dan(1991, 12, 1)] = 900
	maks := ZimskiMaksimumi(niz, dan(1992, 1, 1))
	if len(maks) != 29 || maks[0] != 1 || maks[28] != 29 {
		t.Fatalf("maksimumi %v", maks)
	}
	p, ima := PragoviIz(maks)
	// k = 29/3 = 9,67: 9. najveći je 21, 10. je 20 → 20,33; k = 5,8 → 24,2; k = 2,9 → 27,1
	if !ima || p != (Pragovi{20, 24, 27}) {
		t.Errorf("pragovi %v %v", p, ima)
	}
	if _, ima := PragoviIz(maks[:19]); ima {
		t.Error("s 19 zima ima pragove")
	}
}

func TestRazina(t *testing.T) {
	p := Pragovi{130, 160, 220}
	for v, want := range map[float64]int{0: 0, 129.9: 0, 130: 1, 170: 2, 220: 3, 400: 3} {
		if got := Razina(v, p); got != want {
			t.Errorf("%v mm: razina %d, želim %d", v, got, want)
		}
	}
	if Razina(50, Pragovi{}) != 0 {
		t.Error("bez pragova razina nije 0")
	}
}

// Histereza: uključi se na pragu, ostaje dok snijeg ne padne ispod pola
// praga; dan bez podatka stanje ne mijenja.
func TestUkljucen(t *testing.T) {
	niz := map[int64]float64{1: 50, 2: 100, 3: 80, 4: 60, 6: 49, 7: 90}
	for do, want := range map[int64]bool{1: false, 2: true, 3: true, 4: true, 5: true, 6: false, 7: false} {
		if got := Ukljucen(niz, 100, 1, do); got != want {
			t.Errorf("do dana %d: %v, želim %v", do, got, want)
		}
	}
	if Ukljucen(niz, 0, 1, 7) {
		t.Error("bez praga uključen")
	}
}

func TestTrend(t *testing.T) {
	niz := map[int64]float64{1: 100, 4: 106, 5: 100, 6: 98, 7: 95}
	for d, want := range map[int64]string{4: "raste", 5: "", 7: "topi se", 6: ""} {
		if got := Trend(niz, d); got != want {
			t.Errorf("dan %d: %q, želim %q", d, got, want)
		}
	}
	if got := Trend(map[int64]float64{1: 100, 4: 103}, 4); got != "miruje" {
		t.Errorf("mala promjena: %q", got)
	}
}

// Sliv je težinski srednjak točaka koje taj dan imaju vrijednost.
func TestNizSliva(t *testing.T) {
	nizovi := map[string]map[int64]float64{
		"a": {1: 100, 2: 100},
		"b": {1: 40},
		"c": {1: 1000}, // bez težine
	}
	got := NizSliva(nizovi, map[string]float64{"a": 0.25, "b": 0.75})
	if want := map[int64]float64{1: 55, 2: 100}; !reflect.DeepEqual(got, want) {
		t.Errorf("sliv %v, želim %v", got, want)
	}
}

// Procjena: snijeg pada u zalihu, topi se 3 mm po °C; dan bez podataka
// snijeg ne mijenja.
func TestProcjena(t *testing.T) {
	dani := map[int64]prognoza.DanOborine{
		11: {Oborina: 10, Snijeg: 10, Temperatura: -3, ImaSnijega: true},
		12: {Oborina: 0, Temperatura: 5, ImaSnijega: true},
	}
	got := Procjena(100, 10, 13, dani)
	if want := map[int64]float64{11: 110, 12: 95, 13: 95}; !reflect.DeepEqual(got, want) {
		t.Errorf("procjena %v, želim %v", got, want)
	}
}

func TestAlpskiSlivovi(t *testing.T) {
	if o := Oznake(); !reflect.DeepEqual(o, []string{"A", "B", "K", "L"}) {
		t.Errorf("oznake %v", o)
	}
	if s, ima := AlpskiSliv("K"); !ima || s.Naziv != "Inn i Salzach" || s.Rijeka != "Dunav" {
		t.Errorf("K: %+v", s)
	}
	if _, ima := AlpskiSliv("O"); ima {
		t.Error("bavarski Dunav je na kartici")
	}
	if Dan(dan(2026, 11, 23)).Format("2. 1. 2006.") != "23. 11. 2026." {
		t.Error("dan")
	}
}

// probniIzvor: dvije točke u A (jedna bez SNOWGRID-a), jedna u L i jedna u
// nizinskom G. Povijest od 1961. daje pragove za A; danas je 24. 11. 2026.
type probniIzvor struct {
	naTlu         map[string]map[int64]float64
	citanja       int
	greskaNaTlu   error
	greskaSatne   error
	greskaSnijega error
	greskaTocaka  error
	greskaModela  error
	bezSatnih     bool
}

var danas = dan(2026, 11, 24)

func noviProbniIzvor() *probniIzvor {
	p := &probniIzvor{naTlu: map[string]map[int64]float64{"k-a-1": {}, "k-l-1": {}}}
	// zime 1961/62–2025/26: najviše 100 + 2·(godina−1961) mm
	for g := 1961; g < 2026; g++ {
		for d := dan(g, 8, 1); d < dan(g+1, 8, 1); d++ {
			p.naTlu["k-a-1"][d] = float64(100 + 2*(g-1961))
		}
	}
	// tekuća sezona: od 1. 11. raste 10 mm na dan, do 22. 11.
	for d := dan(2026, 8, 1); d <= dan(2026, 11, 22); d++ {
		v := 0.0
		if d >= dan(2026, 11, 1) {
			v = float64(10 * (d - dan(2026, 11, 1) + 1))
		}
		p.naTlu["k-a-1"][d] = v
		p.naTlu["k-l-1"][d] = 5
	}
	return p
}

func (p *probniIzvor) izvor() *Izvor {
	return &Izvor{
		Tocke: func() ([]prognoza.OborinskaTocka, error) {
			return []prognoza.OborinskaTocka{{Code: "k-a-1", Sliv: "A", Tezina: 0.6}, {Code: "k-a-2", Sliv: "A", Tezina: 0.4},
				{Code: "k-l-1", Sliv: "L", Tezina: 1}, {Code: "k-g-1", Sliv: "G", Tezina: 1}, {Code: "k-a-0", Sliv: "A"}}, p.greskaTocaka
		},
		NaTlu: func(tocke []string, od, do int64) (map[string]map[int64]float64, error) {
			p.citanja++
			out := map[string]map[int64]float64{}
			for _, t := range tocke {
				out[t] = map[int64]float64{}
				for d, v := range p.naTlu[t] {
					if d >= od && d <= do {
						out[t][d] = v
					}
				}
			}
			return out, p.greskaNaTlu
		},
		Satne: func(od, do int64) (map[string]map[int64]float64, error) {
			out := map[string]map[int64]float64{"k-a-1": {}}
			if p.bezSatnih {
				return map[string]map[int64]float64{}, p.greskaSatne
			}
			for h := od; h <= do; h++ {
				out["k-a-1"][h] = 1 // 24 mm na dan, sve snijeg
			}
			return out, p.greskaSatne
		},
		Snijeg: func(od, do int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			sn, te := map[string]map[int64]float64{"k-a-1": {}}, map[string]map[int64]float64{"k-a-1": {}}
			for h := od; h <= do; h++ {
				sn["k-a-1"][h], te["k-a-1"][h] = 1, -2
			}
			return sn, te, p.greskaSnijega
		},
		Model: func(tocka string, d int64) (map[int64]float64, error) {
			return map[int64]float64{d - 2: 30, d - 1: 40}, p.greskaModela
		},
		Sad: func() time.Time { return time.Unix(danas*86400+12*3600, 0) },
	}
}

// Stanje slivova: A ima pragove iz povijesti, vrijednost zadnjeg dana,
// razinu, trend i procjenu; L nema dovoljno zima pa ni pragove; B i K bez
// podataka. Izračunato se pamti dok se ne zove Zaboravi.
func TestSlivovi(t *testing.T) {
	p := noviProbniIzvor()
	iz := p.izvor()
	slivovi, err := iz.Slivovi()
	if err != nil {
		t.Fatal(err)
	}
	if len(slivovi) != 4 || slivovi[0].Oznaka != "A" || slivovi[3].Oznaka != "L" {
		t.Fatalf("slivovi %+v", slivovi)
	}
	a := slivovi[0]
	// 65 punih zima, maksimumi 100…228: k=21,7 → 186,7; k=13 → 204; k=6,5 → 217
	if !a.ImaPragove || a.Pragovi != (Pragovi{187, 204, 217}) {
		t.Errorf("pragovi A %v", a.Pragovi)
	}
	if !a.Ima || a.Dan != dan(2026, 11, 22) || a.Mm != 220 || a.Razina != 3 || !a.Ukljucen || a.Trend != "raste" {
		t.Errorf("A: %+v", a)
	}
	if len(a.Sezona) == 0 || a.Sezona[0].Dan != dan(2026, 8, 1) || a.Sezona[len(a.Sezona)-1].Mm != 220 {
		t.Errorf("sezona A %v…", a.Sezona[:2])
	}
	// procjena od 22. 11. do 29. 11.: svaki dan +24 mm snijega, bez topljenja
	if len(a.Procjena) != 8 || a.Procjena[0] != (TockaKrivulje{dan(2026, 11, 22), 220}) || a.Procjena[7].Mm != 220+7*24 {
		t.Errorf("procjena A %v", a.Procjena)
	}
	if a.Kasni(time.Unix(danas*86400, 0)) || !a.Kasni(time.Unix((danas+2)*86400, 0)) {
		t.Error("kasni")
	}
	l := slivovi[3]
	if !l.Ima || l.ImaPragove || l.Razina != 0 || l.Ukljucen || l.Mm != 5 || l.Procjena == nil {
		t.Errorf("L: %+v", l)
	}
	if slivovi[1].Ima || slivovi[2].Ima {
		t.Error("B ili K imaju snijeg")
	}

	citanja := p.citanja
	if _, err := iz.Slivovi(); err != nil || p.citanja != citanja {
		t.Errorf("drugi put se čitalo (%d → %d): %v", citanja, p.citanja, err)
	}
	// nakon deset minuta stanje se računa iznova, a pragovi istog dana ne
	iz.Sad = func() time.Time { return time.Unix(danas*86400+13*3600, 0) }
	if _, err := iz.Slivovi(); err != nil || p.citanja != citanja+1 {
		t.Errorf("nakon sat vremena %d čitanja, želim %d", p.citanja, citanja+1)
	}
	iz.Zaboravi()
	if _, err := iz.Slivovi(); err != nil || p.citanja != citanja+3 {
		t.Errorf("nakon Zaboravi %d čitanja, želim %d", p.citanja, citanja+3)
	}
}

// SNOWGRID star više od tjedan dana: procjene nema; bez satnih nizova
// procjena ne ide.
func TestSlivoviBezProcjene(t *testing.T) {
	p := noviProbniIzvor()
	p.bezSatnih = true
	iz := p.izvor()
	sl, err := iz.Slivovi()
	if err != nil || sl[0].Procjena != nil {
		t.Errorf("bez satnih: %v %v", sl[0].Procjena, err)
	}
	p = noviProbniIzvor()
	iz = p.izvor()
	iz.Satne, iz.Snijeg = nil, nil
	if sl, err := iz.Slivovi(); err != nil || sl[0].Procjena != nil {
		t.Errorf("bez izvora satnih: %v", err)
	}
	p = noviProbniIzvor()
	iz = p.izvor()
	iz.Sad = func() time.Time { return time.Unix((danas+30)*86400, 0) }
	if sl, err := iz.Slivovi(); err != nil || sl[0].Procjena != nil || !sl[0].Kasni(iz.sad()) {
		t.Errorf("star SNOWGRID: %+v %v", sl[0].Procjena, err)
	}
	if (&Izvor{}).sad().IsZero() {
		t.Error("pravo vrijeme")
	}
}

// Greške izvora se vraćaju.
func TestSlivoviGreske(t *testing.T) {
	for ime, pokvari := range map[string]func(*probniIzvor){
		"točke":  func(p *probniIzvor) { p.greskaTocaka = errors.New("x") },
		"na tlu": func(p *probniIzvor) { p.greskaNaTlu = errors.New("x") },
		"satne":  func(p *probniIzvor) { p.greskaSatne = errors.New("x") },
		"snijeg": func(p *probniIzvor) { p.greskaSnijega = errors.New("x") },
	} {
		t.Run(ime, func(t *testing.T) {
			p := noviProbniIzvor()
			pokvari(p)
			if _, err := p.izvor().Slivovi(); err == nil {
				t.Error("bez greške")
			}
		})
	}
	// tekuća sezona se ne da pročitati, a pragovi da
	p := noviProbniIzvor()
	iz := p.izvor()
	naTlu := iz.NaTlu
	iz.NaTlu = func(tocke []string, od, do int64) (map[string]map[int64]float64, error) {
		if od != prviDan {
			return nil, errors.New("sezona")
		}
		return naTlu(tocke, od, do)
	}
	if _, err := iz.Slivovi(); err == nil {
		t.Error("sezona bez greške")
	}
}

// Točka: SNOWGRID oko točke, a gdje ga nema, zaliha iz modela.
func TestTocka(t *testing.T) {
	p := noviProbniIzvor()
	iz := p.izvor()
	st, err := iz.Tocka("k-a-1")
	if err != nil || !st.Ima || st.IzModela || st.Mm != 220 || st.Trend != "raste" || len(st.Procjena) != 8 {
		t.Errorf("k-a-1: %+v %v", st, err)
	}
	st, err = iz.Tocka("k-a-2")
	if err != nil || !st.Ima || !st.IzModela || st.Dan != danas-1 || st.Mm != 40 || len(st.Sezona) != 2 {
		t.Errorf("k-a-2: %+v %v", st, err)
	}
	iz.Model = nil
	if st, err := iz.Tocka("k-a-2"); err != nil || st.Ima || st.IzModela {
		t.Errorf("bez modela: %+v %v", st, err)
	}
	iz = p.izvor()
	p.greskaModela = errors.New("model")
	if _, err := iz.Tocka("k-a-2"); err == nil {
		t.Error("model bez greške")
	}
	p.greskaModela, p.greskaSatne = nil, errors.New("satne")
	if _, err := iz.Tocka("k-a-1"); err == nil {
		t.Error("satne bez greške")
	}
	p.greskaNaTlu = errors.New("na tlu")
	if _, err := iz.Tocka("k-a-1"); err == nil {
		t.Error("na tlu bez greške")
	}
}
