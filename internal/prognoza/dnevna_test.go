package prognoza

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

// Na izmišljenoj rijeci cilj ponavlja ulaz dva dana kasnije. Model naučen na
// prvom dijelu mora na drugom pogađati bolje od postojanosti, i na 1. i na
// 3. dan.
func TestDnevniPobjedujePostojanost(t *testing.T) {
	ulaz, cilj := DnevniNiz{}, DnevniNiz{}
	val := func(d int64) float64 {
		return 300 + 150*math.Sin(float64(d)/9) + 60*math.Sin(float64(d)/3.7)
	}
	for d := int64(0); d < 8000; d++ {
		ulaz[d] = val(d)
		cilj[d] = val(d-2) + 20
	}
	c := DnevniCilj{Letva: "cilj", Ulazi: []string{"ulaz"}}
	nizovi := map[string]DnevniNiz{"cilj": cilj, "ulaz": ulaz}
	m, err := NamjestiDnevni(nizovi, c, 6000)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []int{1, 3} {
		var mi, post float64
		n := 0
		for d := int64(6010); d < 7990; d++ {
			x, ok := DnevneZnacajke(c, nizovi, d)
			if !ok {
				continue
			}
			p, _ := m.Prognoziraj(x)
			stvarno := cilj[d+int64(k)] - cilj[d]
			mi += (p[k] - stvarno) * (p[k] - stvarno)
			post += stvarno * stvarno
			n++
		}
		mi, post = math.Sqrt(mi/float64(n)), math.Sqrt(post/float64(n))
		if mi > post/3 {
			t.Errorf("%d. dan: promašaj %.1f cm, postojanost %.1f — model ne zna ništa", k, mi, post)
		}
	}
}

// Dan 0 je srednjak 24 sata do sata izdavanja; dan kojem fali više od šest
// sati izostaje, da prognoza ne krene iz pola dana.
func TestDnevniIzSatnog(t *testing.T) {
	v := map[int64]float64{}
	for h := int64(1000); h <= 1100; h++ {
		v[h] = float64(h)
	}
	for h := int64(1030); h <= 1045; h++ {
		delete(v, h) // rupa preko NajveciRazmak ne premošćuje se
	}
	d := DnevniIzSatnog(NoviNiz(v), 1100, 4)
	if got, want := d[0], float64(1077+1100)/2; math.Abs(got-want) > 1e-9 {
		t.Errorf("dan 0: %g umjesto %g", got, want)
	}
	if _, ima := d[-1]; !ima {
		t.Errorf("dan -1 je pun, a izostao je")
	}
	if _, ima := d[-2]; ima {
		t.Errorf("dan -2 ima rupu od 16 sati, a ipak je izračunat: %g", d[-2])
	}
}

// Ulazi dnevnog modela obuhvaćaju i rezerve, a inačice idu glavna pa rezerve.
func TestDnevneRezerveUlazeUUlaze(t *testing.T) {
	ulazi := DnevniUlazi()
	ima := func(l string) bool {
		for _, u := range ulazi {
			if u == l {
				return true
			}
		}
		return false
	}
	if !ima("borl-i") || !ima("varazdin") {
		t.Errorf("ulazi dnevnog modela: %v", ulazi)
	}
	for _, c := range DnevniCiljevi {
		if c.Letva == "botovo" {
			in := c.Inacice()
			// glavni i rezerva s oborinom, pa isti redom bez oborine
			if len(in) != 4 || in[0].Ulazi[1] != "borl-i" || in[1].Ulazi[1] != "varazdin" ||
				len(in[0].Slivovi) != 3 || len(in[1].Slivovi) != 3 ||
				in[2].Ulazi[1] != "borl-i" || in[3].Ulazi[1] != "varazdin" || in[2].Slivovi != nil || in[3].Slivovi != nil ||
				!in[2].BezOborine() || in[0].BezOborine() {
				t.Errorf("inačice Botova: %+v", in)
			}
		}
	}
}

// nizSRupama je satni niz od od do do (100 cm), bez sati iz zadanih raspona
// [a, b].
func nizSRupama(od, do int64, rupe ...[2]int64) Niz {
	v := map[int64]float64{}
	for t := od; t <= do; t++ {
		v[t] = 100
	}
	for _, r := range rupe {
		for t := r[0]; t <= r[1]; t++ {
			delete(v, t)
		}
	}
	return NoviNiz(v)
}

// Dnevni model kojem ulaz nema mjerenja mora reći koja letva i od kada do
// kada: upozorenje uz izdanje iz toga piše što ne radi. Tekst greške ostaje
// isti, jer ga dnevnik i alati već ispisuju.
func TestDnevniBezUlazaImenujeRupu(t *testing.T) {
	sada := int64(500000)
	satni := map[string]Niz{
		"donja":  nizSRupama(sada-200, sada),
		"gornja": nizSRupama(sada-200, sada, [2]int64{sada - 34, sada - 14}), // 21 sat bez mjerenja
	}
	m := &DnevniModel{Cilj: DnevniCilj{Letva: "donja", Ulazi: []string{"gornja"}}}
	_, err := PrognozirajDnevno(m, satni, nil, sada)
	var nd *NepotpuniDani
	if !errors.As(err, &nd) {
		t.Fatalf("greška %v nije NepotpuniDani", err)
	}
	if !reflect.DeepEqual(nd.Fali, []string{"gornja"}) {
		t.Errorf("fali %v, želim [gornja]", nd.Fali)
	}
	if want := []RupaUlaza{{Letva: "gornja", Zadnje: sada - 35, Sljedece: sada - 13}}; !reflect.DeepEqual(nd.Rupe, want) {
		t.Errorf("rupe %+v, želim %+v", nd.Rupe, want)
	}
	if got, want := err.Error(), "donja: nepotpuna zadnja četiri dana na [gornja]"; got != want {
		t.Errorf("tekst greške %q, želim %q", got, want)
	}
}

// Značajke traže dane 0, −1 i −3. Letva kojoj fali samo dan −2 ne smije biti
// okrivljena: dosad je greška imenovala svaku letvu s manje od četiri dana.
func TestDnevniNeOkrivljujeLetvuKojojFaliSamoDrugiDan(t *testing.T) {
	sada := int64(500000)
	satni := map[string]Niz{
		"cilj": nizSRupama(sada-200, sada),
		"a":    nizSRupama(sada-200, sada, [2]int64{sada - 66, sada - 52}), // samo dan −2
		"b":    nizSRupama(sada-200, sada, [2]int64{sada - 90, sada - 76}), // dan −3
	}
	_, err := PrognozirajDnevno(&DnevniModel{Cilj: DnevniCilj{Letva: "cilj", Ulazi: []string{"a", "b"}}}, satni, nil, sada)
	var nd *NepotpuniDani
	if !errors.As(err, &nd) || !reflect.DeepEqual(nd.Fali, []string{"b"}) {
		t.Fatalf("greška %v, želim da fali samo b", err)
	}
	if len(nd.Rupe) != 1 || nd.Rupe[0].Zadnje != sada-91 || nd.Rupe[0].Sljedece != sada-75 {
		t.Errorf("rupa b: %+v", nd.Rupe)
	}
	d, err := PrognozirajDnevno(&DnevniModel{Cilj: DnevniCilj{Letva: "cilj", Ulazi: []string{"a"}}}, satni, nil, sada)
	if err != nil || len(d) != DnevniDosezi+1 {
		t.Errorf("s a bez dana −2 model mora računati: %v, %d dana", err, len(d))
	}
}

// Rupa koja još traje nema sljedećeg očitanja. Stara, zatvorena rupa
// ranije u prozoru ne smije je sakriti.
func TestRupaKojaJosTraje(t *testing.T) {
	sada := int64(500000)
	satni := map[string]Niz{
		"cilj": nizSRupama(sada-200, sada),
		"ulaz": nizSRupama(sada-200, sada-8, [2]int64{sada - 66, sada - 52}),
	}
	_, err := PrognozirajDnevno(&DnevniModel{Cilj: DnevniCilj{Letva: "cilj", Ulazi: []string{"ulaz"}}}, satni, nil, sada)
	var nd *NepotpuniDani
	if !errors.As(err, &nd) {
		t.Fatalf("greška %v nije NepotpuniDani", err)
	}
	if want := []RupaUlaza{{Letva: "ulaz", Zadnje: sada - 8}}; !reflect.DeepEqual(nd.Rupe, want) {
		t.Errorf("rupe %+v, želim %+v", nd.Rupe, want)
	}
	if _, ima := rupaUlaza(satni["cilj"], sada); ima {
		t.Error("pun niz nema rupe")
	}
}

// Ulaz koji kasni sat iza izdanja ima kratku rupu na samom kraju; dan je
// oborila duga rupa prije nje i nju upozorenje mora imenovati.
func TestRupaJeOnaKojaJeOborilaDan(t *testing.T) {
	sada := int64(500000)
	satni := map[string]Niz{
		"cilj": nizSRupama(sada-200, sada),
		"ulaz": nizSRupama(sada-200, sada-1, [2]int64{sada - 34, sada - 14}),
	}
	_, err := PrognozirajDnevno(&DnevniModel{Cilj: DnevniCilj{Letva: "cilj", Ulazi: []string{"ulaz"}}}, satni, nil, sada)
	var nd *NepotpuniDani
	if !errors.As(err, &nd) {
		t.Fatalf("greška %v nije NepotpuniDani", err)
	}
	if want := []RupaUlaza{{Letva: "ulaz", Zadnje: sada - 35, Sljedece: sada - 13}}; !reflect.DeepEqual(nd.Rupe, want) {
		t.Errorf("rupe %+v, želim %+v", nd.Rupe, want)
	}
}

// Ulaz koji je prije dva dana bio dulje bez mjerenja, a sad ih nema osam
// sati: rupa je ona koja traje, ne stara i dulja, jer bi upozorenje inače
// reklo da su se mjerenja vratila. Dok rupa na kraju sama ne obara dan (do
// šest sati), dan je oborila stara i ona se imenuje.
func TestStaraDuljaRupaNeSkrivaOnuKojaTraje(t *testing.T) {
	sada := int64(500000)
	rupaNa := func(zadnje int64) []RupaUlaza {
		t.Helper()
		satni := map[string]Niz{
			"cilj": nizSRupama(sada-200, sada),
			"ulaz": nizSRupama(sada-200, zadnje, [2]int64{sada - 45, sada - 25}), // 21 sat, dan −1
		}
		_, err := PrognozirajDnevno(&DnevniModel{Cilj: DnevniCilj{Letva: "cilj", Ulazi: []string{"ulaz"}}}, satni, nil, sada)
		var nd *NepotpuniDani
		if !errors.As(err, &nd) {
			t.Fatalf("greška %v nije NepotpuniDani", err)
		}
		return nd.Rupe
	}
	if got, want := rupaNa(sada-8), []RupaUlaza{{Letva: "ulaz", Zadnje: sada - 8}}; !reflect.DeepEqual(got, want) {
		t.Errorf("osam sati bez mjerenja: rupe %+v, želim %+v", got, want)
	}
	if got, want := rupaNa(sada-6), []RupaUlaza{{Letva: "ulaz", Zadnje: sada - 46, Sljedece: sada - 24}}; !reflect.DeepEqual(got, want) {
		t.Errorf("šest sati bez mjerenja: rupe %+v, želim %+v", got, want)
	}
}

// Cilj s kišom kojemu fali oborina imenuje sliv, ne letvu, i nema rupe u
// satnom nizu; s kišom računa i nosi oznaku modela s kišom.
func TestDnevniSKisomImenujeSliv(t *testing.T) {
	sada := int64(500000)
	satni := map[string]Niz{"cilj": nizSRupama(sada-200, sada)}
	m := &DnevniModel{Cilj: DnevniCilj{Letva: "cilj", Slivovi: []string{"sliv"}}}
	_, err := PrognozirajDnevno(m, satni, nil, sada)
	var nd *NepotpuniDani
	if !errors.As(err, &nd) || !reflect.DeepEqual(nd.Fali, []string{"oborina sliv"}) || nd.Rupe != nil {
		t.Fatalf("greška %v, želim da fali samo oborina sliv, bez rupa", err)
	}
	kisa := DnevniNiz{}
	for d := int64(-7); d <= 7; d++ {
		kisa[d] = 0
	}
	d, err := PrognozirajDnevno(m, satni, map[string]DnevniNiz{OborinaKljuc("sliv"): kisa}, sada)
	if err != nil || len(d) != DnevniDosezi+1 || d[0].Model != ModelDnevniKisa {
		t.Errorf("s kišom: %v, %+v", err, d)
	}
}
