package snijeg

import (
	"errors"
	"math"
	"testing"
	"time"

	"gocop/internal/prognoza"
)

// Brojevi u testovima su izmišljeni.

// Za ovo doba: uspoređuje se samo ±7 dana oko istog dana ranijih godina,
// tekuća sezona ne ulazi, a ispod 50 mm snijeg nikad nije „puno”.
func TestPunoZaDoba(t *testing.T) {
	povijest := map[int64]float64{}
	for g := 1961; g <= 2025; g++ {
		sredina := dan(g, 11, 22)
		for d := sredina - 7; d <= sredina+7; d++ {
			povijest[d] = float64(10 + g - 1961) // 10…74 mm
		}
		povijest[sredina-8], povijest[sredina+8] = 1000, 1000 // izvan prozora
	}
	for d := dan(2026, 11, 15); d <= dan(2026, 11, 29); d++ {
		povijest[d] = 1000 // tekuća sezona
	}
	danas := dan(2026, 11, 22)
	// 70 mm: manje je 60 od 65 godina (0,923)
	if udio, n := UdioIspodZaDoba(povijest, danas, 70); n != 65*15 || math.Abs(udio-60.0/65) > 1e-9 {
		t.Errorf("udio %v od %d", udio, n)
	}
	if !PunoZaDoba(povijest, danas, 70) {
		t.Error("70 mm nije puno za ovo doba")
	}
	// 68 mm: manje je 58 od 65 (0,892)
	if PunoZaDoba(povijest, danas, 68) {
		t.Error("68 mm je puno za ovo doba")
	}
	// isti udio, ali ispod 50 mm
	niske := map[int64]float64{}
	for d, v := range povijest {
		niske[d] = v / 2
	}
	if udio, _ := UdioIspodZaDoba(niske, danas, 35); udio < 0.9 || PunoZaDoba(niske, danas, 35) {
		t.Errorf("35 mm (udio %v) je puno za ovo doba", udio)
	}
	// premalo godina
	if PunoZaDoba(map[int64]float64{dan(2020, 11, 22): 1}, danas, 300) {
		t.Error("puno bez povijesti")
	}
	if udio, n := UdioIspodZaDoba(nil, danas, 1); udio != 0 || n != 0 {
		t.Error("prazna povijest")
	}
}

// nizGodina: 1 mm na dan u godinama od–do, a od 1. 6. pet dana po
// (godina − 1990) mm, pa je najveći zbroj pet dana 5·(godina − 1990).
func nizGodina(od, do int) prognoza.DnevniNiz {
	n := prognoza.DnevniNiz{}
	for g := od; g <= do; g++ {
		for d := dan(g, 1, 1); d < dan(g+1, 1, 1); d++ {
			n[d] = 1
		}
		for d := dan(g, 6, 1); d < dan(g, 6, 6); d++ {
			n[d] = float64(g - 1990)
		}
	}
	return n
}

// Godišnji maksimumi zbroja pet dana i razine za 2, 5 i 10 godina; godina
// s premalo dana i godina od doGodine ne ulaze.
func TestRazineVode(t *testing.T) {
	n := nizGodina(2000, 2025)
	for d := dan(1999, 9, 1); d < dan(1999, 9, 20); d++ {
		n[d] = 500 // nepuna godina
	}
	maks := GodisnjiMaksimumi(n, 2025)
	if len(maks) != 25 {
		t.Fatalf("%d maksimuma: %v", len(maks), maks)
	}
	// 25 godina, 50…170: k=12,5 → 112,5; k=5 → 150; k=2,5 → 162,5
	r, ima := RazineVodeIz(maks)
	if !ima || r != (RazineVode{113, 150, 163}) {
		t.Errorf("razine %v", r)
	}
	if _, ima := RazineVodeIz(maks[:19]); ima {
		t.Error("razine iz 19 godina")
	}
}

// Voda rijeke je srednjak njezinih slivova po površini, samo u danima koje
// imaju svi.
func TestVodaRijeke(t *testing.T) {
	nizovi := map[string]prognoza.DnevniNiz{
		prognoza.OborinaKljuc("A"): {1: 10, 2: 20},
		prognoza.OborinaKljuc("B"): {1: 30},
		prognoza.OborinaKljuc("K"): {1: 5},
	}
	v := VodaRijeke(nizovi, "Drava")
	if len(v) != 1 || math.Abs(v[1]-(14649*10+13308*30)/(14649.0+13308)) > 1e-9 {
		t.Errorf("Drava %v", v)
	}
	if v := VodaRijeke(nizovi, "Dunav"); len(v) != 0 {
		t.Errorf("Dunav bez L %v", v)
	}
	if v := VodaRijeke(nizovi, "Sava"); len(v) != 0 {
		t.Errorf("Sava %v", v)
	}
}

// vodaDana je niz od pet dana unaprijed s istom vrijednošću u slivovima.
func vodaDana(v float64, slivovi ...string) map[string]prognoza.DnevniNiz {
	out := map[string]prognoza.DnevniNiz{}
	for _, s := range slivovi {
		n := prognoza.DnevniNiz{}
		for d := int64(1); d <= ProcjenaDana; d++ {
			n[d] = v
		}
		out[prognoza.OborinaKljuc(s)] = n
	}
	return out
}

// Upozorenje: voda iznad razine za 2 godine, s otopljenim snijegom i
// najrjeđom dosegnutom razinom; ispod razine, bez dana ili bez razina ga
// nema.
func TestAlarmIz(t *testing.T) {
	r := RazineVode{70, 80, 90}
	a := AlarmIz("Drava", vodaDana(16.4, "A", "B"), vodaDana(6, "A", "B"), r)
	if a == nil || a.Rijeka != "Drava" || math.Abs(a.Mm-82) > 1e-9 || math.Abs(a.Topljenje-52) > 1e-9 || a.Godina != 5 {
		t.Errorf("Drava %+v", a)
	}
	if a := AlarmIz("Drava", vodaDana(20, "A", "B"), nil, r); a == nil || a.Godina != 10 || a.Topljenje != 0 {
		t.Errorf("bez kiše %+v", a)
	}
	if a := AlarmIz("Drava", vodaDana(13.9, "A", "B"), nil, r); a != nil {
		t.Errorf("ispod razine %+v", a)
	}
	voda := vodaDana(30, "A", "B")
	delete(voda[prognoza.OborinaKljuc("B")], 5)
	if a := AlarmIz("Drava", voda, nil, r); a != nil {
		t.Errorf("bez petog dana %+v", a)
	}
	if a := AlarmIz("Drava", vodaDana(30, "A", "B"), nil, RazineVode{}); a != nil {
		t.Errorf("bez razina %+v", a)
	}
}

// probnaVoda daje izvoru vodu idućih dana i povijest iz koje su razine
// Drave i Dunava 113, 150 i 163 mm.
type probnaVoda struct {
	voda, kisa      map[string]prognoza.DnevniNiz
	greskaVode      error
	greskaPovijesti error
	povijesti       int
}

func (v *probnaVoda) dodaj(iz *Izvor) {
	iz.Voda = func(sada int64) (map[string]prognoza.DnevniNiz, map[string]prognoza.DnevniNiz, error) {
		return v.voda, v.kisa, v.greskaVode
	}
	iz.PovijestVode = func() (map[string]prognoza.DnevniNiz, error) {
		v.povijesti++
		out := map[string]prognoza.DnevniNiz{}
		for _, s := range Oznake() {
			out[prognoza.OborinaKljuc(s)] = nizGodina(2000, 2024)
		}
		return out, v.greskaPovijesti
	}
}

// Na izvoru: Gornja Drava ima puno snijega za ovo doba (220 mm, više od 60
// od 65 godina), pa Drava dobije upozorenje kad voda prijeđe 113 mm; Dunav
// ne, iako i on ima puno vode. Razine se računaju jednom na dan.
func TestSlivoviUpozorenje(t *testing.T) {
	p := noviProbniIzvor()
	iz := p.izvor()
	v := &probnaVoda{voda: vodaDana(32, "A", "B", "K", "L"), kisa: vodaDana(20, "A", "B", "K", "L")}
	v.dodaj(iz)
	sl, err := iz.Slivovi()
	if err != nil {
		t.Fatal(err)
	}
	if !sl[0].PunoZaDoba || sl[3].PunoZaDoba {
		t.Errorf("za ovo doba: A %v, L %v", sl[0].PunoZaDoba, sl[3].PunoZaDoba)
	}
	a := sl[0].Alarm
	if a == nil || sl[1].Alarm != a || a.Rijeka != "Drava" || math.Abs(a.Mm-160) > 1e-9 || math.Abs(a.Topljenje-60) > 1e-9 || a.Godina != 5 {
		t.Fatalf("Drava %+v", a)
	}
	if sl[2].Alarm != nil || sl[3].Alarm != nil {
		t.Errorf("Dunav ima upozorenje %+v", sl[2].Alarm)
	}
	iz.Zaboravi()
	if _, err := iz.Slivovi(); err != nil || v.povijesti != 1 {
		t.Errorf("povijest vode čitana %d puta: %v", v.povijesti, err)
	}

	// voda ispod razine za 2 godine
	iz.Zaboravi()
	v.voda = vodaDana(22, "A", "B", "K", "L")
	if sl, err := iz.Slivovi(); err != nil || sl[0].Alarm != nil {
		t.Errorf("ispod razine %+v %v", sl[0].Alarm, err)
	}
	// prognoze nema
	iz.Zaboravi()
	v.voda = map[string]prognoza.DnevniNiz{}
	if sl, err := iz.Slivovi(); err != nil || sl[0].Alarm != nil {
		t.Errorf("bez prognoze %+v %v", sl[0].Alarm, err)
	}
	// voda se ne da pročitati: kartica ostaje, upozorenja nema
	iz.Zaboravi()
	v.voda, v.greskaVode = vodaDana(32, "A", "B"), errors.New("baza oborina")
	if sl, err := iz.Slivovi(); err != nil || sl[0].Alarm != nil {
		t.Errorf("greška vode: %v %+v", err, sl[0].Alarm)
	}
}

// SNOWGRID kasni više od tri dana ili se povijest vode ne da pročitati:
// upozorenja nema.
func TestSlivoviUpozorenjeBez(t *testing.T) {
	p := noviProbniIzvor()
	iz := p.izvor()
	v := &probnaVoda{voda: vodaDana(32, "A", "B"), kisa: vodaDana(20, "A", "B")}
	v.dodaj(iz)
	iz.Sad = func() time.Time { return time.Unix((danas+3)*86400, 0) }
	sl, err := iz.Slivovi()
	if err != nil || !sl[0].Kasni(iz.sad()) || !sl[0].PunoZaDoba || sl[0].Alarm != nil || v.povijesti != 0 {
		t.Errorf("star SNOWGRID: %v %+v", err, sl[0].Alarm)
	}
	p = noviProbniIzvor()
	iz = p.izvor()
	v = &probnaVoda{voda: vodaDana(32, "A", "B"), greskaPovijesti: errors.New("arhiva")}
	v.dodaj(iz)
	if sl, err := iz.Slivovi(); err != nil || sl[0].Alarm != nil {
		t.Errorf("bez arhive: %v %+v", err, sl[0].Alarm)
	}
	// bez izvora vode
	p = noviProbniIzvor()
	if sl, err := p.izvor().Slivovi(); err != nil || sl[0].Alarm != nil || !sl[0].PunoZaDoba {
		t.Errorf("bez izvora vode: %v", err)
	}
}

// Voda iz oborina: kiša 12 mm na dan i 12 mm snijega, a pri 5 °C se iz
// zalihe otopi 15 mm na dan; voda je 27 mm na dan, kiša 12.
func TestVodaIzOborina(t *testing.T) {
	var greskaSatnih, greskaSnijega error
	iz := &prognoza.OborinskiIzvor{
		Tocke: []prognoza.OborinskaTocka{{Code: "t-a", Sliv: "A", Tezina: 1}},
		Satne: func(od, do int64) (map[string]map[int64]float64, error) {
			n := map[int64]float64{}
			for h := od; h <= do; h++ {
				n[h] = 1
			}
			return map[string]map[int64]float64{"t-a": n}, greskaSatnih
		},
		Snijeg: func(od, do int64) (map[string]map[int64]float64, map[string]map[int64]float64, error) {
			sn, te := map[int64]float64{}, map[int64]float64{}
			for h := od; h <= do; h++ {
				sn[h], te[h] = 0.5, 5
			}
			return map[string]map[int64]float64{"t-a": sn}, map[string]map[int64]float64{"t-a": te}, greskaSnijega
		},
		ZalihaSnijega: func(string, int64) (float64, error) { return 100, nil },
		Kisomjeri:     []prognoza.OborinskaTocka{{Code: "k", SlivMjesta: "A"}},
	}
	sada := danas*24 + 12
	voda, kisa, err := VodaIzOborina(iz, sada)
	if err != nil {
		t.Fatal(err)
	}
	a := prognoza.OborinaKljuc("A")
	for d := int64(1); d <= ProcjenaDana; d++ {
		if math.Abs(voda[a][d]-27) > 1e-9 || math.Abs(kisa[a][d]-12) > 1e-9 {
			t.Errorf("dan %d: voda %v, kiša %v", d, voda[a][d], kisa[a][d])
		}
	}
	if len(voda[a]) != ProcjenaDana || len(voda) != 1 {
		t.Errorf("voda %v", voda)
	}
	if v, k, err := VodaIzOborina(nil, sada); v != nil || k != nil || err != nil {
		t.Error("bez izvora")
	}
	// bez snijega kiša je oborina kakva je pala
	bez := iz.BezSnijega()
	if _, k, err := VodaIzOborina(bez, sada); err != nil || k[a][1] != 24 {
		t.Errorf("bez snijega: %v %v", k[a][1], err)
	}
	// greške
	greskaSatnih = errors.New("satne")
	if _, _, err := VodaIzOborina(iz, sada); err == nil {
		t.Error("greška satnih")
	}
	greskaSatnih, greskaSnijega = nil, errors.New("snijeg")
	if _, _, err := VodaIzOborina(iz, sada); err == nil {
		t.Error("greška snijega")
	}
}
