package prognoza

import (
	"math"
	"strings"
	"testing"
)

// pojasIz je pojas letve s jednim ulazom, bez odsječka.
func pojasIz(letva, ulaz string, pomak int, od, do float64) Pojas {
	return Pojas{Letva: letva, Velicina: "vodostaj", Od: od, Do: do, Rasap: 3,
		Ulazi: []Ulaz{{Letva: ulaz, Velicina: "vodostaj", PomakH: pomak, Sirina: 1, Nagib: 1}}}
}

// Račun pamti sat do kojeg vjeruje mjerenju, ne seže dublje od Najranije i
// ne vrti se u krug kad dvije letve računaju jedna drugu u istom satu.
func TestRacunaloGraniceTrazenja(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	const sada = int64(1000)
	a, b := Izvor{Letva: "a", Velicina: "vodostaj"}, Izvor{Letva: "b", Velicina: "vodostaj"}
	pojasi := map[string][]Pojas{"a": {pojasIz("a", "b", 0, -1000, 1000)}, "b": {pojasIz("b", "a", 0, -1000, 1000)}}
	r := NovoRacunalo(pojasi, map[Izvor]Niz{a: ravanNiz(0, sada, 100, 0), b: ravanNiz(0, sada, 100, 0)}, sada)
	if r.Sada() != sada {
		t.Errorf("Sada %d, želim %d", r.Sada(), sada)
	}
	if _, ok := r.U(a, sada-Najranije-1); ok {
		t.Error("vrijednost dublje od Najranije, iako je izmjerena")
	}
	if v, ok := r.U(a, sada-Najranije); !ok || v.Iznos != 100 {
		t.Errorf("na samoj granici Najranije: %+v %v", v, ok)
	}
	izdane, err := r.Prognoziraj("a", 12, "proba")
	if err != nil || len(izdane) != 1 || izdane[0].Ciljni != sada {
		t.Errorf("krug a ↔ b u istom satu: %+v, %v; želim samo sat izdavanja", izdane, err)
	}
	if _, ok := r.U(b, sada+1); ok {
		t.Error("b u satu poslije izdavanja izračunata iz kruga")
	}
}

// Kad se ispravak jedne letve računa iz druge, a ispravak druge iz prve,
// ostatak koji je već u računu ne traži se iznova: krug završi praznim
// ispravkom, a na mirnoj vodi prognoza ostaje na njoj.
func TestOstatakUKruguNeVrtiBeskonacno(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 24)
	const sada = int64(1000)
	a, b := Izvor{Letva: "a", Velicina: "vodostaj"}, Izvor{Letva: "b", Velicina: "vodostaj"}
	pojasi := map[string][]Pojas{"a": {pojasIz("a", "b", 1, -1000, 1000)}, "b": {pojasIz("b", "a", 1, -1000, 1000)}}
	r := NovoRacunalo(pojasi, map[Izvor]Niz{
		a: ravanNiz(sada-30, sada-5, 100, 0),
		b: ravanNiz(sada-100, sada-10, 100, 0),
	}, sada)
	v, ok := r.U(a, sada+1)
	if !ok || math.Abs(v.Iznos-100) > 1e-9 || !v.Prognoza {
		t.Errorf("a u satu poslije izdavanja: %+v %v, želim 100", v, ok)
	}
	if !r.ostaci[a].ima || !r.ostaci[b].ima {
		t.Errorf("ostaci a %+v i b %+v moraju biti izmjereni", r.ostaci[a], r.ostaci[b])
	}
}

// Stalni dio ispravka mjeri se samo kad ga se ima iz čega izmjeriti i kad je
// pogreška u prozoru skladna; inače sav ispravak blijedi s poluvijekom.
func TestStalniDioKadaGaNema(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 24)
	postavi(t, &StalniIspravakSati, 72)
	postavi(t, &StalniSamoSkladan, true)
	const sada = int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	donja := Izvor{Letva: "donja", Velicina: "vodostaj"}
	prognoza := func(t *testing.T, mjereno map[int64]float64) []Izdana {
		t.Helper()
		r := NovoRacunalo(lanac(1, 0, 5, 3), map[Izvor]Niz{gornja: ravanNiz(800, sada, 100, 0), donja: NoviNiz(mjereno)}, sada)
		izdane, err := r.Prognoziraj("donja", 48, "proba")
		if err != nil || len(izdane) != 49 {
			t.Fatalf("doseg %d, %v", len(izdane), err)
		}
		return izdane
	}
	blijedi := func(r0 float64, k int) float64 { return 100 + r0*math.Exp2(-float64(k)/24) }

	// Donja je izmjerena samo u satu izdavanja: u prozoru nema pogreške.
	izdane := prognoza(t, map[int64]float64{sada: 130})
	if x := izdane[24].Vrijednost; math.Abs(x-blijedi(30, 24)) > 1e-9 {
		t.Errorf("bez mjerenja u prozoru na +24 h %g, želim %g", x, blijedi(30, 24))
	}

	// Mjerenja svakih šest sati do dana prije izdavanja, pa rupa: prozor ima
	// pogrešku, ali 24-satnog srednjaka u satu izdavanja nema.
	m := map[int64]float64{sada: 130}
	for tt := sada - 72; tt <= sada-24; tt += 6 {
		m[tt] = 130
	}
	izdane = prognoza(t, m)
	if x := izdane[24].Vrijednost; math.Abs(x-blijedi(30, 24)) > 1e-9 {
		t.Errorf("bez srednjaka u satu izdavanja na +24 h %g, želim %g", x, blijedi(30, 24))
	}

	// Pogreška u prozoru mijenja predznak: nije stalna.
	m = map[int64]float64{}
	for tt := int64(800); tt < sada; tt++ {
		m[tt] = 95
		if tt < sada-36 {
			m[tt] = 115
		}
	}
	m[sada] = 110
	izdane = prognoza(t, m)
	if x := izdane[24].Vrijednost; math.Abs(x-blijedi(10, 24)) > 1e-9 {
		t.Errorf("neskladna pogreška na +24 h %g, želim %g", x, blijedi(10, 24))
	}
}

// U protoku je mirna voda udio najmanjeg srednjaka u prozoru, a ne
// centimetri: porast od 100 m³/s na 1300 je mirna voda i stalna pogreška
// ostaje cijelim dosegom, a porast od 300 je val i sav ispravak blijedi.
func TestStalniDioUProtoku(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 24)
	postavi(t, &StalniIspravakSati, 72)
	postavi(t, &StalniSamoSkladan, true)
	const sada = int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "protok"}
	donja := Izvor{Letva: "donja", Velicina: "protok"}
	pojasi := map[string][]Pojas{"donja": {{Letva: "donja", Velicina: "protok", Od: 0, Do: 10000, Rasap: 30,
		Ulazi: []Ulaz{{Letva: "gornja", Velicina: "protok", PomakH: 5, Sirina: 1, Nagib: 1}}}}}
	// Model donje je gornja, 1000 m³/s. Donja stoji na 1300, a zadnjih 21
	// sat na 1300 + porast: u prozoru je pogreška devet puta 300 i triput
	// 300 + porast, pa je stalni dio 300.
	prognoza := func(porast float64) []Izdana {
		t.Helper()
		m := map[int64]float64{}
		for tt := int64(800); tt <= sada; tt++ {
			m[tt] = 1300
			if tt >= sada-20 {
				m[tt] += porast
			}
		}
		r := NovoRacunalo(pojasi, map[Izvor]Niz{gornja: ravanNiz(800, sada, 1000, 0), donja: NoviNiz(m)}, sada)
		izdane, err := r.Prognoziraj("donja", 96, "proba")
		if err != nil || len(izdane) != 97 {
			t.Fatalf("doseg %d, %v", len(izdane), err)
		}
		return izdane
	}
	// Srednjak zadnjeg dana je 1387,5, 87,5 iznad najmanjeg: više od
	// StalniMirnaVodaCm, a manje od 15 % od 1300.
	izdane := prognoza(100)
	for _, k := range []int{1, 48, 96} {
		want := 1000 + 300 + 100*math.Exp2(-float64(k)/24)
		if x := izdane[k].Vrijednost; math.Abs(x-want) > 1e-9 {
			t.Errorf("porast 100 m³/s na +%d h: %g, želim %g (stalni dio ostaje)", k, x, want)
		}
	}
	// Srednjak zadnjeg dana je 262,5 iznad najmanjeg, više od 15 % od 1300:
	// stalnog dijela nema.
	izdane = prognoza(300)
	for _, k := range []int{1, 48, 96} {
		want := 1000 + 600*math.Exp2(-float64(k)/24)
		if x := izdane[k].Vrijednost; math.Abs(x-want) > 1e-9 {
			t.Errorf("porast 300 m³/s na +%d h: %g, želim %g (sav ispravak blijedi)", k, x, want)
		}
	}
}

// Kašnjenje prvog pojasa samo je polazište: pri vodi drugog pojasa račun
// prijeđe na njegovo kašnjenje i čita ulaz toliko ranije.
func TestIzracunPopravljaKasnjenjePoVodnosti(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	const sada = int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	pojasi := map[string][]Pojas{"donja": {pojasIz("donja", "gornja", 2, 0, 100), pojasIz("donja", "gornja", 6, 100, 300)}}
	niz := ravanNiz(sada-100, sada, 200, 0.5) // 250 cm u satu izdavanja, raste pola cm na sat
	r := NovoRacunalo(pojasi, map[Izvor]Niz{gornja: niz}, sada)
	izdane, err := r.Prognoziraj("donja", 6, "proba")
	if err != nil || len(izdane) != 7 {
		t.Fatalf("doseg %d, %v", len(izdane), err)
	}
	for k, i := range izdane {
		want, _ := niz.U(sada + int64(k) - 6)
		if math.Abs(i.Vrijednost-want) > 1e-9 {
			t.Errorf("+%d h: %g, želim gornju šest sati ranije %g", k, i.Vrijednost, want)
		}
	}
}

// Kašnjenje između sredina pojasa prelijeva se pravocrtno; ispod sredine
// prvog vrijedi njegovo, iznad zadnjeg zadnje, a bez pojasa nula.
func TestKasnjenjePoVodnosti(t *testing.T) {
	pojasi := []Pojas{pojasIz("donja", "gornja", 2, 0, 100), pojasIz("donja", "gornja", 6, 100, 300)}
	for _, s := range []struct{ v, want float64 }{{10, 2}, {50, 2}, {125, 4}, {200, 6}, {900, 6}} {
		if got := Kasnjenje(pojasi, s.v); math.Abs(got-s.want) > 1e-9 {
			t.Errorf("pri %g: %g, želim %g", s.v, got, s.want)
		}
	}
	if got := Kasnjenje(nil, 100); got != 0 {
		t.Errorf("bez pojasa: %g", got)
	}
}

// Sporedni ulazi ulaze svaki sa svojim kašnjenjem i nagibom, a raspon im se
// zbraja kvadratno; bez sporednog ulaza nema ni vrijednosti.
func TestIzracunSaSporednimUlazom(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	const sada = int64(1000)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	pritoka := Izvor{Letva: "pritoka", Velicina: "vodostaj"}
	pojasi := map[string][]Pojas{"donja": {{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Odsjecak: 10, Rasap: 4,
		Ulazi: []Ulaz{
			{Letva: "gornja", Velicina: "vodostaj", PomakH: 1, Sirina: 1, Nagib: 0.5},
			{Letva: "pritoka", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 2},
		}}}}
	r := NovoRacunalo(pojasi, map[Izvor]Niz{gornja: ravanNiz(900, sada, 200, 0), pritoka: ravanNiz(900, sada, 30, 1)}, sada)
	// +1 h: gornja u satu izdavanja (200), pritoka dva sata prije njega
	// (128); oba izmjerena, pa je raspon rasap dionice.
	v, ok := r.U(Izvor{Letva: "donja", Velicina: "vodostaj"}, sada+1)
	if !ok || math.Abs(v.Iznos-(10+0.5*200+2*128)) > 1e-9 || math.Abs(v.Raspon-4) > 1e-9 || !v.Prognoza {
		t.Errorf("+1 h: %+v %v, želim 366 ± 4", v, ok)
	}
	// Pritoka izmjerena do tri sata prije izdavanja, a dva sata prije
	// računa se iz izvora (128 cm) s rasapom 3: raspon donje je
	// √(4² + (2·3)²), a ne 4 + 2·3.
	izvor := Izvor{Letva: "izvor", Velicina: "vodostaj"}
	sPritokom := map[string][]Pojas{"donja": pojasi["donja"], "pritoka": {pojasIz("pritoka", "izvor", 1, -1000, 1000)}}
	r = NovoRacunalo(sPritokom, map[Izvor]Niz{gornja: ravanNiz(900, sada, 200, 0), pritoka: ravanNiz(900, sada-3, 30, 1), izvor: ravanNiz(900, sada, 128, 0)}, sada)
	if p, ok := r.U(pritoka, sada-2); !ok || p.Iznos != 128 || p.Raspon != 3 || !p.Prognoza {
		t.Fatalf("pritoka dva sata prije izdavanja: %+v %v, želim izračunatih 128 ± 3", p, ok)
	}
	v, ok = r.U(Izvor{Letva: "donja", Velicina: "vodostaj"}, sada+1)
	if want := math.Sqrt(4*4 + 6*6); !ok || math.Abs(v.Iznos-366) > 1e-9 || math.Abs(v.Raspon-want) > 1e-9 || !v.Prognoza {
		t.Errorf("+1 h s izračunatom pritokom: %+v %v, želim 366 ± %g", v, ok, want)
	}
	// Pritoka koja nije javila ništa: vrijednosti nema.
	bez := NovoRacunalo(pojasi, map[Izvor]Niz{gornja: ravanNiz(900, sada, 200, 0)}, sada)
	if v, ok := bez.U(Izvor{Letva: "donja", Velicina: "vodostaj"}, sada+1); ok {
		t.Errorf("bez sporednog ulaza: %+v", v)
	}
}

// Letva bez pojasa nema prognozu i to se kaže; letva kojoj ulaz nema ništa
// daje prazan niz bez greške.
func TestPrognozirajBezPojasaIBezUlaza(t *testing.T) {
	r := NovoRacunalo(lanac(1, 0, 5, 3), map[Izvor]Niz{}, 1000)
	if _, err := r.Prognoziraj("nepoznata", 12, "proba"); err == nil || !strings.Contains(err.Error(), "nepoznata: nema namještenih pojasa") {
		t.Errorf("bez pojasa: %v", err)
	}
	izdane, err := r.Prognoziraj("donja", 12, "proba")
	if err != nil || len(izdane) != 0 {
		t.Errorf("bez ulaza: %+v, %v", izdane, err)
	}
}
