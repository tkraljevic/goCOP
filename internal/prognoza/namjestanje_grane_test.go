package prognoza

import (
	"database/sql"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// valUlaza je izmišljen satni niz glavnog ulaza: dva spora vala.
func valUlaza(h int64) float64 {
	return 100 + 40*math.Sin(float64(h)/37) + 15*math.Sin(float64(h)/211)
}

// valPritoka je izmišljen niz pritoka, drugih valova nego glavni ulaz.
func valPritoka(h int64) float64 {
	return 50 + 20*math.Sin(float64(h)/13) + 8*math.Sin(float64(h)/97)
}

// satniNiz je niz od sata od do sata do (bez njega) s vrijednostima v.
func satniNiz(od, do int64, v func(h int64) float64) map[int64]float64 {
	n := make(map[int64]float64, do-od)
	for h := od; h < do; h++ {
		n[h] = v(h)
	}
	return n
}

// Namještanje ne vidi sate od NamjestiDo nadalje ni razdoblja iz
// NamjestiBez ([od, do)); bez granica niz ostaje kakav jest.
func TestDoGranice(t *testing.T) {
	niz := func() map[int64]float64 { return satniNiz(0, 10, func(h int64) float64 { return float64(h) }) }
	if got := doGranice(niz()); len(got) != 10 {
		t.Errorf("bez granica ostalo %d sati, želim 10", len(got))
	}
	postavi(t, &NamjestiDo, int64(8))
	postavi(t, &NamjestiBez, [][2]int64{{2, 4}, {5, 6}})
	want := map[int64]float64{0: 0, 1: 1, 4: 4, 6: 6, 7: 7}
	if got := doGranice(niz()); !reflect.DeepEqual(got, want) {
		t.Errorf("s granicama %v, želim %v", got, want)
	}
	postavi(t, &NamjestiDo, int64(0))
	if got := doGranice(niz()); len(got) != 7 || got[9] != 9 {
		t.Errorf("samo razdoblja bez namještanja: %v", got)
	}
}

// Satni niz iz arhive: arhiva bez spoja i vrijednost koja nije broj su
// greška, a ne prazan niz.
func TestNizIzArhiveGreske(t *testing.T) {
	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prazna.Close()
	if n, err := NizIzArhive(prazna, "batina", "vodostaj"); err == nil {
		t.Errorf("arhiva bez spoja: %v bez greške", n)
	}
	db := arhivaSatnih(t, map[string]map[int64]float64{"batina:vodostaj": {1: 100, 2: 101}})
	if n, err := NizIzArhive(db, "batina", "vodostaj"); err != nil || !reflect.DeepEqual(n, map[int64]float64{1: 100, 2: 101}) {
		t.Errorf("niz %v, %v", n, err)
	}
	if _, err := db.Exec(`INSERT INTO spoj VALUES ('batina', 'vodostaj', 'satni', 3*3600, 'x')`); err != nil {
		t.Fatal(err)
	}
	if n, err := NizIzArhive(db, "batina", "vodostaj"); err == nil {
		t.Errorf("vrijednost koja nije broj: %v bez greške", n)
	}
}

// NamjestiLetvu javlja zašto karika nema račun: nema ulaza, arhive, dovoljno
// sati ulaza ili zajedničkih sati, glavni ulaz stoji, cilj stoji ili nijedan
// pojas nema dovoljno sati.
func TestNamjestiLetvuJavljaZastoNemaRacuna(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	cilj := satniNiz(0, 6000, func(h int64) float64 { return 2*valUlaza(h-5) + 3 })
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{
		"cilj:protok":     cilj,
		"ulaz:protok":     satniNiz(0, 6000, valUlaza),
		"kratki:protok":   satniNiz(0, 4999, valUlaza),
		"pomaknut:protok": satniNiz(3000, 9000, valUlaza),
		"ravan:protok":    satniNiz(0, 6000, func(int64) float64 { return 100 }),
		"stoji:protok":    satniNiz(0, 6000, func(int64) float64 { return 240 }),
	})
	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prazna.Close()

	for _, s := range []struct {
		ime    string
		arhiva *sql.DB
		cilj   string
		ulazi  []string
		pojasa int
		greska string
	}{
		{ime: "bez ulaza", arhiva: arhiva, cilj: "cilj", greska: "cilj: nijedan ulaz"},
		{ime: "arhiva bez spoja", arhiva: prazna, cilj: "cilj", ulazi: []string{"ulaz"}, greska: "no such table"},
		{ime: "kratak ulaz", arhiva: arhiva, cilj: "cilj", ulazi: []string{"kratki"},
			greska: "cilj: kratki ima samo 4999 satnih vrijednosti u protok"},
		{ime: "malo zajedničkih sati", arhiva: arhiva, cilj: "cilj", ulazi: []string{"pomaknut"}, greska: "cilj ← pomaknut: samo "},
		{ime: "glavni ulaz stoji", arhiva: arhiva, cilj: "cilj", ulazi: []string{"ravan"}, greska: "cilj ← ravan: glavni ulaz je ravan"},
		{ime: "cilj stoji", arhiva: arhiva, cilj: "stoji", ulazi: []string{"ulaz"}, greska: "stoji: kašnjenja se nisu dala izmjeriti"},
		{ime: "pojasi premali", arhiva: arhiva, cilj: "cilj", ulazi: []string{"ulaz"}, pojasa: 2500,
			greska: "cilj: nijedan pojas nije dao račun"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			if s.pojasa > 0 {
				postavi(t, &NajmanjeSatiPojasa, s.pojasa)
			}
			var izvori []Izvor
			for _, u := range s.ulazi {
				izvori = append(izvori, Izvor{Letva: u, Velicina: "protok"})
			}
			p, err := NamjestiLetvu(s.arhiva, s.cilj, "protok", izvori)
			if err == nil || !strings.Contains(err.Error(), s.greska) {
				t.Fatalf("greška %v, želim %q (pojasi %+v)", err, s.greska, p)
			}
			if s.ime == "malo zajedničkih sati" && !strings.HasSuffix(err.Error(), "zajedničkih sati") {
				t.Errorf("greška %v", err)
			}
		})
	}
}

// Kad se niz ulaza ne da pročitati, a cilj da, vraća se greška čitanja, a
// ne da ulaz ima premalo sati.
func TestNamjestiLetvuUlazSeNeDaProcitati(t *testing.T) {
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{
		"cilj:protok": satniNiz(0, 6000, valUlaza), "ulaz:protok": satniNiz(0, 6000, valUlaza)})
	zamijeniStupac(t, arhiva, "spoj", "vrijednost", pucaNa("letva = 'ulaz'", "vrijednost"))
	p, err := NamjestiLetvu(arhiva, "cilj", "protok", []Izvor{{Letva: "ulaz", Velicina: "protok"}})
	if err == nil {
		t.Fatalf("bez greške: %+v", p)
	}
	if !strings.Contains(err.Error(), "integer overflow") || strings.Contains(err.Error(), "satnih vrijednosti") {
		t.Errorf("greška %q, želim grešku čitanja ulaza (integer overflow)", err)
	}
}

// Pojas s premalo sati ne dobiva račun, a ostali ga dobivaju; pojas u kojem
// cilj stoji na istoj vodi ne dobiva ga ni s dovoljno sati, jer u njemu
// nema veze koju bi se mjerilo.
func TestNamjestiLetvuPreskaceSlabePojase(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	ulaz := satniNiz(0, 6000, valUlaza)
	cilj := satniNiz(5, 6000, func(h int64) float64 { return 2*valUlaza(h-5) + 3 })
	stoji := satniNiz(5, 6000, func(h int64) float64 { return 2*math.Max(valUlaza(h-5), 104) + 3 })
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{"ulaz:protok": ulaz, "cilj:protok": cilj, "stoji:protok": stoji})
	izvori := []Izvor{{Letva: "ulaz", Velicina: "protok"}}

	postavi(t, &NajmanjeSatiPojasa, 1500)
	p, err := NamjestiLetvu(arhiva, "cilj", "protok", izvori)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 2 || p[0].Do != p[1].Od || p[0].Sati < 1500 || p[1].Sati < 1500 {
		t.Errorf("s 1500 sati po pojasu: %+v — želim samo dva donja pojasa", p)
	}

	postavi(t, &NajmanjeSatiPojasa, 200)
	svi, err := NamjestiLetvu(arhiva, "cilj", "protok", izvori)
	if err != nil {
		t.Fatal(err)
	}
	bez, err := NamjestiLetvu(arhiva, "stoji", "protok", izvori)
	if err != nil {
		t.Fatal(err)
	}
	if len(svi) != 5 || len(bez) != 4 || bez[0].Od != svi[1].Od {
		t.Fatalf("pojasi bez stajanja %d, sa stajanjem u donjem pojasu %d: %+v", len(svi), len(bez), bez)
	}
	for _, q := range bez {
		if q.Ulazi[0].PomakH != 5 {
			t.Errorf("pojas %.0f–%.0f: pomak %d, želim 5", q.Od, q.Do, q.Ulazi[0].PomakH)
		}
	}
}

// Sporedni ulaz ulazi pravocrtno, s jednim nagibom za sve pojase, a kašnjenje
// mu se traži kraće, do NajveciPomakPritoka.
func TestNamjestiLetvuSporedniUlaz(t *testing.T) {
	postavi(t, &NajmanjeSatiPojasa, 200)
	postavi(t, &NajveciPomak, 8)
	postavi(t, &NajveciPomakPritoka, 4)
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{
		"glavni:protok": satniNiz(0, 6000, valUlaza),
		"pritok:protok": satniNiz(0, 6000, valPritoka),
		"cilj:protok":   satniNiz(5, 6000, func(h int64) float64 { return 2*valUlaza(h-5) + 0.5*valPritoka(h-3) + 3 }),
	})
	izvori := []Izvor{{Letva: "glavni", Velicina: "protok"}, {Letva: "pritok", Velicina: "protok"}}
	p, err := NamjestiLetvu(arhiva, "cilj", "protok", izvori)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) == 0 {
		t.Fatal("nijedan pojas")
	}
	for _, q := range p {
		g, s := q.Ulazi[0], q.Ulazi[1]
		if len(q.Ulazi) != 2 || s.Letva != "pritok" || g.PomakH != 5 || s.PomakH != 3 ||
			math.Abs(g.Nagib-2) > 0.01 || math.Abs(s.Nagib-0.5) > 0.01 || q.R < 0.999 {
			t.Errorf("pojas %.0f–%.0f: %+v, r %.4f — želim glavni 5 h ×2, pritok 3 h ×0,5", q.Od, q.Do, q.Ulazi, q.R)
		}
		if s.Nagib != p[0].Ulazi[1].Nagib {
			t.Errorf("nagib pritoka %g u pojasu %.0f, a u prvom %g — mora biti isti", s.Nagib, q.Od, p[0].Ulazi[1].Nagib)
		}
	}

	// Pritoku se kašnjenje ne traži dalje od NajveciPomakPritoka, a glavnom
	// toku da.
	postavi(t, &NajveciPomakPritoka, 2)
	p, err = NamjestiLetvu(arhiva, "cilj", "protok", izvori)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range p {
		if q.Ulazi[0].PomakH != 5 || q.Ulazi[1].PomakH > 2 {
			t.Errorf("pojas %.0f–%.0f: glavni %d h, pritok %d h — pritok smije najviše 2", q.Od, q.Do, q.Ulazi[0].PomakH, q.Ulazi[1].PomakH)
		}
	}
}

// Sporedni ulaz kojem nijedan sat nema punog prozora za svako kašnjenje (ovdje
// fali svaki pedeseti sat) ostaje na početnom kašnjenju i bez glačanja, a
// sati u kojima mu fali vrijednost ne ulaze u račun — glavni ulaz se namjesti
// kao da pritoka nema.
func TestNamjestiLetvuPritokSRupama(t *testing.T) {
	postavi(t, &NajmanjeSatiPojasa, 200)
	postavi(t, &NajveciPomak, 8)
	pritok := satniNiz(0, 6000, valPritoka)
	for h := int64(0); h < 6000; h += 50 {
		delete(pritok, h)
	}
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{
		"glavni:protok": satniNiz(0, 6000, valUlaza),
		"pritok:protok": pritok,
		"cilj:protok":   satniNiz(5, 6000, func(h int64) float64 { return 2*valUlaza(h-5) + 3 }),
	})
	p, err := NamjestiLetvu(arhiva, "cilj", "protok",
		[]Izvor{{Letva: "glavni", Velicina: "protok"}, {Letva: "pritok", Velicina: "protok"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p) == 0 {
		t.Fatal("nijedan pojas")
	}
	for _, q := range p {
		g, s := q.Ulazi[0], q.Ulazi[1]
		if s.PomakH != 0 || s.Sirina != 1 || math.Abs(s.Nagib) > 1e-3 ||
			g.PomakH != 5 || math.Abs(g.Nagib-2) > 1e-3 || q.R < 0.999 {
			t.Errorf("pojas %.0f–%.0f: %+v, r %.4f", q.Od, q.Do, q.Ulazi, q.R)
		}
	}
}

// Kad cilj slijedi prosjek ulaza kroz nekoliko sati, a ne jedan sat,
// namještanje uzme i taj prozor: ovdje pet sati koji završavaju pet sati
// prije cilja, na ulazu s titrajem od pet sati koji rijeka priguši.
func TestNamjestiLetvuNalaziProzor(t *testing.T) {
	postavi(t, &NajmanjeSatiPojasa, 200)
	postavi(t, &NajveciPomak, 8)
	titra := func(h int64) float64 { return valUlaza(h) + 12*math.Sin(2*math.Pi*float64(h)/5) }
	cilj := satniNiz(9, 6000, func(h int64) float64 {
		var s float64
		for k := h - 9; k <= h-5; k++ {
			s += titra(k)
		}
		return 2*s/5 + 3
	})
	arhiva := arhivaSatnih(t, map[string]map[int64]float64{"ulaz:protok": satniNiz(0, 6000, titra), "cilj:protok": cilj})
	p, err := NamjestiLetvu(arhiva, "cilj", "protok", []Izvor{{Letva: "ulaz", Velicina: "protok"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range p {
		if u := q.Ulazi[0]; u.Sirina != 5 || u.PomakH != 5 || q.R < 0.999 {
			t.Errorf("pojas %.0f–%.0f: pomak %d, prozor %d, r %.4f — želim 5, 5 i 1", q.Od, q.Do, u.PomakH, u.Sirina, q.R)
		}
	}
}

// Izvan krajnjih čvorova pravac se nastavlja krajnjim odsječkom.
func TestUdioIzvanCvorova(t *testing.T) {
	c := []float64{0, 10, 20}
	for _, s := range []struct {
		v    float64
		i    int
		udio float64
	}{{-5, 0, -0.5}, {5, 0, 0.5}, {15, 1, 0.5}, {20, 1, 1}, {25, 1, 1.5}} {
		if i, u := udio(c, s.v); i != s.i || u != s.udio {
			t.Errorf("%g: odsječak %d, udio %g; želim %d, %g", s.v, i, u, s.i, s.udio)
		}
	}
}

// Sustav bez ijednog sata daje nule, ne NaN: nepoznanica koju nijedan sat ne
// vidi ne smije srušiti ostale.
func TestRijesiSustavBezSati(t *testing.T) {
	A, b := prazneJednadzbe(3)
	if got := rijesiSRezervom(A, b); !reflect.DeepEqual(got, []float64{0, 0, 0}) {
		t.Errorf("prazan sustav: %v", got)
	}
}

// Percentil praznog niza je nula, a gornji rub je zadnja vrijednost.
func TestPercentil(t *testing.T) {
	if p := percentil(nil, 0.5); p != 0 {
		t.Errorf("prazan niz: %g", p)
	}
	n := []float64{1, 2, 3, 4}
	for _, s := range []struct{ p, want float64 }{{0, 1}, {0.5, 3}, {1, 4}} {
		if got := percentil(n, s.p); got != s.want {
			t.Errorf("percentil %g: %g, želim %g", s.p, got, s.want)
		}
	}
}
