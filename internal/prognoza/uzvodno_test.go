package prognoza

import (
	"context"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// uzvodniNiz je satni niz uzvodne letve od 200 sati prije izdanja do sata
// zadnji: u satu prije izdanja vodostaj je za d24 viši nego 24 sata prije i
// za d48 nego 48 sati prije, a između se mijenja pravocrtno.
func uzvodniNiz(sada, zadnji int64, d24, d48 float64) Niz {
	h0 := sada - 1
	v := map[int64]float64{}
	for h := sada - 200; h <= zadnji; h++ {
		switch {
		case h <= h0-48:
			v[h] = 300
		case h <= h0-24:
			v[h] = 300 + (d48-d24)*float64(h-(h0-48))/24
		default:
			v[h] = 300 + (d48 - d24) + d24*float64(h-(h0-24))/24
		}
	}
	return NoviNiz(v)
}

// satniSUzvodnima su satni nizovi Batine (100 cm) i obje uzvodne letve s
// istim promjenama, mjerene do sata izdanja.
func satniSUzvodnima(sada int64, d24, d48 float64) map[string]Niz {
	return map[string]Niz{
		"batina":        nizSRupama(sada-200, sada),
		"wildungsmauer": uzvodniNiz(sada, sada, d24, d48),
		"achleiten":     uzvodniNiz(sada, sada, d24, d48),
	}
}

// Zadane promjene daju ispravak c0 + b·x iz tablice: kad obje uzvodne letve
// porastu 50 cm u 24 h i 80 cm u 48 h, Batina 6. dan ide 63,3 cm gore, a
// samo s Wildungsmauerom 36,3 cm. Dan 0 je mjerenje i ne pomiče se.
func TestUzvodniIspravakIzTablice(t *testing.T) {
	const sada = int64(500000)
	satni := satniSUzvodnima(sada, 50, 80)
	pomak, skup := UzvodniIspravak("batina", satni, sada, 0)
	if skup != UzvodnoWA || math.Abs(pomak[6]-63.339) > 1e-9 || pomak[0] != 0 {
		t.Errorf("W + Achleiten: skup %d, pomak %v; želim skup %d i 6. dan +63,339", skup, pomak, UzvodnoWA)
	}
	for k := 1; k <= DnevniDosezi; k++ {
		red := uzvodniKoefWA["batina"][k]
		e := red[0] + 50*red[1] + 80*red[2] + 50*red[3] + 80*red[4]
		if math.Abs(pomak[k]+e) > 1e-9 {
			t.Errorf("%d. dan: pomak %g, želim −(c0 + b·x) = %g", k, pomak[k], -e)
		}
	}

	delete(satni, "achleiten")
	pomak, skup = UzvodniIspravak("batina", satni, sada, 0)
	if skup != UzvodnoW || math.Abs(pomak[6]-36.2756) > 1e-9 {
		t.Errorf("samo W: skup %d, pomak %v; želim skup %d i 6. dan +36,2756", skup, pomak, UzvodnoW)
	}

	delete(satni, "wildungsmauer")
	if pomak, skup = UzvodniIspravak("batina", satni, sada, 0); skup != UzvodnoBez || pomak != [DnevniDosezi + 1]float64{} {
		t.Errorf("bez Wildungsmauera: skup %d, pomak %v; želim bez ispravka", skup, pomak)
	}
	if pomak, skup = UzvodniIspravak("komarom", satniSUzvodnima(sada, 50, 80), sada, 0); skup != 0 || pomak != [DnevniDosezi + 1]float64{} {
		t.Errorf("Komárom ispravak nema: skup %d, pomak %v", skup, pomak)
	}
}

// Mjerenje uzvodne letve smije kasniti do tri sata od sata prije izdanja;
// četiri sata staro znači da letva nema mjerenja.
func TestUzvodnoStaroMjerenje(t *testing.T) {
	const sada = int64(500000)
	for _, s := range []struct {
		zadnji int64
		skup   int
	}{
		{sada - 1, UzvodnoWA},
		{sada - 3, UzvodnoWA},
		{sada - 4, UzvodnoBez},
	} {
		satni := satniSUzvodnima(sada, 20, 30)
		satni["wildungsmauer"] = uzvodniNiz(sada, s.zadnji, 20, 30)
		if _, skup := UzvodniIspravak("batina", satni, sada, 0); skup != s.skup {
			t.Errorf("Wildungsmauer zadnji %d h prije izdanja: skup %d, želim %d", sada-s.zadnji, skup, s.skup)
		}
	}
	satni := satniSUzvodnima(sada, 20, 30)
	satni["achleiten"] = uzvodniNiz(sada, sada-4, 20, 30)
	if _, skup := UzvodniIspravak("batina", satni, sada, 0); skup != UzvodnoW {
		t.Errorf("Achleiten zadnji 4 h prije izdanja: skup %d, želim samo W", skup)
	}
}

// redom je niz satnih vrijednosti koji završava u satu sat; NaN je sat bez
// očitanja.
func redom(sat int64, v ...float64) map[int64]float64 {
	out := map[int64]float64{}
	for i, x := range v {
		if !math.IsNaN(x) {
			out[sat-int64(len(v)-1-i)] = x
		}
	}
	return out
}

// Vrijednost se traži u zadanom satu pa do dva sata ranije, bez
// premošćivanja; lažni skok veći od 50 cm ne ulazi ni u svojem satu ni u
// satu kad se letva vratila, kao na Achleitenu 2013., 2017. i 2025.
func TestUzvodnaVrijednost(t *testing.T) {
	const sat = int64(1000)
	for _, s := range []struct {
		ime string
		niz map[int64]float64
		v   float64
		ima bool
	}{
		{"u satu", map[int64]float64{sat - 1: 200, sat: 210}, 210, true},
		{"sat ranije", map[int64]float64{sat - 2: 190, sat - 1: 200}, 200, true},
		{"dva sata ranije", map[int64]float64{sat - 2: 190}, 190, true},
		{"tri sata ranije je prestaro", map[int64]float64{sat - 3: 180}, 0, false},
		{"premošćivanja nema", map[int64]float64{sat - 5: 100, sat + 5: 200}, 0, false},
		{"lažni skok", map[int64]float64{sat - 1: 200, sat: 260}, 200, true},
		{"lažni pad", map[int64]float64{sat - 1: 200, sat: 140}, 200, true},
		{"skok od 50 cm je pravi", map[int64]float64{sat - 1: 200, sat: 250}, 250, true},
		{"bez sata prije nema usporedbe", map[int64]float64{sat - 2: 100, sat: 260}, 260, true},
		{"lažan je bio sat prije", redom(sat, 300, 370, 290), 290, true},
		{"lažan je bio sat prije, a dva sata prije nema očitanja", redom(sat, 300, math.NaN(), 370, 305), 305, true},
		{"vratio se nakon dva lažna sata", redom(sat, 300, 120, 125, 302), 302, true},
		{"sat poslije povratka nakon dva lažna sata", redom(sat, 300, 120, 125, 302, 304), 304, true},
		{"sat poslije povratka nakon dva jednaka lažna sata", redom(sat, 300, 302, 230, 230, 310, 310), 310, true},
		{"drugi sat lažnog pada", redom(sat, 301, 300, 300, 120, 125), 300, true},
		{"drugi sat lažnog pada na istu vrijednost", redom(sat, 300, 300, 302, 230, 230), 302, true},
		{"prvi sat pravog skoka kasni i kad je blizu razine tri sata prije", redom(sat, 316, 300, 314, 365), 314, true},
		{"drugi sat pravog skoka kasni", redom(sat, 300, 302, 302, 355, 384), 302, true},
		{"treći sat pravog skoka", redom(sat, 300, 302, 355, 384, 372), 372, true},
		{"lažni skok dva sata prije ne ulazi", redom(sat, 300, 480, 302, 305), 305, true},
		{"rast do 50 cm u satu je pravi", redom(sat, 240, 270, 300, 330, 360), 360, true},
		{"razina dva sata prije bez dva sata prije sebe", redom(sat, 300, 300, 120, 125), 125, true},
		{"razina dva sata prije bez sata prije sebe", redom(sat, 300, 120, 125), 125, true},
		{"lažni sat kad mjerenje kasni", redom(sat-1, 300, 370), 300, true},
	} {
		v, ima := uzvodnaVrijednost(NoviNiz(s.niz), sat)
		if v != s.v || ima != s.ima {
			t.Errorf("%s: %g %v, želim %g %v", s.ime, v, ima, s.v, s.ima)
		}
	}
}

// Skok od 60 cm u zadnjem satu Wildungsmauera ne ulazi u ispravak: uzima se
// sat prije, kao da je mjerenje kasnilo sat.
func TestUzvodniSkokUZadnjemSatu(t *testing.T) {
	const sada = int64(500000)
	satni := satniSUzvodnima(sada, 20, 30)
	pravi, _ := UzvodniIspravak("batina", satni, sada, 0)
	v := map[int64]float64{}
	n := satni["wildungsmauer"]
	for i, h := range n.Sati {
		if h <= sada-1 {
			v[h] = n.Iznosi[i]
		}
	}
	v[sada-1] = v[sada-2] + 60
	satni["wildungsmauer"] = NoviNiz(v)
	satni["achleiten"] = uzvodniNiz(sada, sada-1, 20, 30)
	sa, skup := UzvodniIspravak("batina", satni, sada, 0)
	// sat prije je 20/24 cm niže, pa je promjena za toliko manja
	x := []float64{20 - 20.0/24, 30 - 20.0/24, 20, 30}
	for k := 1; k <= DnevniDosezi; k++ {
		if want := uzvodniPomakDana(uzvodniKoefWA["batina"][k], x, 0); skup != UzvodnoWA || math.Abs(sa[k]-want) > 1e-9 {
			t.Errorf("%d. dan: pomak %g (skup %d), želim %g; bez skoka %g", k, sa[k], skup, want, pravi[k])
		}
	}
}

// ravniNiz je Achleiten na 400 cm od 200 sati prije izdanja do sata prije
// njega, s lažnim vrijednostima u zadanim satima.
func ravniNiz(sada int64, lazni map[int64]float64) Niz {
	v := map[int64]float64{}
	for h := sada - 200; h < sada; h++ {
		v[h] = 400
		if x, ima := lazni[h]; ima {
			v[h] = x
		}
	}
	return NoviNiz(v)
}

// Lažni skok Achleitena ne ulazi u ispravak ni u izdanju sat poslije njega,
// ni kad je skok sat ili dva prije sata koji se traži, ni 24 ni 48 sati
// poslije: pomak je isti kao bez skoka. Tako su 2013., 2017. i 2025.
// izgledali osamljeni skok od 184 cm i pad od dva sata.
func TestUzvodniLazniSkokNeUlazi(t *testing.T) {
	const k = int64(500000)
	for _, s := range []struct {
		ime   string
		lazni map[int64]float64
	}{
		{"osamljeni skok", map[int64]float64{k: 584}},
		{"osamljeni pad", map[int64]float64{k: 216}},
		{"pad od dva sata", map[int64]float64{k: 216, k + 1: 219}},
	} {
		for _, sada := range []int64{k + 1, k + 2, k + 3, k + 4, k + 25, k + 26, k + 27, k + 28, k + 49, k + 50, k + 51, k + 52} {
			w := uzvodniNiz(sada, sada, 20, 30)
			bez, _ := UzvodniIspravak("batina", map[string]Niz{"wildungsmauer": w, "achleiten": ravniNiz(sada, nil)}, sada, 0)
			sa, skup := UzvodniIspravak("batina", map[string]Niz{"wildungsmauer": w, "achleiten": ravniNiz(sada, s.lazni)}, sada, 0)
			if sa != bez || skup != UzvodnoWA {
				t.Errorf("%s, izdanje %d h poslije skoka: pomak %v (skup %d), želim kao bez skoka %v", s.ime, sada-k, sa, skup, bez)
			}
		}
	}
}

// Zaštita i granice: ispravak ne spušta prognozu dok cilj raste više od 5 cm
// u 24 h, diže najviše 100 cm, a ne prelazi granice iz učenja.
func TestUzvodnaZastita(t *testing.T) {
	x := []float64{10, 20}
	for _, s := range []struct {
		ime  string
		red  []float64
		rast float64
		want float64
	}{
		{"spušta kad cilj miruje", []float64{10, 0, 0, -500, 500}, 0, -10},
		{"spušta kad cilj raste 5 cm", []float64{10, 0, 0, -500, 500}, 5, -10},
		{"ne spušta kad cilj raste 6 cm", []float64{10, 0, 0, -500, 500}, 6, 0},
		{"spušta kad cilj pada", []float64{10, 0, 0, -500, 500}, -30, -10},
		{"diže i kad cilj raste", []float64{-20, 0, 0, -500, 500}, 6, 20},
		{"diže najviše 100 cm", []float64{-150, 0, 0, -500, 500}, 0, 100},
		{"granica učenja prema dolje", []float64{30, 0, 0, -500, 20}, 0, -20},
		{"granica učenja prema gore", []float64{-60, 0, 0, -45, 500}, 0, 45},
		{"c0 + b·x", []float64{1, 2, 3, -500, 500}, 0, -81},
	} {
		if got := uzvodniPomakDana(s.red, x, s.rast); got != s.want {
			t.Errorf("%s: %g, želim %g", s.ime, got, s.want)
		}
	}
	// na pravoj tablici: velik porast gore Batinu 6. dan diže 157 cm, a granica 100
	pomak, _ := UzvodniIspravak("batina", map[string]Niz{
		"wildungsmauer": uzvodniNiz(500000, 500000, 200, 300),
		"achleiten":     uzvodniNiz(500000, 500000, 100, 150),
	}, 500000, 0)
	if pomak[6] != UzvodnoNajvise {
		t.Errorf("velik porast: Batina 6. dan %g, želim %g", pomak[6], UzvodnoNajvise)
	}
}

// Dnevna prognoza cilja s ispravkom: vrijednost, donja i gornja granica
// pomiču se za isti iznos, a oznaka modela dobiva „-uzv” samo kad je
// ispravak računao. Bez uzvodnih letvi prognoza je ista kao bez ispravka.
func TestPrognozirajDnevnoSUzvodnim(t *testing.T) {
	const sada = int64(500000)
	m := dnevniSKoeficijentima(DnevniCilj{Letva: "batina"}, func(k int) float64 { return float64(k) })
	satni := satniSUzvodnima(sada, 50, 80)
	pomak, _ := UzvodniIspravak("batina", satni, sada, 0)
	d, err := PrognozirajDnevno(m, satni, nil, sada)
	if err != nil || len(d) != DnevniDosezi+1 {
		t.Fatalf("%v, %d dana", err, len(d))
	}
	for k, x := range d {
		v := 100 + float64(k) + pomak[k]
		if x.Model != ModelDnevniUzv || x.Vrijednost != v || x.uzvodno != pomak[k] ||
			x.Dolje != v-float64(min(k, 1)) || x.Gore != v+float64(min(k, 1)) {
			t.Errorf("%d. dan: %+v, želim %g ± 1 uz %s", k, x, v, ModelDnevniUzv)
		}
	}
	if d[6].Vrijednost < 160 {
		t.Errorf("6. dan %g: ispravak nije digao prognozu", d[6].Vrijednost)
	}

	bez, err := PrognozirajDnevno(m, map[string]Niz{"batina": satni["batina"]}, nil, sada)
	if err != nil {
		t.Fatal(err)
	}
	for k, x := range bez {
		if x.Model != ModelDnevni || x.Vrijednost != 100+float64(k) || x.uzvodno != 0 {
			t.Errorf("bez uzvodnih, %d. dan: %+v", k, x)
		}
	}
	// bez Wildungsmauera inačica s kišom računa kao i prije, bez greške
	sKisom := dnevniSKoeficijentima(DnevniCilj{Letva: "batina", Slivovi: []string{"S"}}, func(k int) float64 { return float64(k) })
	kisa := DnevniNiz{}
	for dan := int64(-20); dan <= 20; dan++ {
		kisa[dan] = 0
	}
	satni = satniSUzvodnima(sada, 50, 80)
	delete(satni, "wildungsmauer")
	d, err = PrognozirajDnevno(sKisom, satni, map[string]DnevniNiz{OborinaKljuc("S"): kisa}, sada)
	if err != nil || d[6].Model != ModelDnevniKisa || d[6].Vrijednost != 106 {
		t.Errorf("s kišom bez Wildungsmauera: %v, %+v", err, d)
	}
}

// Kad satni nizovi nemaju uzvodne letve, dnevna prognoza je bit po bit ista
// kao račun bez ispravka: naučeni model s regresijom i analogijama.
func TestDnevnaBezUzvodnihBitPoBit(t *testing.T) {
	val := func(d int64) float64 { return 300 + 150*math.Sin(float64(d)/9) + 60*math.Sin(float64(d)/3.7) }
	ulaz, cilj := DnevniNiz{}, DnevniNiz{}
	for d := int64(0); d < 6000; d++ {
		ulaz[d], cilj[d] = val(d), val(d-2)+20
	}
	c := DnevniCilj{Letva: "batina", Ulazi: []string{"ulaz"}}
	m, err := NamjestiDnevni(map[string]DnevniNiz{"batina": cilj, "ulaz": ulaz}, c, 0)
	if err != nil {
		t.Fatal(err)
	}
	const sada = int64(500000)
	satni := map[string]Niz{}
	for _, l := range c.letve() {
		v := map[int64]float64{}
		for h := sada - 200; h <= sada; h++ {
			v[h] = val(6100-(sada-h)/24) + float64(len(l))
		}
		satni[l] = NoviNiz(v)
	}
	dnevni := map[string]DnevniNiz{}
	for _, l := range c.letve() {
		dnevni[l] = DnevniIzSatnog(satni[l], sada, 4)
	}
	x, ok := DnevneZnacajke(c, dnevni, 0)
	if !ok {
		t.Fatal("nema značajki")
	}
	promjena, raspon := m.Prognoziraj(x)
	d, err := PrognozirajDnevno(m, satni, nil, sada)
	if err != nil {
		t.Fatal(err)
	}
	for k := 1; k <= DnevniDosezi; k++ {
		v := x[1] + promjena[k]
		got := d[k]
		if math.Float64bits(got.Vrijednost) != math.Float64bits(v) || math.Float64bits(got.Dolje) != math.Float64bits(v-raspon[k]) ||
			math.Float64bits(got.Gore) != math.Float64bits(v+raspon[k]) || got.Model != ModelDnevni {
			t.Errorf("%d. dan: %+v, želim %v ± %v", k, got, v, raspon[k])
		}
	}
}

// Inačica bez kiše dobiva isti ispravak: dani koje cilj uzima iz nje
// (SastaviBezKise) nose njezinu vrijednost s pomakom i njezinu oznaku.
func TestSastaviBezKiseSUzvodnim(t *testing.T) {
	postavi(t, &DnevniBezKiseDoDana, map[string]int{"batina": 2})
	const sada = int64(500000)
	satni := satniSUzvodnima(sada, 50, 80)
	pomak, _ := UzvodniIspravak("batina", satni, sada, 0)
	sKisom := dnevniSKoeficijentima(DnevniCilj{Letva: "batina", Slivovi: []string{"S"}}, func(k int) float64 { return 2 * float64(k) })
	bez := dnevniSKoeficijentima(DnevniCilj{Letva: "batina"}, func(k int) float64 { return -float64(k) })
	oborine := map[string]DnevniNiz{OborinaKljuc("S"): {}}
	for dan := int64(-20); dan <= 20; dan++ {
		oborine[OborinaKljuc("S")][dan] = 0
	}
	d, err := PrognozirajDnevno(sKisom, satni, oborine, sada)
	if err != nil {
		t.Fatal(err)
	}
	got := SastaviBezKise(d, sKisom, []*DnevniModel{sKisom, bez}, satni, oborine, sada)
	for k := 1; k <= DnevniDosezi; k++ {
		v, model := 100+2*float64(k)+pomak[k], ModelDnevniKisaUzv
		if k <= 2 {
			v, model = 100-float64(k)+pomak[k], ModelDnevniUzv
		}
		if got[k].Vrijednost != v || got[k].Model != model {
			t.Errorf("%d. dan: %g %s, želim %g %s", k, got[k].Vrijednost, got[k].Model, v, model)
		}
	}
}

// Oznaka modela: s kišom ili bez, i „-uzv” samo uz skup s mjerenjima.
func TestOznakaDnevnog(t *testing.T) {
	for _, s := range []struct {
		kisa bool
		skup int
		want string
	}{
		{true, UzvodnoWA, ModelDnevniKisaUzv},
		{true, UzvodnoW, ModelDnevniKisaUzv},
		{true, UzvodnoBez, ModelDnevniKisa},
		{true, 0, ModelDnevniKisa},
		{false, UzvodnoW, ModelDnevniUzv},
		{false, UzvodnoBez, ModelDnevni},
	} {
		if got := oznakaDnevnog(s.kisa, s.skup); got != s.want {
			t.Errorf("kiša %v, skup %d: %s, želim %s", s.kisa, s.skup, got, s.want)
		}
	}
	if ModelDnevniUzv != ModelDnevni+"-uzv" || ModelDnevniKisaUzv != ModelDnevniKisa+"-uzv" {
		t.Errorf("oznake %s, %s", ModelDnevniUzv, ModelDnevniKisaUzv)
	}
}

// Stanje ispravka u izdanju: skup, uzvodna letva bez mjerenja i koje joj
// mjerenje fali, sa zadnjim očitanjem kad fali sat prije izdanja, i ciljevi
// koje je ispravak digao više od 50 cm, zaokruženo na centimetar, svaki s
// danom najvećeg podizanja.
func TestUzvodnoIzdanja(t *testing.T) {
	const sada = int64(500000)
	dnevne := []DnevnaIzdana{
		{Letva: "batina", Dan: 4, uzvodno: 51}, {Letva: "batina", Dan: 5, uzvodno: 55}, {Letva: "batina", Dan: 6, uzvodno: 62},
		{Letva: "aljmas", Dan: 6, uzvodno: 50},
		{Letva: "vukovar", Dan: 4, uzvodno: 70}, {Letva: "vukovar", Dan: 5, uzvodno: 60},
		{Letva: "ilok", Dan: 5, uzvodno: 50.5}, {Letva: "ilok", Dan: 6, uzvodno: 50.4},
		{Letva: "osijek", Dan: 6, uzvodno: 90},
	}
	satni := satniSUzvodnima(sada, 0, 0)
	u := uzvodnoIzdanja(satni, sada, dnevne)
	want := UzvodnoIzdanja{Skup: UzvodnoWA, Podignuto: []UzvodnoPodizanje{{"batina", 6, 62}, {"vukovar", 4, 70}, {"ilok", 5, 50.5}}}
	if !reflect.DeepEqual(u, want) {
		t.Errorf("stanje %+v, želim %+v", u, want)
	}

	jedan := []DnevnaIzdana{{Letva: "batina", Dan: 1}}
	satni["achleiten"] = uzvodniNiz(sada, sada-10, 0, 0)
	if u := uzvodnoIzdanja(satni, sada, jedan); u.Skup != UzvodnoW || u.Bez != (RupaUlaza{Letva: "achleiten", Zadnje: sada - 10}) ||
		u.Fali != sada-1 || u.Prije != 0 || u.Podignuto != nil {
		t.Errorf("Achleiten zadnji prije 10 h: %+v", u)
	}
	// zadnje mjerenje svježe, a fali ono od prije 24 sata: bez zadnjeg očitanja
	v := map[int64]float64{}
	for h := sada - 20; h <= sada; h++ {
		v[h] = 300
	}
	satni["achleiten"] = NoviNiz(v)
	if u := uzvodnoIzdanja(satni, sada, jedan); u.Bez != (RupaUlaza{Letva: "achleiten"}) || u.Fali != sada-25 || u.Prije != 24 {
		t.Errorf("Achleiten bez mjerenja od prije 24 h: %+v", u)
	}
	for h := sada - 46; h <= sada-21; h++ {
		v[h] = 300
	}
	satni["achleiten"] = NoviNiz(v)
	if u := uzvodnoIzdanja(satni, sada, jedan); u.Bez != (RupaUlaza{Letva: "achleiten"}) || u.Fali != sada-49 || u.Prije != 48 {
		t.Errorf("Achleiten bez mjerenja od prije 48 h: %+v", u)
	}
	delete(satni, "wildungsmauer")
	if u := uzvodnoIzdanja(satni, sada, jedan); u.Skup != UzvodnoBez || u.Bez != (RupaUlaza{Letva: "wildungsmauer"}) || u.Fali != sada-1 {
		t.Errorf("bez Wildungsmauera: %+v", u)
	}

	if u := uzvodnoIzdanja(satni, sada, dnevne[len(dnevne)-1:]); !reflect.DeepEqual(u, UzvodnoIzdanja{}) {
		t.Errorf("bez cilja s ispravkom: %+v", u)
	}
	izbor := map[string]Izbor{}
	UzvodnoIzdanja{}.upisi(izbor)
	if len(izbor) != 0 {
		t.Errorf("bez cilja s ispravkom izbor %v", izbor)
	}
}

// Izbor i napomene uz izdanje: skup ide u izbor, a napomena kaže koje
// mjerenje uzvodnoj letvi fali (zadnje, ili ono od prije 24 ili 48 sati dok
// letva javlja) i koje je ciljeve ispravak digao više od 50 cm, po mjesnom
// vremenu. Uz punu tablicu napomene nema; izbor sam ne daje upozorenje.
func TestUzvodnoIzborINapomene(t *testing.T) {
	sat := func(dan, h int) int64 { return time.Date(2026, 10, dan, h, 0, 0, 0, time.UTC).Unix() / 3600 }
	const samoW = "dnevna prognoza Dunava od 4. dana ispravlja se samo prema letvi wildungsmauer"
	for _, s := range []struct {
		u     UzvodnoIzdanja
		izbor Izbor
		opisi []string
	}{
		{UzvodnoIzdanja{Skup: UzvodnoWA}, Izbor{UzvodnoWA, "dnevna prognoza Dunava od 4. dana ispravlja se prema letvama wildungsmauer i achleiten"}, nil},
		{UzvodnoIzdanja{Skup: UzvodnoW, Bez: RupaUlaza{Letva: "achleiten", Zadnje: sat(21, 15)}, Fali: sat(22, 15)},
			Izbor{UzvodnoW, samoW},
			[]string{fmt.Sprintf("achleiten %d nema mjerenja od 21. 10. u 17 h; %s", sat(21, 15), samoW)}},
		{UzvodnoIzdanja{Skup: UzvodnoW, Bez: RupaUlaza{Letva: "achleiten"}, Fali: sat(22, 15), Prije: 24},
			Izbor{UzvodnoW, samoW},
			[]string{"achleiten 0 fali mjerenje 22. 10. u 17 h (prije 24 sata); " + samoW}},
		{UzvodnoIzdanja{Skup: UzvodnoW, Bez: RupaUlaza{Letva: "achleiten"}, Fali: sat(21, 15), Prije: 48},
			Izbor{UzvodnoW, samoW},
			[]string{"achleiten 0 fali mjerenje 21. 10. u 17 h (prije 48 sati); " + samoW}},
		{UzvodnoIzdanja{Skup: UzvodnoBez, Bez: RupaUlaza{Letva: "wildungsmauer"}},
			Izbor{UzvodnoBez, "dnevna prognoza Dunava od 4. dana računa se bez ispravka prema uzvodnim letvama"},
			[]string{"wildungsmauer 0 nema mjerenja; dnevna prognoza Dunava od 4. dana računa se bez ispravka prema uzvodnim letvama"}},
		{UzvodnoIzdanja{Skup: UzvodnoWA, Podignuto: []UzvodnoPodizanje{{"batina", 6, 62.4}, {"aljmas", 5, 55}, {"ilok", 5, 50.5}}},
			Izbor{UzvodnoWA, "dnevna prognoza Dunava od 4. dana ispravlja se prema letvama wildungsmauer i achleiten"},
			[]string{" 0 ispravak prema uzvodnim letvama digao je dnevnu prognozu za više od 50 cm: batina 6. dan za 62 cm, " +
				"aljmas 5. dan za 55 cm, ilok 5. dan za 51 cm. U velikoj vodi ispravak zna dići i previsoko"}},
	} {
		izbor := map[string]Izbor{}
		s.u.upisi(izbor)
		if !reflect.DeepEqual(izbor, map[string]Izbor{KljucUzvodno: s.izbor}) {
			t.Errorf("skup %d: izbor %+v, želim %+v", s.u.Skup, izbor, s.izbor)
		}
		var opisi []string
		for _, x := range Upozori(&Ishod{Uzvodno: s.u, Izbor: izbor}, nil, nil, nil) {
			if x.Vrsta == VrstaDnevni {
				continue // izmišljeno izdanje nema dnevnih ciljeva
			}
			if x.Vrsta != VrstaUzvodno || x.Tezina != TezinaNapomena || x.Upitno != nil {
				t.Errorf("skup %d: %+v", s.u.Skup, x)
			}
			opisi = append(opisi, fmt.Sprintf("%s %d %s", x.Letva, x.Zadnje, x.Opis))
		}
		if !reflect.DeepEqual(opisi, s.opisi) {
			t.Errorf("skup %d: napomene %q, želim %q", s.u.Skup, opisi, s.opisi)
		}
	}
	if v, l := vrstaIzbora(KljucUzvodno, Izbor{Inacica: UzvodnoBez}); v != "" || l != "" {
		t.Errorf("izbor uzvodnog ispravka: vrsta %q, letva %q", v, l)
	}
	if got := premaLetvama([]string{"a", "b", "c"}); got != "letvama a, b i c" {
		t.Errorf("tri letve: %q", got)
	}
}

// Izdanje uzima uzvodne letve iz očitanja samo za ciljeve s ispravkom:
// Batina računa s njima i nosi „-uzv”, izbor bilježi skup; Wildungsmauer
// koji kasni četiri sata znači ispravak bez njega, sa zadnjim očitanjem.
func TestDnevnoSUzvodnimLetvama(t *testing.T) {
	zadnji := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	sada := zadnji.Unix() / 3600
	batina := DnevniCilj{Letva: "batina", Ulazi: []string{"ulaz"}}
	o := dnevneProbe(t, zadnji, []DnevniCilj{batina}, map[string][]*DnevniModel{"batina": {{Cilj: batina}}},
		map[string]int{"batina": 150, "ulaz": 100, "achleiten": 400})
	dodajSatnaOcitanja(t, o.Ocitanja, "wildungsmauer", zadnji.Add(-120*time.Hour), zadnji.Add(-4*time.Hour), func(int) int { return 300 })

	dnevne, _, _, izbor, _, uzv := o.dnevno(context.Background(), sada, zadnji.Add(-Unatrag))
	if len(dnevne) != DnevniDosezi+1 || dnevne[6].Model != ModelDnevni || izbor[KljucUzvodno].Inacica != UzvodnoBez ||
		uzv.Bez != (RupaUlaza{Letva: "wildungsmauer", Zadnje: sada - 4}) {
		t.Errorf("Wildungsmauer kasni 4 h: %d dana, %+v, izbor %+v, stanje %+v", len(dnevne), dnevne[len(dnevne)-1], izbor, uzv)
	}

	dodajSatnaOcitanja(t, o.Ocitanja, "wildungsmauer", zadnji.Add(-3*time.Hour), zadnji, func(int) int { return 300 })
	dnevne, _, _, izbor, _, uzv = o.dnevno(context.Background(), sada, zadnji.Add(-Unatrag))
	// uzvodne letve miruju, pa je pomak samo −c0 (Batina 6. dan +0,404 cm)
	if dnevne[6].Model != ModelDnevniUzv || math.Abs(dnevne[6].Vrijednost-150.404) > 1e-9 ||
		izbor[KljucUzvodno].Inacica != UzvodnoWA || uzv.Skup != UzvodnoWA || uzv.Bez.Letva != "" {
		t.Errorf("s uzvodnima: %+v, izbor %+v, stanje %+v", dnevne[6], izbor, uzv)
	}

	satni := o.satniCilja(context.Background(), batina, zadnji.Add(-Unatrag))
	if _, ima := satni["achleiten"]; !ima || len(satni) != 4 {
		t.Errorf("nizovi Batine: %v", slices.Sorted(maps.Keys(satni)))
	}
	satni = o.satniCilja(context.Background(), DnevniCilj{Letva: "komarom", Ulazi: []string{"ulaz"}}, zadnji.Add(-Unatrag))
	if _, ima := satni["wildungsmauer"]; ima {
		t.Errorf("Komárom ispravak nema, a nizovi su %v", slices.Sorted(maps.Keys(satni)))
	}
	// niz koji već stoji ne čita se iznova
	satni = map[string]Niz{"wildungsmauer": NoviNiz(map[int64]float64{sada: 1})}
	o.ucitajUzvodne(context.Background(), satni, "batina", zadnji.Add(-Unatrag))
	if len(satni["wildungsmauer"].Sati) != 1 || len(satni["achleiten"].Sati) == 0 {
		t.Errorf("zatečeni Wildungsmauer %d sati, Achleiten %d", len(satni["wildungsmauer"].Sati), len(satni["achleiten"].Sati))
	}
}

// Tablice ispravka: obje imaju iste ciljeve, svaki redak c0, po dva
// koeficijenta na letvu i granice od manje prema većoj. Ciljevi su dnevni
// ciljevi izvan satnog lanca, a uzvodne letve nisu ulazi dnevnog modela.
func TestUzvodneTablice(t *testing.T) {
	ciljevi := uzvodniSkupovi[0].koef
	for i, s := range uzvodniSkupovi {
		if len(s.koef) != len(ciljevi) {
			t.Errorf("skup %d: %d ciljeva, prvi %d", i+1, len(s.koef), len(ciljevi))
		}
		for letva, dani := range s.koef {
			if _, ima := ciljevi[letva]; !ima || dani[0] != nil {
				t.Errorf("skup %d, %s: nije u prvom skupu ili ima dan 0", i+1, letva)
			}
			for k := 1; k <= DnevniDosezi; k++ {
				if red := dani[k]; len(red) != 3+2*len(s.letve) || red[len(red)-2] > red[len(red)-1] {
					t.Errorf("skup %d, %s %d. dan: %v", i+1, letva, k, red)
				}
			}
		}
	}
	for letva := range ciljevi {
		if _, lanac := UgadjanjeCvorova[letva]; lanac || VrhoviIzDnevnog[letva] {
			t.Errorf("%s je u satnom lancu: ispravak bi mijenjao lanac", letva)
		}
		if !slices.ContainsFunc(DnevniCiljevi, func(c DnevniCilj) bool { return c.Letva == letva }) {
			t.Errorf("%s nije dnevni cilj", letva)
		}
	}
	for _, l := range UzvodneLetve {
		if slices.Contains(DnevniUlazi(), l) {
			t.Errorf("%s je ulaz dnevnog modela", l)
		}
	}
}

// otisakDnevnogModela su postavke dnevnog modela o kojima ovise greške na
// kojima su tablice ispravka naučene, i račun promjena uzvodnih letvi.
func otisakDnevnogModela() string {
	var b strings.Builder
	for _, c := range DnevniCiljevi {
		if ImaUzvodniIspravak(c.Letva) {
			fmt.Fprintf(&b, "%s: ulazi %v, kiša %v, rezerve %v\n", c.Letva, c.Ulazi, c.Slivovi, DnevneRezerve[c.Letva])
		}
	}
	fmt.Fprintf(&b, "dosezi %d, udio regresije %v, analogija %d, težinske %v, režim kiše %v, kvadrati kiše %v\n",
		DnevniDosezi, DnevniUdioRegresije, DnevnihAnalogija, DnevneAnalogijeTezinske, DnevniRezimKise, DnevneKvadratneKise)
	fmt.Fprintf(&b, "kiša: dani %v, unaprijed %v (%v), korijen %v, težina u analogijama %v\n",
		OborinskiDani, OborinskiDaniUnaprijed, OborinaUnaprijed, OborinaKorijen, OborinaTezinaKNN)
	fmt.Fprintf(&b, "dan uživo iz najmanje %d sati\n", satiZaDan)
	fmt.Fprintf(&b, "uzvodno: %v, kašnjenje %d h, skok %v cm", UzvodneLetve, UzvodnoKasnjenje, UzvodniSkok)
	return b.String()
}

// izmisljeniDnevniNizovi su stalni izmišljeni dnevni nizovi letvi i kiše
// međuslivova četiriju ciljeva s ispravkom: letve slijede uzvodne s danom
// kašnjenja i kišu, uz šum iz stalnog sjemena. Sve je od 0 do dana−1, kiša
// deset dana dulje.
func izmisljeniDnevniNizovi(dana int64) map[string]DnevniNiz {
	rnd := rand.New(rand.NewPCG(24, 10))
	out := map[string]DnevniNiz{}
	for _, s := range []string{"H", "I", "J"} {
		n := DnevniNiz{}
		for d := int64(0); d < dana+10; d++ {
			n[d] = max(0, 30*rnd.Float64()-18)
		}
		out[OborinaKljuc(s)] = n
	}
	val := func(d int64, f float64) float64 {
		return 150*math.Sin(float64(d)/(9+f)) + 60*math.Sin(float64(d)/(3.7+f/3))
	}
	prije := DnevniNiz{}
	for i, l := range []string{"komarom", "budapest", "mohacs", "batina", "osijek", "donji-miholjac", "aljmas", "vukovar", "ilok"} {
		n := DnevniNiz{}
		for d := int64(0); d < dana; d++ {
			kisa := out[OborinaKljuc("H")][d] + out[OborinaKljuc("J")][max(d-1, 0)]
			n[d] = 300 + 0.6*prije[d-1] + 0.4*val(d, float64(i)) + 2*kisa + 15*rnd.Float64()
		}
		out[l], prije = n, n
	}
	return out
}

// izlazModela su promjena prema danu 0 i polovina raspona dnevne prognoze
// za 1.–6. dan, zaokruženo na 0,001 cm.
type izlazModela [DnevniDosezi][2]float64

// izlazDnevnogModela uči dnevni model cilja na izmišljenim nizovima i
// prognozira ga putem kojim ide uživo (satni nizovi i kiša oko izdanja), bez
// uzvodnih letvi.
func izlazDnevnogModela(t *testing.T, c DnevniCilj, nizovi map[string]DnevniNiz, dan int64) izlazModela {
	t.Helper()
	m, err := NamjestiDnevni(nizovi, c, 0)
	if err != nil {
		t.Fatal(err)
	}
	sada := dan*24 + 6
	satni := map[string]Niz{}
	for _, l := range c.letve() {
		v := map[int64]float64{}
		for r := int64(-4); r <= 0; r++ {
			for h := sada + 24*r - 23; h <= sada+24*r; h++ {
				v[h] = nizovi[l][dan+r]
			}
		}
		satni[l] = NoviNiz(v)
	}
	oborine := map[string]DnevniNiz{}
	for _, s := range c.Slivovi {
		n := DnevniNiz{}
		for r := int64(-10); r <= 10; r++ {
			n[r] = nizovi[OborinaKljuc(s)][dan+r]
		}
		oborine[OborinaKljuc(s)] = n
	}
	d, err := PrognozirajDnevno(m, satni, oborine, sada)
	if err != nil {
		t.Fatal(err)
	}
	var out izlazModela
	for k := 1; k <= DnevniDosezi; k++ {
		out[k-1] = [2]float64{math.Round((d[k].Vrijednost-d[0].Vrijednost)*1000) / 1000, math.Round(d[k].Raspon()*1000) / 1000}
	}
	return out
}

// uzvodniOtisakIzlaza je izlaz dnevnog modela ciljeva s ispravkom na
// izmišljenim nizovima (izlazDnevnogModela), uz postavke iz uzvodniOtisak.
var uzvodniOtisakIzlaza = map[string]izlazModela{
	"batina":  {{7.453, 14.368}, {13.144, 16.247}, {16.044, 22.881}, {17.481, 30.778}, {10.476, 33.451}, {8.189, 35.164}},
	"aljmas":  {{11.794, 8.796}, {22.371, 10.709}, {27.229, 18.708}, {30.919, 24.009}, {27.327, 27.758}, {26.247, 34.006}},
	"vukovar": {{7.001, 13.152}, {13.789, 11.55}, {19.042, 14.629}, {21.483, 16.978}, {18.096, 20.379}, {19.383, 22.48}},
	"ilok":    {{8.302, 13.133}, {12.196, 16.858}, {14.602, 22.092}, {15.742, 24.972}, {11.919, 28.173}, {11.746, 31.258}},
}

// Test-kočnica za račun: izlaz dnevnog modela ciljeva s ispravkom na stalnim
// izmišljenim nizovima, putem kojim ide uživo. Pada kad se promijeni nešto
// što otisak postavki ne vidi: učenje, analogije, značajke ili dan iz satnih
// vrijednosti. Tablice ispravka tada treba izračunati iznova (komentar uz
// uzvodniKoefWA), pa otisak izlaza prepisati iz ispisa ovog testa.
func TestUzvodniOtisakIzlaza(t *testing.T) {
	nizovi := izmisljeniDnevniNizovi(3600)
	for _, c := range DnevniCiljevi {
		if !ImaUzvodniIspravak(c.Letva) {
			continue
		}
		got := izlazDnevnogModela(t, c, nizovi, 3590)
		want, ima := uzvodniOtisakIzlaza[c.Letva]
		jednako := ima
		for k := range got {
			for j := range got[k] {
				jednako = jednako && math.Abs(got[k][j]-want[k][j]) <= 0.002
			}
		}
		if !jednako {
			t.Errorf("račun dnevnog modela promijenjen je, a tablica uzvodnog ispravka nije obnovljena; %q: %#v", c.Letva, got)
		}
	}
	postavi(t, &DnevnihAnalogija, 20)
	if got := izlazDnevnogModela(t, DnevniCilj{Letva: "batina", Ulazi: []string{"komarom", "budapest", "mohacs"}, Slivovi: []string{"H", "I", "J"}},
		nizovi, 3590); got == uzvodniOtisakIzlaza["batina"] {
		t.Error("otisak izlaza ne vidi promjenu broja analogija")
	}
}

// Test-kočnica: tablice ispravka naučene su na greškama dnevnog modela s
// ovim postavkama. Kad se koja promijeni, tablice treba izračunati iznova
// (komentar uz uzvodniKoefWA), pa otisak prepisati.
func TestUzvodniOtisak(t *testing.T) {
	if got := otisakDnevnogModela(); got != uzvodniOtisak {
		t.Errorf("postavke dnevnog modela promijenjene su, a tablica uzvodnog ispravka nije obnovljena:\n%s\nželim\n%s", got, uzvodniOtisak)
	}
	postavi(t, &DnevnihAnalogija, 20)
	if otisakDnevnogModela() == uzvodniOtisak {
		t.Error("otisak ne vidi promjenu broja analogija")
	}
}
