package prognoza

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// niz slaže sate od..do s centimetrima iz f.
func nizSati(od, do int64, f func(t int64) int64) map[int64]sirovoOcitanje {
	n := map[int64]sirovoOcitanje{}
	for t := od; t <= do; t++ {
		n[t] = cmU(f(t))
	}
	return n
}

func stalno(v int64) func(int64) int64 { return func(int64) int64 { return v } }

func imenaSusjeda(r RekonstruiranaRupa) []string {
	var out []string
	for _, s := range r.Iz {
		out = append(out, s.Letva)
	}
	return out
}

// Rupa s poznatim krajevima: pravac letve i odstupanje susjeda od njegova
// pravca, u satu pomaknutom za zastoj (nizvodni susjed: 5 h kasnije).
func TestRekonstruirajProsluRupu(t *testing.T) {
	letva := nizSati(0, 99, stalno(100))
	for h := int64(41); h <= 62; h++ { // 22 sata bez mjerenja, krajevi 40 i 63
		delete(letva, h)
	}
	dolje := nizSati(0, 120, func(h int64) int64 {
		if h >= 50 && h <= 55 {
			return 60 // val prolazi nizvodnom letvom 5 h poslije
		}
		return 50
	})
	susjedi := []Susjed{{Letva: "dolje", Zastoj: -5, Prosla: 2}, {Letva: "gore", Prosla: 5, Traje: 6}}
	rupe := Rekonstruiraj(letva, susjedi, map[string]map[int64]sirovoOcitanje{"dolje": dolje, "gore": nizSati(0, 99, stalno(7))})
	if len(rupe) != 1 {
		t.Fatalf("rupe: %+v", rupe)
	}
	r := rupe[0]
	if r.Zadnje != 40 || r.Sljedece != 63 || r.Sati != 22 || r.Do != 62 || r.Tocnost != 2 || !slices.Equal(imenaSusjeda(r), []string{"dolje"}) {
		t.Errorf("rupa: %+v", r)
	}
	for h, want := range map[int64]int64{44: 100, 45: 110, 50: 110, 51: 100, 62: 100} {
		if v := letva[h].cm.Int64; v != want {
			t.Errorf("sat %d = %d, želim %d", h, v, want)
		}
	}
	if letva[40].cm.Int64 != 100 || letva[63].cm.Int64 != 100 {
		t.Error("vlastito mjerenje se ne smije dirati")
	}
}

// Sat koji prvi susjed ne može dati daje drugi; napomena navodi oba, a
// točnost je slabija od njihovih.
func TestRekonstruirajDrugiSusjedPoSatima(t *testing.T) {
	letva := nizSati(0, 60, stalno(100))
	for h := int64(21); h <= 40; h++ {
		delete(letva, h)
	}
	prvi := nizSati(0, 60, stalno(10))
	delete(prvi, 30) // bez ovog sata prvi ne može dati sat 30
	drugi := nizSati(0, 60, stalno(20))
	drugi[30] = cmU(25)
	rupe := Rekonstruiraj(letva, []Susjed{{Letva: "prvi", Prosla: 2}, {Letva: "drugi", Prosla: 5}},
		map[string]map[int64]sirovoOcitanje{"prvi": prvi, "drugi": drugi})
	if len(rupe) != 1 || rupe[0].Sati != 20 || rupe[0].Tocnost != 5 || !slices.Equal(imenaSusjeda(rupe[0]), []string{"prvi", "drugi"}) {
		t.Fatalf("rupe: %+v", rupe)
	}
	if v := letva[30].cm.Int64; v != 105 {
		t.Errorf("sat 30 od drugog susjeda = %d, želim 105", v)
	}
}

// Kratku rupu prognoza premosti sama, preduga ostaje rupa, a krajevi bez
// centimetara (samo protok) ne daju pravac.
func TestRekonstruirajGraniceProsleRupe(t *testing.T) {
	susjed := map[string]map[int64]sirovoOcitanje{"s": nizSati(0, 200, stalno(10))}
	for _, c := range []struct {
		ime      string
		od, do   int64
		samoQ    bool
		popunjen bool
	}{
		{"6 h", 11, 16, false, false},
		{"7 h", 11, 17, false, true},
		{"48 h", 11, 58, false, true},
		{"49 h", 11, 59, false, false},
		{"kraj samo u protoku", 11, 30, true, false},
	} {
		letva := nizSati(0, 100, stalno(50))
		for h := c.od; h <= c.do; h++ {
			delete(letva, h)
		}
		if c.samoQ {
			letva[c.do+1] = sirovoOcitanje{q: sql.NullFloat64{Float64: 300, Valid: true}}
		}
		rupe := Rekonstruiraj(letva, []Susjed{{Letva: "s", Prosla: 3}}, susjed)
		if (len(rupe) > 0) != c.popunjen {
			t.Errorf("%s: rupe %+v", c.ime, rupe)
		}
	}
}

// Rupa koja traje: zadnje mjerenje i kretanje uzvodnog susjeda od tada,
// najviše 24 h i nikad iza susjedova zadnjeg mjerenja.
func TestRekonstruirajRupuKojaTraje(t *testing.T) {
	gore := func(do int64) map[int64]sirovoOcitanje {
		return nizSati(0, do, func(h int64) int64 { return 50 + max(0, h-96) })
	}
	susjedi := []Susjed{{Letva: "dolje", Zastoj: -2, Prosla: 1, Traje: 1}, {Letva: "bez", Prosla: 1}, {Letva: "gore", Zastoj: 4, Prosla: 3, Traje: 6}}
	nizovi := func(do int64) map[string]map[int64]sirovoOcitanje {
		return map[string]map[int64]sirovoOcitanje{"gore": gore(do), "dolje": nizSati(0, 300, stalno(1)), "bez": nizSati(0, 300, stalno(1))}
	}

	letva := nizSati(0, 100, stalno(100))
	rupe := Rekonstruiraj(letva, susjedi, nizovi(120))
	if len(rupe) != 1 {
		t.Fatalf("rupe: %+v", rupe)
	}
	if r := rupe[0]; r.Zadnje != 100 || r.Sljedece != 0 || r.Sati != 20 || r.Do != 120 || r.Tocnost != 6 || !slices.Equal(imenaSusjeda(r), []string{"gore"}) {
		t.Errorf("rupa: %+v", r)
	}
	if v := letva[110].cm.Int64; v != 110 {
		t.Errorf("sat 110 = %d, želim 110", v)
	}
	if _, ima := letva[121]; ima {
		t.Error("popunjen sat koji susjed još nije izmjerio")
	}

	letva = nizSati(0, 100, stalno(100))
	if rupe := Rekonstruiraj(letva, susjedi, nizovi(300)); len(rupe) != 1 || rupe[0].Do != 124 {
		t.Errorf("najviše 24 h: %+v", rupe)
	}
	letva = nizSati(0, 100, stalno(100))
	if rupe := Rekonstruiraj(letva, susjedi, nizovi(106)); len(rupe) != 0 || len(letva) != 101 {
		t.Errorf("šest sati se ne popunjava: %+v", rupe)
	}
	letva = nizSati(0, 100, stalno(100))
	letva[100] = sirovoOcitanje{q: sql.NullFloat64{Float64: 300, Valid: true}}
	if rupe := Rekonstruiraj(letva, susjedi, nizovi(120)); len(rupe) != 0 {
		t.Errorf("zadnje mjerenje samo u protoku: %+v", rupe)
	}
	if rupe := Rekonstruiraj(map[int64]sirovoOcitanje{}, susjedi, nizovi(120)); len(rupe) != 0 {
		t.Errorf("letva bez mjerenja: %+v", rupe)
	}
}

// Susjed koji stoji dok se letva miče rupu ne popunjava; dok i letva stoji
// (Mohács na −30 cm), stajanje susjeda nije kvar.
func TestRekonstruirajPreskaceSusjedaKojiStoji(t *testing.T) {
	susjedi := []Susjed{{Letva: "stoji", Prosla: 1, Traje: 1}, {Letva: "mice", Prosla: 4, Traje: 4}}
	nizovi := map[string]map[int64]sirovoOcitanje{
		"stoji": nizSati(0, 230, stalno(15)),
		"mice":  nizSati(0, 230, func(h int64) int64 { return h / 4 }),
	}
	letva := nizSati(0, 200, func(h int64) int64 { return h / 4 })
	if rupe := Rekonstruiraj(letva, susjedi, nizovi); len(rupe) != 1 || !slices.Equal(imenaSusjeda(rupe[0]), []string{"mice"}) {
		t.Errorf("letva se miče: %+v", rupe)
	}
	letva = nizSati(0, 200, stalno(-30))
	if rupe := Rekonstruiraj(letva, susjedi, nizovi); len(rupe) != 1 || !slices.Equal(imenaSusjeda(rupe[0]), []string{"stoji"}) {
		t.Errorf("letva stoji: %+v", rupe)
	}
	if raspon(map[int64]sirovoOcitanje{}, 0, 10) != -1 {
		t.Error("raspon bez mjerenja")
	}
}

func TestOpisRekonstrukcije(t *testing.T) {
	sat := func(s string) int64 {
		k, err := time.ParseInLocation("2006-01-02 15", s, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		return k.Unix() / 3600
	}
	bezdan, dunaszekcso := Susjed{Letva: "bezdan", Zastoj: -5}, Susjed{Letva: "dunaszekcso"}
	for _, c := range []struct {
		r    RekonstruiranaRupa
		want string
	}{
		{RekonstruiranaRupa{Zadnje: sat("2026-10-07 09"), Sljedece: sat("2026-10-08 07"), Sati: 21, Iz: []Susjed{bezdan}, Tocnost: 2},
			"rupa od 7.10. u 11:00 do 8.10. u 09:00 (21 h) procijenjena iz letve bezdan 5 h nizvodno; na arhivi 95 % unutar ±2 cm"},
		{RekonstruiranaRupa{Zadnje: sat("2026-10-07 09"), Sljedece: sat("2026-10-08 07"), Sati: 15, Iz: []Susjed{bezdan, dunaszekcso}, Tocnost: 3},
			"rupa od 7.10. u 11:00 do 8.10. u 09:00 (15 od 21 h) procijenjena iz letvi bezdan 5 h nizvodno i dunaszekcso u istom satu; na arhivi 95 % unutar ±3 cm"},
		{RekonstruiranaRupa{Zadnje: sat("2026-10-11 02"), Sati: 10, Iz: []Susjed{{Letva: "barcs", Zastoj: 4}}, Tocnost: 10},
			"nema mjerenja od 11.10. u 04:00; 10 h procijenjeno iz kretanja letve barcs 4 h uzvodno; na arhivi 95 % unutar ±10 cm"},
	} {
		if got := c.r.Opis(); got != c.want {
			t.Errorf("\n got %q\nwant %q", got, c.want)
		}
	}
}

// bazaLetvi slaže bazu očitanja: šifra → sat od epohe → cm.
func bazaLetvi(t *testing.T, letve map[string]map[int64]int64) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE stations (id TEXT PRIMARY KEY, code TEXT);
		CREATE TABLE readings (station_id TEXT, measured_at DATETIME, level_cm INTEGER, flow_m3s REAL);`); err != nil {
		t.Fatal(err)
	}
	for kod, sati := range letve {
		if _, err := db.Exec(`INSERT INTO stations VALUES (?, ?)`, kod, kod); err != nil {
			t.Fatal(err)
		}
		for h, cm := range sati {
			if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES (?,?,?)`,
				kod, time.Unix(h*3600, 0).UTC(), cm); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}

func satiCm(od, do int64, f func(int64) int64) map[int64]int64 {
	out := map[int64]int64{}
	for h := od; h <= do; h++ {
		out[h] = f(h)
	}
	return out
}

// Niz za račun ima rekonstruirane sate, izmjereni nema; napomena stoji dok
// rupa seže u zadnja četiri dana, a susjedu sate popuni druga obala.
func TestRekonstrukcijaIzBaze(t *testing.T) {
	sada := time.Now().UTC().Truncate(time.Hour).Unix() / 3600
	postavi(t, &Rekonstrukcije, map[string][]Susjed{"szentborbas": {{Letva: "barcs", Zastoj: 4, Prosla: 8, Traje: 10}}})
	szentborbas := satiCm(sada-200, sada-10, stalno(-124))
	for h := sada - 100; h <= sada-80; h++ { // i jedna prošla rupa, od 21 h
		delete(szentborbas, h)
	}
	o := &Osvjezivac{Ocitanja: bazaLetvi(t, map[string]map[int64]int64{
		"szentborbas":    szentborbas,
		"barcs":          satiCm(sada-200, sada-3, func(h int64) int64 { return -194 + max(0, h-(sada-14)) }),
		"terezino-polje": satiCm(sada-200, sada, func(h int64) int64 { return -395 + max(0, h-(sada-14)) }),
	})}
	ctx := context.Background()
	od := time.Unix((sada-300)*3600, 0)
	iz := Izvor{Letva: "szentborbas", Velicina: "vodostaj"}
	racun, err := o.ucitajNiz(ctx, iz, od)
	if err != nil {
		t.Fatal(err)
	}
	if z, _ := racun.Zadnji(); z != sada {
		t.Errorf("rekonstruirani niz završava u %d, želim %d (Barcs do sada s druge obale)", z, sada)
	}
	if v, _ := racun.U(sada); v != -124+10 {
		t.Errorf("sat izdanja = %v, želim -114", v)
	}
	izmjereni, _ := o.ucitajIzmjereni(ctx, iz, od)
	if z, _ := izmjereni.Zadnji(); z != sada-10 {
		t.Errorf("izmjereni niz završava u %d, želim %d", z, sada-10)
	}

	opis, ima := o.rekonstrukcijaDo(ctx, "szentborbas", od, sada)
	if !ima || !strings.HasPrefix(opis, "rekonstrukcija: rupa od ") || !strings.Contains(opis, "(21 h) procijenjena iz letve barcs 4 h uzvodno; na arhivi 95 % unutar ±8 cm; nema mjerenja od ") ||
		!strings.HasSuffix(opis, "10 h procijenjeno iz kretanja letve barcs 4 h uzvodno; na arhivi 95 % unutar ±10 cm") {
		t.Errorf("napomena: %q %v", opis, ima)
	}
	if _, ima := o.rekonstrukcijaDo(ctx, "szentborbas", od, sada+RekonstrukcijaNapomena+20); ima {
		t.Error("napomena za rupu stariju od četiri dana")
	}
	if _, ima := o.rekonstrukcijaDo(ctx, "barcs", od, sada); ima {
		t.Error("napomena za letvu bez rekonstrukcije")
	}

	otkazan, odustani := context.WithCancel(ctx)
	odustani()
	if _, ima := o.rekonstrukcijaDo(otkazan, "szentborbas", od, sada); ima {
		t.Error("napomena bez baze")
	}
	sirovo := map[int64]sirovoOcitanje{sada - 30: cmU(1), sada - 10: cmU(1)}
	if rupe := o.rekonstruiraj(otkazan, "szentborbas", sirovo, od); len(rupe) != 0 {
		t.Errorf("bez susjeda iz baze: %+v", rupe)
	}
	o.dopuniDrugomObalom(otkazan, "barcs", sirovo, od)
	if len(sirovo) != 2 {
		t.Error("druga obala bez baze")
	}
}

// Krug osvježavanja: karika kojoj su susjedi popunili kraj niza ne javlja
// upozorenje bez mjerenja, nego napomenu odakle su sati.
func TestOsvjezavanjeSRekonstrukcijom(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	sada := time.Now().UTC().Truncate(time.Hour).Unix() / 3600
	postavi(t, &Rekonstrukcije, map[string][]Susjed{"gornja": {{Letva: "treca", Zastoj: 2, Prosla: 4, Traje: 6}}})
	o := probniOsvjezivac(t, bazaLetvi(t, map[string]map[int64]int64{
		"gornja": satiCm(sada-47, sada-10, func(h int64) int64 { return 100 + (sada - h) }),
		"donja":  satiCm(sada-47, sada, func(h int64) int64 { return 90 + (sada - h) }),
		"treca":  satiCm(sada-47, sada, func(h int64) int64 { return 70 + (sada - h) }),
	}))
	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Sada != sada {
		t.Errorf("izdano za %d, želim %d", ishod.Sada, sada)
	}
	if iz, ima := ishod.Izbor["rekonstrukcija:gornja"]; !ima || !strings.Contains(iz.Opis, "procijenjeno iz kretanja letve treca 2 h uzvodno") {
		t.Errorf("izbor: %+v", ishod.Izbor)
	}
	i := slices.IndexFunc(ishod.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaRekonstrukcija })
	if i < 0 || ishod.Upozorenja[i].Tezina != TezinaNapomena || ishod.Upozorenja[i].Letva != "gornja" {
		t.Errorf("upozorenja: %+v", ishod.Upozorenja)
	}
	if slices.ContainsFunc(ishod.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaUlaz }) {
		t.Errorf("karika s rekonstrukcijom javlja se kao bez mjerenja: %+v", ishod.Upozorenja)
	}
}
