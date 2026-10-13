package prognoza

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// dnevniSKoeficijentima je dnevni model bez analogija kojem je promjena k.
// dana zadana: regresija je samo slobodni član, isti u oba režima.
func dnevniSKoeficijentima(c DnevniCilj, promjena func(k int) float64) *DnevniModel {
	m := &DnevniModel{Cilj: c}
	for k := 1; k <= DnevniDosezi; k++ {
		m.koef[k][0] = []float64{promjena(k)}
		m.koef[k][1] = []float64{promjena(k)}
	}
	return m
}

// Letva iz DnevniBezKiseDoDana prve dane uzima iz inačice bez kiše: prvu iz
// popisa koja računa, a inačice s kišom, prazna mjesta i one kojima fale
// ulazi preskače. Ostali dani i polazni dan 0 ostaju iz modela s kišom, a
// zatečena prognoza se ne dira.
func TestSastaviBezKise(t *testing.T) {
	postavi(t, &DnevniBezKiseDoDana, map[string]int{"proba": 2})
	const sada = int64(500000)
	satni := map[string]Niz{"proba": nizSRupama(sada-200, sada)} // 100 cm
	sKisom := DnevniCilj{Letva: "proba", Slivovi: []string{"S"}}
	m := &DnevniModel{Cilj: sKisom}
	var d []DnevnaIzdana
	for k := 0; k <= DnevniDosezi; k++ {
		v := 100 + 10*float64(k)
		d = append(d, DnevnaIzdana{Letva: "proba", Izdano: sada, Dan: k, Ciljni: sada + int64(24*k),
			Vrijednost: v, Dolje: v - 3, Gore: v + 3, Model: ModelDnevniKisa})
	}
	zateceno := append([]DnevnaIzdana(nil), d...)

	bez := dnevniSKoeficijentima(DnevniCilj{Letva: "proba"}, func(k int) float64 { return -5 * float64(k) })
	bezUlaza := dnevniSKoeficijentima(DnevniCilj{Letva: "proba", Ulazi: []string{"nema-mjerenja"}}, func(int) float64 { return 50 })
	modeli := []*DnevniModel{nil, dnevniSKoeficijentima(sKisom, func(int) float64 { return 70 }), bezUlaza, bez}

	got := SastaviBezKise(d, m, modeli, satni, nil, sada)
	if !reflect.DeepEqual(d, zateceno) {
		t.Errorf("zatečena prognoza promijenjena: %+v", d)
	}
	if len(got) != len(d) {
		t.Fatalf("dana %d, želim %d", len(got), len(d))
	}
	for k, x := range got {
		want := d[k]
		if k >= 1 && k <= 2 {
			v := 100 - 5*float64(k)
			want.Vrijednost, want.Dolje, want.Gore, want.Model = v, v-1, v+1, ModelDnevni
		}
		if x != want {
			t.Errorf("%d. dan: %+v, želim %+v", k, x, want)
		}
	}

	if got := SastaviBezKise(d, m, modeli[:3], satni, nil, sada); !reflect.DeepEqual(got, d) {
		t.Errorf("bez inačice koja računa: %+v, želim zatečenu", got)
	}
	if got := SastaviBezKise(d, &DnevniModel{Cilj: DnevniCilj{Letva: "druga", Slivovi: []string{"S"}}}, modeli, satni, nil, sada); !reflect.DeepEqual(got, d) {
		t.Errorf("letva koja to ne traži: %+v", got)
	}
}

// Kiša u značajkama: korijen zbroja kad je OborinaKorijen uključen, a buduća
// kiša iz PrognozaKise kad je zadana — dan za koji prognoze nema znači da
// značajki nema.
func TestDnevneZnacajkeKorijenIPrognozaKise(t *testing.T) {
	c := DnevniCilj{Letva: "cilj", Slivovi: []string{"A"}}
	nizovi := map[string]DnevniNiz{"cilj": {}, OborinaKljuc("A"): {}}
	for d := int64(0); d <= 20; d++ {
		nizovi["cilj"][d] = 200 + float64(d)
		nizovi[OborinaKljuc("A")][d] = float64(d)
	}
	kisa := func(x []float64) []float64 { return x[4:] }

	x, ok := DnevneZnacajke(c, nizovi, 10)
	// palo 10, 10+9+8, 10+…+4; dolazi 11+12, 11+…+14, 11+…+16
	if want := []float64{10, 27, 49, 23, 50, 81}; !ok || !reflect.DeepEqual(kisa(x), want) {
		t.Errorf("kiša %v, želim %v", x, want)
	}

	postavi(t, &OborinaKorijen, true)
	x, ok = DnevneZnacajke(c, nizovi, 10)
	for i, v := range []float64{10, 27, 49, 23, 50, 81} {
		if !ok || math.Abs(kisa(x)[i]-math.Sqrt(v)) > 1e-12 {
			t.Errorf("s korijenom: %v, želim korijene od 10, 27, 49, 23, 50, 81", kisa(x))
			break
		}
	}
	postavi(t, &OborinaKorijen, false)

	var pitano []string
	postavi(t, &PrognozaKise, func(sliv string, t int64, d int) (float64, bool) {
		pitano = append(pitano, sliv)
		return float64(100 * d), t == 10
	})
	x, ok = DnevneZnacajke(c, nizovi, 10)
	if want := []float64{10, 27, 49, 300, 1000, 2100}; !ok || !reflect.DeepEqual(kisa(x), want) {
		t.Errorf("s prognozom kiše %v, želim %v", x, want)
	}
	if len(pitano) != 2+4+6 || pitano[0] != "A" {
		t.Errorf("prognoza kiše pitana %v", pitano)
	}
	if x, ok := DnevneZnacajke(c, nizovi, 11); ok {
		t.Errorf("dan bez prognoze kiše dao je značajke %v", x)
	}
}

// Kiša oko sada: svaka točka daje dan samo kad ima sva 24 sata, točka bez
// težine ne ulazi, a međusliv dobiva dan kad ga ima barem polovica težine —
// težinski srednjak dana prisutnih točaka. Satni nizovi traže se od prvog
// sata najstarijeg dana do zadnjeg sata prognoze.
func TestOborineOkoSada(t *testing.T) {
	const sada = int64(1000)
	sati := func(od, do int64, mm float64, bez ...int64) map[int64]float64 {
		n := map[int64]float64{}
		for h := od; h <= do; h++ {
			n[h] = mm
		}
		for _, h := range bez {
			delete(n, h)
		}
		return n
	}
	var od, do int64
	iz := &OborinskiIzvor{
		Tocke: []OborinskaTocka{
			{Code: "a-1", Sliv: "S", Tezina: 1},
			{Code: "a-2", Sliv: "S", Tezina: 1},
			{Code: "a-3", Sliv: "S", Tezina: 0},
			{Code: "a-4", Sliv: "S", Tezina: 1},
			{Code: "b-1", Sliv: "T", Tezina: 2},
		},
		Satne: func(a, b int64) (map[string]map[int64]float64, error) {
			od, do = a, b
			return map[string]map[int64]float64{
				"a-1": sati(953, 1024, 1),
				"a-2": sati(953, 1024, 2, 1010), // sutra fali sat
				"a-3": sati(953, 1024, 100),     // bez težine
				"b-1": sati(977, 1000, 0.5),     // samo danas
			}, nil
		},
	}
	got, err := OborineOkoSada(iz, sada, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if od != 953 || do != 1024 {
		t.Errorf("satni nizovi traženi za %d–%d, želim 953–1024", od, do)
	}
	// S: danas i jučer a-1 (24 mm) i a-2 (48 mm), dvije od tri težine;
	// sutra samo a-1, trećina težine, premalo.
	want := map[string]DnevniNiz{OborinaKljuc("S"): {-1: 36, 0: 36}, OborinaKljuc("T"): {0: 12}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kiša %v, želim %v", got, want)
	}

	for _, s := range []struct {
		ime string
		iz  *OborinskiIzvor
	}{
		{"bez izvora", nil},
		{"bez točaka", &OborinskiIzvor{Satne: iz.Satne}},
		{"bez satnih nizova", &OborinskiIzvor{Tocke: iz.Tocke}},
	} {
		if got, err := OborineOkoSada(s.iz, sada, 2, 1); err != nil || got == nil || len(got) != 0 {
			t.Errorf("%s: %v, %v — želim praznu kišu bez greške", s.ime, got, err)
		}
	}

	ne := errors.New("baza oborina ne radi")
	pokvaren := &OborinskiIzvor{Tocke: iz.Tocke, Satne: func(int64, int64) (map[string]map[int64]float64, error) { return nil, ne }}
	if got, err := OborineOkoSada(pokvaren, sada, 2, 1); !errors.Is(err, ne) || got != nil {
		t.Errorf("greška satnih nizova: %v, %v", got, err)
	}
}

// Dnevna kiša iz arhive: točka bez težine ne ulazi, a redak koji se ne da
// pročitati ili upit koji pukne usred čitanja je greška.
func TestDnevneOborineTezineIGreske(t *testing.T) {
	arhiva, _ := arhivaUDatoteci(t)
	upisiDnevne(t, arhiva, "tocka-a", "oborina", 3, func(d int64) float64 { return float64(d + 1) })
	upisiDnevne(t, arhiva, "tocka-0", "oborina", 3, func(int64) float64 { return 100 })
	tocke := []OborinskaTocka{{Code: "tocka-a", Sliv: "A", Tezina: 1}, {Code: "tocka-0", Sliv: "A", Tezina: 0}}
	got, err := DnevneOborine(arhiva, tocke)
	if want := map[string]DnevniNiz{OborinaKljuc("A"): {0: 1, 1: 2, 2: 3}}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("kiša %v, %v; želim %v — točka bez težine ne ulazi", got, err, want)
	}

	t.Run("vrijednost nije broj", func(t *testing.T) {
		db, _ := arhivaUDatoteci(t)
		upisiDnevne(t, db, "tocka-a", "oborina", 3, func(d int64) float64 { return 1 })
		if _, err := db.Exec(`UPDATE spoj SET vrijednost = 'x' WHERE vrijeme = 86400`); err != nil {
			t.Fatal(err)
		}
		if got, err := DnevneOborine(db, tocke[:1]); err == nil {
			t.Errorf("bez greške: %v", got)
		}
	})
	t.Run("upit pukne usred čitanja", func(t *testing.T) {
		db, _ := arhivaUDatoteci(t)
		upisiDnevne(t, db, "tocka-a", "oborina", 3, func(d int64) float64 { return 1 })
		zamijeniStupac(t, db, "spoj", "vrijednost", pucaNa("vrijeme = 2*86400", "vrijednost"), "letva", "vrijeme")
		if got, err := DnevneOborine(db, tocke[:1]); err == nil {
			t.Errorf("bez greške: %v", got)
		}
	})
}

// zagrebackiDan je redni broj dana (od epohe) zadanog datuma, kako ga
// DnevniIzArhive broji.
func zagrebackiDan(g int, mj time.Month, d int) int64 {
	return time.Date(g, mj, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// Dan kojeg u dnevnom nizu nema dobiva srednjak satnih po zagrebačkom danu,
// ali samo s barem 18 sati; dan iz dnevnog niza satni ne diraju. Redak
// koji se ne da pročitati i upit koji pukne su greška.
func TestDnevniIzArhiveDopunjujeIzSatnih(t *testing.T) {
	arhiva, _ := arhivaUDatoteci(t)
	upis := func(korak string, kad time.Time, v float64) {
		t.Helper()
		if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('proba', 'vodostaj', ?, ?, ?)`, korak, kad.Unix(), v); err != nil {
			t.Fatal(err)
		}
	}
	// 10. 1. 2026. dnevni zapis (ponoć po lokalnom), uz satne koji ne smiju
	// ništa promijeniti.
	upis("dnevni", time.Date(2026, 1, 10, 0, 0, 0, 0, models.Zagreb), 150)
	for h := 0; h < 24; h++ {
		upis("satni", time.Date(2026, 1, 10, h, 0, 0, 0, models.Zagreb), 999)
	}
	// 11. 1. samo satni, svih 24 sata: 100, 101, … 123 → srednjak 111,5.
	for h := 0; h < 24; h++ {
		upis("satni", time.Date(2026, 1, 11, h, 0, 0, 0, models.Zagreb), 100+float64(h))
	}
	// 12. 7. (ljetno vrijeme) 18 sati po 80 cm — dovoljno; 13. 7. 17 sati — premalo.
	for h := 0; h < 18; h++ {
		upis("satni", time.Date(2026, 7, 12, h, 0, 0, 0, models.Zagreb), 80)
		if h < 17 {
			upis("satni", time.Date(2026, 7, 13, h, 0, 0, 0, models.Zagreb), 70)
		}
	}
	n, err := DnevniIzArhive(arhiva, "proba")
	if err != nil {
		t.Fatal(err)
	}
	want := DnevniNiz{zagrebackiDan(2026, 1, 10): 150, zagrebackiDan(2026, 1, 11): 111.5, zagrebackiDan(2026, 7, 12): 80}
	if !reflect.DeepEqual(n, want) {
		t.Errorf("dnevni niz %v, želim %v", n, want)
	}

	for _, s := range []struct {
		ime     string
		pokvari string
		pukne   string
	}{
		{ime: "dnevna vrijednost nije broj", pokvari: `UPDATE spoj SET vrijednost = 'x' WHERE korak = 'dnevni'`},
		{ime: "satna vrijednost nije broj", pokvari: `UPDATE spoj SET vrijednost = 'x' WHERE korak = 'satni' AND vrijednost = 999`},
		{ime: "dnevni upit pukne", pukne: "korak = 'dnevni' AND vrijeme = 86400"},
		{ime: "satni upit pukne odmah", pukne: "korak = 'satni' AND vrijednost = 999"},
		{ime: "satni upit pukne usred čitanja", pukne: "korak = 'satni' AND vrijednost = 123"},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db, _ := arhivaUDatoteci(t)
			for _, r := range [][3]any{{"dnevni", 0, 150.0}, {"dnevni", 86400, 151.0}, {"satni", 3 * 86400, 999.0}, {"satni", 3*86400 + 3600, 123.0}, {"satni", 3*86400 + 7200, 124.0}} {
				if _, err := db.Exec(`INSERT INTO spoj VALUES ('proba', 'vodostaj', ?, ?, ?)`, r[0], r[1], r[2]); err != nil {
					t.Fatal(err)
				}
			}
			if s.pokvari != "" {
				if _, err := db.Exec(s.pokvari); err != nil {
					t.Fatal(err)
				}
			} else {
				zamijeniStupac(t, db, "spoj", "vrijednost", pucaNa(s.pukne, "vrijednost"), "letva", "korak", "vrijeme")
			}
			if n, err := DnevniIzArhive(db, "proba"); err == nil {
				t.Errorf("bez greške: %v", n)
			}
		})
	}
}

// kisnaRijeka su dnevni nizovi izmišljene rijeke s kišom međusliva K koja
// pada svaki dan različito.
func kisnaRijeka(dana int64) (DnevniCilj, map[string]DnevniNiz) {
	c := DnevniCilj{Letva: "cilj", Ulazi: []string{"ulaz"}, Slivovi: []string{"K"}}
	n := map[string]DnevniNiz{"cilj": {}, "ulaz": {}, OborinaKljuc("K"): {}}
	kisa := func(d int64) float64 {
		return math.Max(0, 4+5*math.Sin(float64(d)*0.7)+3*math.Sin(float64(d)*1.9))
	}
	for d := int64(0); d < dana; d++ {
		n["ulaz"][d] = valIzmisljeni(d)
		n["cilj"][d] = valIzmisljeni(d-2) + 20 + 2*kisa(d-1)
		n[OborinaKljuc("K")][d] = kisa(d)
	}
	return c, n
}

// Uz DnevniRezimKise kišni dani dobivaju svoju regresiju: prag je kvantil
// zbroja kiše, a režim nastaje samo kad je prag iznad nule. Kad kišnih dana
// ima premalo za ijedan doseg, model se ne namješta.
func TestNamjestiDnevniKisniRezim(t *testing.T) {
	c, nizovi := kisnaRijeka(3500)

	postavi(t, &DnevniRezimKise, 0.9)
	m, err := NamjestiDnevni(nizovi, c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.pragKise <= 0 {
		t.Fatalf("prag kiše %g", m.pragKise)
	}
	kisnih := 0
	for _, u := range m.uzorci {
		if m.zbrojKise(u.x) >= m.pragKise {
			kisnih++
		}
	}
	if udio := float64(kisnih) / float64(len(m.uzorci)); udio < 0.09 || udio > 0.11 {
		t.Errorf("kišnih dana %.3f, želim desetinu", udio)
	}
	for k := 1; k <= DnevniDosezi; k++ {
		if m.koef[k][2] == nil {
			t.Errorf("%d. dan nema regresije kišnog režima", k)
		}
	}

	postavi(t, &DnevniRezimKise, 0.97)
	if m, err := NamjestiDnevni(nizovi, c, 0); err == nil || !strings.Contains(err.Error(), "cilj: premalo dana za 1. dan (režim 2)") {
		t.Errorf("premalo kišnih dana: %v, model %v", err, m != nil)
	}

	// Kiša koja gotovo nikad ne pada: kvantil je nula, trećeg režima nema.
	suho := map[string]DnevniNiz{"cilj": nizovi["cilj"], "ulaz": nizovi["ulaz"], OborinaKljuc("K"): {}}
	for d := range nizovi[OborinaKljuc("K")] {
		suho[OborinaKljuc("K")][d] = 0
		if d%50 == 0 {
			suho[OborinaKljuc("K")][d] = 10
		}
	}
	postavi(t, &DnevniRezimKise, 0.5)
	m, err = NamjestiDnevni(suho, c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.pragKise != 0 || m.koef[1][2] != nil {
		t.Errorf("suha rijeka: prag kiše %g, kišna regresija %v", m.pragKise, m.koef[1][2])
	}
}

// uzorakDnevni je dan učenja sa zadanim značajkama i istom promjenom za
// svaki doseg; bez je doseg za koji promjene nema.
func uzorakDnevni(x []float64, y float64, bez ...int) dnevniUzorak {
	u := dnevniUzorak{x: x}
	for k := 1; k <= DnevniDosezi; k++ {
		u.y[k], u.ok[k] = y, true
	}
	for _, k := range bez {
		u.ok[k] = false
	}
	return u
}

// Analogije: dan bez promjene za doseg preskače se i uzima sljedeći
// najsličniji; težinske analogije važu promjenu obrnuto udaljenosti.
func TestPrognozirajAnalogije(t *testing.T) {
	postavi(t, &DnevniUdioRegresije, 0.0)
	postavi(t, &DnevnihAnalogija, 2)
	m := &DnevniModel{Cilj: DnevniCilj{Letva: "c"}, prag: math.Inf(1), sd: []float64{0, 1, 1, 1},
		uzorci: []dnevniUzorak{
			uzorakDnevni([]float64{1, 100, 0, 0}, 10),
			uzorakDnevni([]float64{1, 101, 0, 0}, 20, 2), // drugi dan nema promjene
			uzorakDnevni([]float64{1, 110, 0, 0}, 40),
		}}
	x := []float64{1, 100, 0, 0}
	p, r := m.Prognoziraj(x)
	// 1. dan: 10 i 20; 2. dan: 10 i 40, jer drugi najbliži dan promjene nema.
	if p[1] != 15 || math.Abs(r[1]-1.04*5) > 1e-9 || p[2] != 25 || math.Abs(r[2]-1.04*15) > 1e-9 {
		t.Errorf("obične analogije: promjena %v, raspon %v", p, r)
	}

	postavi(t, &DnevneAnalogijeTezinske, true)
	p, _ = m.Prognoziraj(x)
	w1, w2, w3 := 2.0, 1/(math.Sqrt(2)+0.5), 1/(math.Sqrt(200)+0.5)
	if want := (w1*10 + w2*20) / (w1 + w2); math.Abs(p[1]-want) > 1e-9 {
		t.Errorf("težinske, 1. dan: %g, želim %g", p[1], want)
	}
	if want := (w1*10 + w3*40) / (w1 + w3); math.Abs(p[2]-want) > 1e-9 {
		t.Errorf("težinske, 2. dan: %g, želim %g", p[2], want)
	}
}

// Kiša u udaljenosti analogija ima težinu OborinaTezinaKNN, a kvadrati kiše
// (DnevneKvadratneKise) u udaljenost ne ulaze.
func TestPrognozirajTezinaKiseUAnalogijama(t *testing.T) {
	postavi(t, &DnevniUdioRegresije, 0.0)
	postavi(t, &DnevnihAnalogija, 1)
	c := DnevniCilj{Letva: "c", Slivovi: []string{"S"}}
	// jedinica, razina, dvije promjene, šest značajki kiše
	znacajke := func(razina, kisa float64) []float64 {
		return []float64{1, razina, 0, 0, kisa, kisa, kisa, kisa, kisa, kisa}
	}
	sd := make([]float64, 10)
	for i := range sd {
		sd[i] = 1
	}
	m := &DnevniModel{Cilj: c, prag: math.Inf(1), sd: sd, uzorci: []dnevniUzorak{
		uzorakDnevni(znacajke(100, 0), 10), // ista razina, bez kiše
		uzorakDnevni(znacajke(103, 5), 50), // malo viša razina, ista kiša
	}}
	x := znacajke(100, 5)
	for _, s := range []struct {
		tezina float64
		want   float64
	}{{0, 10}, {0.5, 50}, {1, 50}} {
		postavi(t, &OborinaTezinaKNN, s.tezina)
		if p, _ := m.Prognoziraj(x); p[1] != s.want {
			t.Errorf("težina kiše %g: promjena %g, želim %g", s.tezina, p[1], s.want)
		}
	}

	// S kvadratima kiše značajki je više, ali udaljenost ih ne gleda: dan
	// iste kiše ostaje najbliži iako su mu kvadrati daleko.
	postavi(t, &DnevneKvadratneKise, true)
	postavi(t, &OborinaTezinaKNN, 1.0)
	sKvadratima := func(x []float64, kv float64) []float64 {
		return append(append([]float64(nil), x...), kv, kv, kv, kv, kv, kv)
	}
	m.sd = append(sd, 1, 1, 1, 1, 1, 1)
	m.uzorci = []dnevniUzorak{
		uzorakDnevni(sKvadratima(znacajke(100, 0), 0.25), 10),
		uzorakDnevni(sKvadratima(znacajke(103, 5), 1000), 50),
	}
	if m.kvadratiOd() != 10 {
		t.Fatalf("kvadrati od %d, želim 10", m.kvadratiOd())
	}
	if p, _ := m.Prognoziraj(sKvadratima(x, 0.25)); p[1] != 50 {
		t.Errorf("s kvadratima: promjena %g, želim 50 — kvadrati ne smiju ući u udaljenost", p[1])
	}
}
