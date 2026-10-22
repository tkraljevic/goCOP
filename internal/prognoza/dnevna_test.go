package prognoza

import (
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"gocop/internal/models"
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

// Greške inačica istog cilja spajaju se: svaka letva jednom, redom kojim su
// inačice pale, oborine iza letvi; rupa letve ostaje ona prve inačice koja
// ju je našla.
func TestSpojiNepotpune(t *testing.T) {
	glavna := &NepotpuniDani{Letva: "donja", Fali: []string{"glavni", "oborina A"},
		Rupe: []RupaUlaza{{Letva: "glavni", Zadnje: 10, Sljedece: 30}}}
	rezerva := &NepotpuniDani{Letva: "donja", Fali: []string{"pricuvni", "oborina A"},
		Rupe: []RupaUlaza{{Letva: "pricuvni", Zadnje: 5, Sljedece: 28}}}
	bezKise := &NepotpuniDani{Letva: "donja", Fali: []string{"glavni"},
		Rupe: []RupaUlaza{{Letva: "glavni", Zadnje: 11, Sljedece: 30}}}
	if got := spojiNepotpune(nil, glavna); got != glavna {
		t.Errorf("prva greška mora ostati kakva jest: %v", got)
	}
	got := spojiNepotpune(spojiNepotpune(glavna, rezerva), bezKise)
	want := &NepotpuniDani{Letva: "donja", Fali: []string{"glavni", "pricuvni", "oborina A"},
		Rupe: []RupaUlaza{{Letva: "glavni", Zadnje: 10, Sljedece: 30}, {Letva: "pricuvni", Zadnje: 5, Sljedece: 28}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("spojeno %+v, želim %+v", got, want)
	}
	if got.Error() != "donja: nepotpuna zadnja četiri dana na [glavni pricuvni oborina A]" {
		t.Errorf("tekst: %q", got.Error())
	}
	if !reflect.DeepEqual(glavna.Fali, []string{"glavni", "oborina A"}) {
		t.Errorf("spajanje je promijenilo grešku glavne inačice: %v", glavna.Fali)
	}
	druga := errors.New("nešto drugo")
	if got := spojiNepotpune(glavna, druga); got != druga {
		t.Errorf("greška koja nije NepotpuniDani ostaje zadnja: %v", got)
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

// znacajkeSKisom slaže značajke dana 10 za cilj s ulazom i dva međusliva:
// na A pada 1, 2, 3 … mm po danu (dan d ima d mm), na B dvostruko.
func znacajkeSKisom(t *testing.T) (*DnevniModel, []float64) {
	t.Helper()
	c := DnevniCilj{Letva: "cilj", Ulazi: []string{"ulaz"}, Slivovi: []string{"A", "B"}}
	nizovi := map[string]DnevniNiz{"cilj": {}, "ulaz": {}, OborinaKljuc("A"): {}, OborinaKljuc("B"): {}}
	for d := int64(0); d <= 20; d++ {
		nizovi["cilj"][d] = 200 + float64(d)
		nizovi["ulaz"][d] = 100 + float64(d)
		nizovi[OborinaKljuc("A")][d] = float64(d)
		nizovi[OborinaKljuc("B")][d] = 2 * float64(d)
	}
	x, ok := DnevneZnacajke(c, nizovi, 10)
	if !ok {
		t.Fatal("dan 10 nema značajki")
	}
	return &DnevniModel{Cilj: c}, x
}

// Kišni dan mjeri se po svakom međuslivu zbrojem kiše zadnja tri dana i,
// kad prognoza kiše ulazi u model, sljedeća četiri. Značajke koje ne sežu do
// kiše ne daju ništa.
func TestZbrojKiseKisnogDana(t *testing.T) {
	m, x := znacajkeSKisom(t)
	// A: palo 10+9+8 = 27, dolazi 11+12+13+14 = 50; B dvostruko.
	if z := m.zbrojKise(x); z != 3*(27+50) {
		t.Errorf("s prognozom kiše %g, želim %d", z, 3*(27+50))
	}
	if z := m.zbrojKise(x[:m.oborinaOd()+1]); z != 0 {
		t.Errorf("značajke bez kiše dale su %g", z)
	}

	postavi(t, &OborinaUnaprijed, false)
	m, x = znacajkeSKisom(t)
	if z := m.zbrojKise(x); z != 3*27 {
		t.Errorf("bez prognoze kiše %g, želim %d", z, 3*27)
	}
}

// Režim dana: kišni kad zbroj kiše dosegne prag kiše, inače visoka voda kad
// razina cilja dosegne prag, inače obični. Bez praga kiše kišnog režima nema.
func TestRezimDnevnogModela(t *testing.T) {
	m, x := znacajkeSKisom(t) // razina cilja 210, zbroj kiše 231
	for _, s := range []struct {
		prag, pragKise float64
		want           int
	}{
		{prag: 300, pragKise: 231, want: 2},
		{prag: 100, pragKise: 232, want: 1},
		{prag: 210, pragKise: 0, want: 1},
		{prag: 211, pragKise: 0, want: 0},
	} {
		m.prag, m.pragKise = s.prag, s.pragKise
		if got := m.rezim(x); got != s.want {
			t.Errorf("prag %g, prag kiše %g: režim %d, želim %d", s.prag, s.pragKise, got, s.want)
		}
	}
}

// Kvadrati kiše dolaze iza svih linearnih značajki: prvi je kvadrat prve
// značajke kiše (podijeljen sa sto). Bez kvadrata granica je kraj značajki.
func TestKvadratiKiseIzaLinearnih(t *testing.T) {
	for _, unaprijed := range []bool{true, false} {
		postavi(t, &OborinaUnaprijed, unaprijed)
		m, x := znacajkeSKisom(t)
		if od := m.kvadratiOd(); od != len(x) {
			t.Errorf("unaprijed %v, bez kvadrata: granica %d, značajki %d", unaprijed, od, len(x))
		}
		postavi(t, &DnevneKvadratneKise, true)
		m, x = znacajkeSKisom(t)
		od := m.kvadratiOd()
		prva := x[m.oborinaOd()]
		if od >= len(x) || x[od] != prva*prva/100 {
			t.Errorf("unaprijed %v: na granici %d stoji %v, želim %g", unaprijed, od, x, prva*prva/100)
		}
		postavi(t, &DnevneKvadratneKise, false)
	}
}

// Dan od kojeg dnevni model daje vrijednost ovisi o vodi u satu izdavanja:
// do praga visoke vode vrijedi granica za nisku vodu, iznad nje i kad
// vodostaja nema granica s poplavnih valova.
func TestDnevnaOdDanaZa(t *testing.T) {
	postavi(t, &VisokaVoda, map[string]float64{"proba": 100, "bez-niske": 50})
	postavi(t, &DnevnaOdDanaNiska, map[string]int{"proba": 1})
	postavi(t, &DnevnaOdDana, map[string]int{"proba": 4, "bez-niske": 3})
	for _, s := range []struct {
		letva   string
		sada    float64
		imaSada bool
		want    int
	}{
		{"proba", 80, true, 1},
		{"proba", 100, true, 1},
		{"proba", 120, true, 4},
		{"proba", 80, false, 4},
		{"bez-niske", 10, true, 3},
		{"nepoznata", 10, true, 0},
	} {
		if got := DnevnaOdDanaZa(s.letva, s.sada, s.imaSada); got != s.want {
			t.Errorf("%s pri %g (ima %v): od %d. dana, želim %d", s.letva, s.sada, s.imaSada, got, s.want)
		}
	}
}

// Pri visokoj vodi Botovo, Donji Miholjac i Belišće uzimaju dnevni model od
// 2., 3. i 4. dana (Odluke (22. 10. 2026.)); pri niskoj i srednjoj vodi i
// ostale dravske letve ostaju kakve su bile.
func TestDnevnaOdDanaZaDravaPriVisokojVodi(t *testing.T) {
	for letva, want := range map[string]struct{ visoka, niska int }{
		"botovo": {2, 1}, "terezino-polje": {3, 1}, "donji-miholjac": {3, 1}, "belisce": {4, 2}, "osijek": {5, 2},
	} {
		prag := VisokaVoda[letva]
		if got := DnevnaOdDanaZa(letva, prag+1, true); got != want.visoka {
			t.Errorf("%s pri visokoj vodi: od %d. dana, želim %d", letva, got, want.visoka)
		}
		if got := DnevnaOdDanaZa(letva, prag-1, true); got != want.niska {
			t.Errorf("%s pri niskoj vodi: od %d. dana, želim %d", letva, got, want.niska)
		}
	}
}

// Točka ulazi u dnevnu prognozu samo kad je aktivna izvedena točka s težinom
// u međuslivu čiju kišu model čita; inače se kaže zašto ne.
func TestUlaziUPrognozu(t *testing.T) {
	slivovi := map[string][]string{"A": {"botovo"}}
	for _, s := range []struct {
		ime     string
		stvarni bool
		aktivan bool
		sliv    string
		tezina  float64
		ulazi   bool
		razlog  string
	}{
		{"izvedena u A", false, true, "A", 0.3, true, ""},
		{"pravi kišomjer", true, true, "A", 0.3, false, "pravi kišomjer služi za usporedbu; model čita izvedene točke"},
		{"neaktivna", false, false, "A", 0.3, false, "točka nije aktivna"},
		{"bez međusliva", false, true, "", 0.3, false, "točka nije u međuslivu"},
		{"bez težine", false, true, "A", 0, false, "točka nema težinu u međuslivu"},
		{"međusliv izvan prognoze", false, true, "Z", 0.3, false, "kiša međusliva Z ne ulazi u dnevnu prognozu"},
	} {
		ulazi, razlog := UlaziUPrognozu(s.stvarni, s.aktivan, s.sliv, s.tezina, slivovi)
		if ulazi != s.ulazi || razlog != s.razlog {
			t.Errorf("%s: %v %q, želim %v %q", s.ime, ulazi, razlog, s.ulazi, s.razlog)
		}
	}
}

// registarKisomjera je registar s tablicom kišomjera, samo sa stupcima koje
// prognoza čita.
func registarKisomjera(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE kisomjeri (code TEXT PRIMARY KEY, sliv TEXT NOT NULL DEFAULT '',
		tezina REAL, aktivan INTEGER NOT NULL DEFAULT 1, vrsta TEXT NOT NULL DEFAULT 'izvedeni')`); err != nil {
		t.Fatal(err)
	}
	return db
}

// Oborinske točke su aktivne izvedene točke u međuslivu; točka bez težine
// dobiva nulu, a pravi kišomjeri, neaktivne točke i točke izvan međusliva
// ne ulaze. Nečitljiv redak ili registar bez tablice je greška.
func TestOborinskeTocke(t *testing.T) {
	db := registarKisomjera(t)
	if _, err := db.Exec(`INSERT INTO kisomjeri (code, sliv, tezina, aktivan, vrsta) VALUES
		('a-1', 'A', 0.6, 1, 'izvedeni'), ('a-2', 'A', NULL, 1, 'izvedeni'),
		('a-3', 'A', 0.4, 0, 'izvedeni'), ('a-4', 'A', 0.4, 1, 'stvarni'),
		('x-1', '', 0.5, 1, 'izvedeni'), ('b-1', 'B', 1, 1, 'izvedeni')`); err != nil {
		t.Fatal(err)
	}
	tocke, err := OborinskeTocke(db)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(tocke, func(i, j int) bool { return tocke[i].Code < tocke[j].Code })
	want := []OborinskaTocka{{"a-1", "A", 0.6}, {"a-2", "A", 0}, {"b-1", "B", 1}}
	if !reflect.DeepEqual(tocke, want) {
		t.Errorf("točke %+v, želim %+v", tocke, want)
	}

	if _, err := db.Exec(`UPDATE kisomjeri SET tezina = 'x' WHERE code = 'b-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := OborinskeTocke(db); err == nil {
		t.Error("težina koja nije broj prošla je bez greške")
	}
	if _, err := db.Exec(`DROP TABLE kisomjeri`); err != nil {
		t.Fatal(err)
	}
	if _, err := OborinskeTocke(db); err == nil {
		t.Error("registar bez kišomjera bez greške")
	}
}

// Broj dana učenja i polovina raspona dnevne prognoze.
func TestUzorakaIRasponDnevne(t *testing.T) {
	m := &DnevniModel{uzorci: make([]dnevniUzorak, 3)}
	if m.Uzoraka() != 3 {
		t.Errorf("uzoraka %d, želim 3", m.Uzoraka())
	}
	if r := (DnevnaIzdana{Vrijednost: 100, Dolje: 92, Gore: 110}).Raspon(); r != 9 {
		t.Errorf("raspon %g, želim 9", r)
	}
}

// Inačica bez oborine je ona kojoj glavni cilj ima oborinu, a ona sama ne.
// Cilj koji i sam ima oborinu ili letva koja nije dnevni cilj to nisu.
func TestBezOborine(t *testing.T) {
	for _, s := range []struct {
		c    DnevniCilj
		want bool
	}{
		{DnevniCilj{Letva: "batina", Ulazi: []string{"mohacs"}}, true},
		{DnevniCilj{Letva: "batina", Ulazi: []string{"mohacs"}, Slivovi: []string{"H"}}, false},
		{DnevniCilj{Letva: "nepoznata", Ulazi: []string{"batina"}}, false},
	} {
		if got := s.c.BezOborine(); got != s.want {
			t.Errorf("%+v: %v, želim %v", s.c, got, s.want)
		}
	}
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "batina", Ulazi: []string{"mohacs"}}})
	if (DnevniCilj{Letva: "batina", Ulazi: []string{"bezdan"}}).BezOborine() {
		t.Error("glavni cilj bez oborine: inačica nije „bez oborine”")
	}
}

// Težina satnog lanca na Dunavu: 1 u satu izdanja, 0,56 od 6. do 72. sata,
// 0 od 96. sata; između čvorova pravocrtno.
func TestTezinaLanca(t *testing.T) {
	for h, want := range map[int]float64{0: 1, 3: 0.78, 6: 0.56, 24: 0.56, 72: 0.56, 84: 0.28, 96: 0, 120: 0} {
		if got := TezinaLanca(h); math.Abs(got-want) > 1e-9 {
			t.Errorf("%d h: %g, želim %g", h, got, want)
		}
	}
	if !Ugadja("vukovar") || Ugadja("osijek") {
		t.Error("ugađanje je na Dunavu, a ne na Dravi")
	}
	if !Ugadja("bezdan") || !Ugadja("apatin") || !Ugadja("backa-palanka") || Ugadja("bogojevo") {
		t.Error("prenesena letva ima ugađanje kad ga imaju svi njezini izvori, a Bogojevo (iz Dalja) nema")
	}
}

// UProtok vodi vrijednost i granice kroz krivulju; vrijednost izvan krivulje
// protoka nema.
func TestUProtok(t *testing.T) {
	k := &models.HQKrivulja{Odsjecci: []models.HQOdsjecak{{OdCm: 0, DoCm: 300, Oblik: models.OblikPolinom, P2: 100}}}
	q, qd, qg, ok := UProtok(k, 100, 90, 110)
	if !ok || q != 100 || qd >= q || qg <= q {
		t.Errorf("protok %g [%g, %g] %v, želim 100 s rasponom", q, qd, qg, ok)
	}
	if _, _, _, ok := UProtok(k, 5000, 4990, 5010); ok {
		t.Error("vodostaj izvan krivulje, a protok ima")
	}
}
