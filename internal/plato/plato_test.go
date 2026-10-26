package plato

import (
	"math"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Sve letve i brojke su izmišljene. Nizovi su 20.–24. 10. 2026., prije
// pomaka sata, po zagrebačkom zidnom satu.

// sat je h sati od ponoći 20. 10. 2026. po zagrebačkom satu (time.Date
// sam prenese sate preko ponoći).
func sat(h int) time.Time { return time.Date(2026, 10, 20, h, 0, 0, 0, models.Zagreb) }

// niz su uzastopni satni vodostaji od sata h0.
func niz(h0 int, cm ...float64) []Tocka {
	out := make([]Tocka, len(cm))
	for i, v := range cm {
		out[i] = Tocka{Kad: sat(h0 + i), Cm: v}
	}
	return out
}

// isto je n puta ista vrijednost.
func isto(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// od je niz vrijednosti od a s korakom k, n njih.
func od(a, k float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = a + k*float64(i)
	}
	return out
}

func spoji(dijelovi ...[]float64) []float64 {
	var out []float64
	for _, d := range dijelovi {
		out = append(out, d...)
	}
	return out
}

// svojstva Donjeg Broda: niz iste vrijednosti normalno do 10 h, prag skoka
// 50 cm, daleko izvan raspona ispod −300 ili iznad 600 cm, najniže u
// arhivi 20 cm, niska voda do 60 cm, susjed Gornji Brod potvrđuje pomakom
// većim od 20 cm.
func svojstva() Svojstva {
	return Svojstva{Trajanje: 10 * time.Hour, Prag: 50, Raspon: &Raspon{Od: -300, Do: 600, Najniza: 20, Nisko: 60},
		Susjedi: []Susjed{{Letva: "gornji-brod", Prag: 20}}}
}

func imena(s string) string {
	return map[string]string{"gornji-brod": "Gornji Brod"}[s]
}

// Odluke (25. 10. 2026.): uređaj stoji na 412 cm 19 sati, a susjedna letva
// se u to vrijeme podigne 36 cm. To je kvar koji potvrđuje susjed, i traje.
func TestZaglavljenSPotvrdomSusjeda(t *testing.T) {
	letva := niz(0, spoji([]float64{404, 406, 408, 410}, isto(412, 20))...)
	susjed := niz(4, od(300, 2, 20)...)
	k := Prepoznaj(letva, svojstva(), map[string][]Tocka{"gornji-brod": susjed})
	if len(k) != 1 || k[0].Vrsta != Zaglavljen || !k[0].Traje || k[0].Susjed == nil || k[0].Skokom || k[0].Izvan {
		t.Fatalf("kvar %+v", k)
	}
	want := "mjerenje stoji na 412 cm od 20. 10. u 4 h, već 19 h, a susjedna letva Gornji Brod se u tom vremenu pomaknula za 36 cm"
	if got := Opis(k[0], imena); got != want {
		t.Errorf("opis\n %q\nželim\n %q", got, want)
	}
	if got := Recenica(k[0], imena); !strings.HasPrefix(got, "Mjerenje stoji") || !strings.HasSuffix(got, "36 cm.") {
		t.Errorf("rečenica %q", got)
	}
	if !k[0].U(sat(10)) || k[0].U(sat(3)) || k[0].Sati() != 19 {
		t.Errorf("trajanje %v–%v", k[0].Od, k[0].Do)
	}
}

// Mirna mala voda: letva stoji dan i pol, a susjed se pomiče nekoliko cm.
// Bez skoka, unutar raspona i bez pomaka susjeda nema uzbune; ni kad je
// niz kraći od najduljeg normalnog trajanja, a susjed se pomaknuo.
func TestMirnaVodaBezUzbune(t *testing.T) {
	letva := niz(0, isto(120, 36)...)
	mirno := niz(0, spoji(od(50, 0.2, 18), od(53.6, -0.2, 18))...)
	if k := Prepoznaj(letva, svojstva(), map[string][]Tocka{"gornji-brod": mirno}); len(k) != 0 {
		t.Errorf("mirna voda: %+v", k)
	}
	kratko := niz(0, spoji(od(100, 3, 5), isto(115, 9), od(118, 3, 5))...)
	dize := niz(0, od(300, 5, 19)...)
	if k := Prepoznaj(kratko, svojstva(), map[string][]Tocka{"gornji-brod": dize}); len(k) != 0 {
		t.Errorf("kratak niz: %+v", k)
	}
	// susjed bez dovoljno očitanja ne potvrđuje
	malo := niz(0, 300, 400)
	if k := Prepoznaj(letva, svojstva(), map[string][]Tocka{"gornji-brod": malo}); len(k) != 0 {
		t.Errorf("susjed s dva očitanja: %+v", k)
	}
}

// Radar na potoku (letva bez arhive i susjeda) skoči s 48 na −372 cm i
// ondje stoji: kvar ulaskom skokom. Kad je prije skoka i samo bio kvar (dva
// sata 145 i 127 cm na suhom potoku), povratak na 1 cm nije ulaz skokom.
func TestUlazSkokom(t *testing.T) {
	s := Svojstva{Trajanje: 12 * time.Hour, Prag: 100}
	k := Prepoznaj(niz(0, spoji(isto(48, 4), isto(-372, 14))...), s, nil)
	if len(k) != 1 || !k[0].Skokom || k[0].Prije != 48 || !k[0].Traje {
		t.Fatalf("kvar %+v", k)
	}
	want := "mjerenje stoji na −372 cm od 20. 10. u 4 h, već 13 h, a na tu vrijednost skočilo je s 48 cm"
	if got := Opis(k[0], imena); got != want {
		t.Errorf("opis %q", got)
	}
	if k := Prepoznaj(niz(0, spoji([]float64{1, 1, 145, 127}, isto(1, 14))...), s, nil); len(k) != 0 {
		t.Errorf("povratak iz kvara: %+v", k)
	}
	// skok nakon rupe dulje od dana ne zna se
	nakonRupe := append(niz(0, isto(48, 4)...), niz(40, isto(-372, 14)...)...)
	if k := Prepoznaj(nakonRupe, s, nil); len(k) != 0 {
		t.Errorf("nakon rupe: %+v", k)
	}
}

// Zaglavljena vrijednost daleko izvan raspona je kvar i bez susjeda; ona na
// najnižoj vrijednosti iz arhive nosi oznaku „na dnu”.
func TestZaglavljenIzvanRasponaINaDnu(t *testing.T) {
	s := svojstva()
	s.Susjedi = nil
	k := Prepoznaj(niz(0, spoji(od(870, 10, 4), isto(910, 14))...), s, nil)
	if len(k) != 1 || !k[0].Izvan {
		t.Fatalf("izvan: %+v", k)
	}
	if got := Opis(k[0], imena); !strings.HasSuffix(got, ", a to je daleko izvan uobičajenog raspona letve") {
		t.Errorf("opis %q", got)
	}
}

// Dno mjerila: niska zaglavljena vrijednost uz susjeda koji se od početka
// samo spušta (pomak potvrđuje kvar, ali voda je ispod dosega mjerila).
// Kad se susjed digne iznad razine s početka, to je kvar i na najnižoj
// vrijednosti iz arhive. Bez očitanja susjeda dno je moguće samo na
// najnižoj vrijednosti iz arhive.
func TestNaDnuPoSusjedu(t *testing.T) {
	s := svojstva()
	letva := niz(0, spoji(od(58, -2, 4), isto(50, 14))...)
	k := Prepoznaj(letva, s, map[string][]Tocka{"gornji-brod": niz(0, od(100, -3, 18)...)})
	if len(k) != 1 || !k[0].NaDnu || !k[0].DnoSusjed || k[0].Susjed == nil || k[0].BezPrognoze() {
		t.Fatalf("na dnu: %+v", k)
	}
	if got := Opis(k[0], imena); !strings.HasSuffix(got, "; nijedna susjedna letva se u tom vremenu nije digla iznad razine s početka, pa je voda vjerojatno ispod dosega mjernog uređaja") {
		t.Errorf("opis %q", got)
	}
	dize := niz(0, spoji(isto(100, 6), od(100, 3, 12))...)
	na20 := niz(0, spoji(od(28, -2, 4), isto(20, 14))...)
	if k := Prepoznaj(na20, s, map[string][]Tocka{"gornji-brod": dize}); len(k) != 1 || k[0].NaDnu || !k[0].BezPrognoze() {
		t.Errorf("susjed se digao: %+v", k)
	}
	visoko := niz(0, spoji(od(108, -2, 4), isto(100, 14))...)
	if k := Prepoznaj(visoko, s, map[string][]Tocka{"gornji-brod": niz(0, od(100, -3, 18)...)}); len(k) != 1 || k[0].NaDnu {
		t.Errorf("iznad niske vode: %+v", k)
	}
	s.Prag = 5 // u 20 cm se ušlo skokom, a susjed nema očitanja
	k = Prepoznaj(niz(0, spoji(isto(40, 4), isto(20, 14))...), s, nil)
	if len(k) != 1 || !k[0].NaDnu || k[0].DnoSusjed {
		t.Fatalf("bez susjeda: %+v", k)
	}
	if got := Opis(k[0], imena); !strings.HasSuffix(got, "; to je najniže što letva u arhivi bilježi, pa je voda možda ispod dosega mjernog uređaja") {
		t.Errorf("opis %q", got)
	}
	// susjed s nepoznatim očitanjem (vlastiti kvar) ne kaže ništa o dnu
	kvar := niz(0, spoji(isto(100, 6), []float64{math.NaN()}, od(100, -3, 11))...)
	if _, ima := digaoSe(kvar, sat(4), sat(17)); ima {
		t.Error("nepoznato očitanje susjeda")
	}
	if _, ima := digaoSe(niz(0, 100, 100), sat(4), sat(17)); ima {
		t.Error("premalo očitanja susjeda")
	}
}

// Mjerilo odskoči s −77 na oko 1385 cm, ondje skače ±20 cm i nakon pet sati
// se vrati; susjed se pomakne 4 cm. Kvar je cijeli taj niz, potvrđen
// susjedom.
func TestOdskocioDalekoIVratioSe(t *testing.T) {
	letva := niz(0, spoji(isto(-77, 4), []float64{1375, 1381, 1366, 1404, 1385, 1392}, isto(-120, 3))...)
	susjed := niz(0, spoji(isto(50, 5), isto(54, 8))...)
	k := Prepoznaj(letva, svojstva(), map[string][]Tocka{"gornji-brod": susjed})
	if len(k) != 1 || k[0].Vrsta != Odskocio || !k[0].Izvan || !k[0].Vratio || !k[0].Skokom || k[0].Susjed == nil || k[0].Traje {
		t.Fatalf("kvar %+v", k)
	}
	want := "mjerenje je 20. 10. u 4 h skočilo s −77 na oko 1383 cm, a nakon 6 h vratilo se na staru razinu; " +
		"susjedna letva Gornji Brod se u tom vremenu pomaknula samo za 4 cm; to je daleko izvan uobičajenog raspona letve"
	if got := Opis(k[0], imena); got != want {
		t.Errorf("opis\n %q\nželim\n %q", got, want)
	}
}

// Elektrana: razina skoči 70 cm, drži se pet sati i vrati se, a susjed
// nizvodno isto. Nije kvar. Ni velika voda koja polako prijeđe raspon.
func TestOdskokKojiSusjedPratiIVelikaVoda(t *testing.T) {
	letva := niz(0, spoji(isto(100, 4), isto(170, 5), isto(100, 3))...)
	susjed := niz(0, spoji(isto(40, 4), isto(105, 5), isto(40, 3))...)
	if k := Prepoznaj(letva, svojstva(), map[string][]Tocka{"gornji-brod": susjed}); len(k) != 0 {
		t.Errorf("elektrana: %+v", k)
	}
	val := niz(0, od(500, 10, 30)...)
	if k := Prepoznaj(val, svojstva(), map[string][]Tocka{"gornji-brod": niz(0, od(400, 10, 30)...)}); len(k) != 0 {
		t.Errorf("velika voda: %+v", k)
	}
}

// Letva bez susjeda: 35, 688, 688, 689, 36 cm je kvar (skok i povratak veći
// od praga i pol); 70 cm skoka uz prag 50 nije.
func TestOdskokBezSusjeda(t *testing.T) {
	s := Svojstva{Trajanje: 100 * time.Hour, Prag: 50}
	k := Prepoznaj(niz(0, 35, 35, 35, 688, 688, 689, 36, 35), s, nil)
	if len(k) != 1 || !k[0].Vratio || k[0].Susjed != nil || k[0].Izvan || !k[0].Natrag.Equal(sat(6)) || k[0].SatiDoPovratka() != 3 {
		t.Fatalf("kvar %+v", k)
	}
	if k := Prepoznaj(niz(0, 100, 100, 100, 170, 171, 170, 100), s, nil); len(k) != 0 {
		t.Errorf("mali odskok: %+v", k)
	}
	// susjed s očitanjima koji se pomaknuo ne da kvar, i kad je skok oštar
	s.Susjedi = []Susjed{{Letva: "gornji-brod", Prag: 20}}
	susjed := niz(0, 10, 10, 10, 600, 600, 600, 10, 10)
	if k := Prepoznaj(niz(0, 35, 35, 35, 688, 688, 689, 36, 35), s, map[string][]Tocka{"gornji-brod": susjed}); len(k) != 0 {
		t.Errorf("susjed je pratio: %+v", k)
	}
}

// Nakon rupe dulje od dana mjerilo javlja 1618 cm, daleko izvan raspona:
// kvar bez razine prije, potvrđen susjedom koji miruje. Jedno takvo
// očitanje nije kvar (to je skok).
func TestOdskokNakonRupe(t *testing.T) {
	letva := append(niz(0, isto(100, 4)...), niz(40, spoji(isto(1618, 5), isto(100, 2))...)...)
	susjed := niz(38, isto(60, 10)...)
	k := Prepoznaj(letva, svojstva(), map[string][]Tocka{"gornji-brod": susjed})
	if len(k) != 1 || k[0].ImaPrije || !k[0].Izvan || k[0].Susjed == nil {
		t.Fatalf("kvar %+v", k)
	}
	want := "mjerenje je od 21. 10. u 16 h do 21. 10. u 20 h stajalo oko 1618 cm (4 h), a susjedna letva Gornji Brod se u tom vremenu pomaknula samo za 0 cm; to je daleko izvan uobičajenog raspona letve"
	if got := Opis(k[0], imena); got != want {
		t.Errorf("opis\n %q\nželim\n %q", got, want)
	}
	jedno := append(niz(0, isto(100, 4)...), niz(40, 1618, 100)...)
	if k := Prepoznaj(jedno, svojstva(), nil); len(k) != 0 {
		t.Errorf("jedno: %+v", k)
	}
}

// Odskok koji traje opisan je od skoka; onaj koji je završio rupom kaže
// koliko je ostao; kvar daleko izvan raspona koji traje od rupe kaže „stoji
// oko”; zaglavljen koji je završio kaže od–do.
func TestOpisiKvarova(t *testing.T) {
	for _, tc := range []struct {
		k    Kvar
		want string
	}{
		{Kvar{Vrsta: Odskocio, Od: sat(4), Do: sat(10), Cm: 1385, Prije: -77, ImaPrije: true, Traje: true, Izvan: true},
			"mjerenje je 20. 10. u 4 h skočilo s −77 na oko 1385 cm i ondje se drži već 6 h, a to je daleko izvan uobičajenog raspona letve"},
		{Kvar{Vrsta: Odskocio, Od: sat(4), Do: sat(10), Cm: 1385, Prije: -77, ImaPrije: true, Izvan: true},
			"mjerenje je 20. 10. u 4 h skočilo s −77 na oko 1385 cm i ondje ostalo 6 h, a to je daleko izvan uobičajenog raspona letve"},
		{Kvar{Vrsta: Odskocio, Od: sat(4), Do: sat(10), Cm: 1618, Traje: true, Izvan: true},
			"mjerenje od 20. 10. u 4 h stoji oko 1618 cm, već 6 h, a to je daleko izvan uobičajenog raspona letve"},
		{Kvar{Vrsta: Zaglavljen, Od: sat(4), Do: sat(34), Cm: 412, Susjed: &Pomak{Letva: "gornji-brod", Cm: 38.4}},
			"mjerenje je stajalo na 412 cm od 20. 10. u 4 h do 21. 10. u 10 h (30 h), a susjedna letva Gornji Brod se u tom vremenu pomaknula za 38 cm"},
	} {
		if got := Opis(tc.k, imena); got != tc.want {
			t.Errorf("opis\n %q\nželim\n %q", got, tc.want)
		}
	}
}

// Zaglavljen niz unutar odskoka opisan je jednom, kao odskok; potvrđen
// susjedom ostaje, kad odskok potvrde nema.
func TestZaglavljenUOdskokuJednom(t *testing.T) {
	s := Svojstva{Trajanje: 6 * time.Hour, Prag: 100}
	k := Prepoznaj(niz(0, spoji(isto(48, 4), isto(-372, 10), isto(48, 3))...), s, nil)
	if len(k) != 1 || k[0].Vrsta != Odskocio {
		t.Errorf("kvar %+v", k)
	}
	z := []Kvar{{Od: sat(5), Do: sat(8), Susjed: &Pomak{}}}
	o := []Kvar{{Od: sat(4), Do: sat(9)}}
	if got := bezObuhvacenih(z, o); len(got) != 1 {
		t.Errorf("potvrđen zaglavljen nestao: %+v", got)
	}
}

// Susjed u vlastitom kvaru ne potvrđuje tuđi: u njegovu nizu sati kvara
// koje pokazuje sam postanu nepoznati, pa pomak preko njih ne zna se, ni
// kad kvar pokrije samo dio vremena.
func TestBezKvarova(t *testing.T) {
	s := Svojstva{Trajanje: 6 * time.Hour, Prag: 100}
	susjed := niz(0, spoji(isto(48, 4), isto(-372, 10), isto(48, 3))...)
	cist := BezKvarova(susjed, s)
	if len(cist) != 17 {
		t.Fatalf("ostalo %d očitanja", len(cist))
	}
	nepoznatih := 0
	for _, x := range cist {
		if math.IsNaN(x.Cm) {
			nepoznatih++
		} else if x.Cm != 48 {
			t.Errorf("ostao kvar %v", x)
		}
	}
	if nepoznatih != 10 {
		t.Errorf("nepoznatih %d", nepoznatih)
	}
	if _, ima := PomakU(cist, nil, sat(0), sat(16)); ima {
		t.Error("pomak preko kvara susjeda")
	}
	if p, ima := PomakU(cist, nil, sat(14), sat(16)); !ima || p != 0 {
		t.Errorf("pomak poslije kvara %v %v", p, ima)
	}
	mirno := niz(0, isto(48, 5)...)
	if got := BezKvarova(mirno, s); len(got) != 5 {
		t.Errorf("mirno: %d", len(got))
	}
	// Susjed koji je odskočio i vratio se ne potvrđuje zaglavljen uređaj,
	// iako se „pomaknuo” 600 cm.
	l := Svojstva{Trajanje: 10 * time.Hour, Prag: 50, Susjedi: []Susjed{{Letva: "gornji-brod", Prag: 20}}}
	odskok := BezKvarova(niz(0, spoji(isto(50, 6), isto(650, 4), isto(50, 10))...), Svojstva{Trajanje: 10 * time.Hour, Prag: 50})
	stoji := niz(0, isto(120, 20)...)
	if k := Prepoznaj(stoji, l, map[string][]Tocka{"gornji-brod": odskok}); len(k) != 0 {
		t.Errorf("potvrdio susjed u kvaru: %+v", k)
	}
	sirovo := niz(0, spoji(isto(50, 6), isto(650, 4), isto(50, 10))...)
	if k := Prepoznaj(stoji, l, map[string][]Tocka{"gornji-brod": sirovo}); len(k) != 1 {
		t.Errorf("bez oznake kvara susjed bi potvrdio: %+v", k)
	}
}

// Mjerilo se polako penje do svoje granice (1080 cm, daleko izvan raspona),
// ondje stoji i u satu padne za 960 cm: kvar od prvog sata izvan raspona do
// zadnjeg prije pada, potvrđen susjedom koji miruje. Velika voda koja polako
// prijeđe raspon i polako se vrati nije kvar.
func TestIzlazniSkok(t *testing.T) {
	s := svojstva()
	s.Prag = 150
	letva := niz(0, spoji(isto(100, 3), od(200, 110, 8), isto(1080, 3), []float64{120, 118})...)
	k := Prepoznaj(letva, s, map[string][]Tocka{"gornji-brod": niz(0, isto(60, 16)...)})
	if len(k) != 1 || k[0].Izlaz != -960 || !k[0].Izvan || k[0].Susjed == nil || !k[0].Od.Equal(sat(7)) || !k[0].Do.Equal(sat(13)) {
		t.Fatalf("kvar %+v", k)
	}
	want := "mjerenje je od 20. 10. u 7 h do 20. 10. u 13 h stajalo oko 970 cm (6 h), a onda je naglo palo za 960 cm; " +
		"susjedna letva Gornji Brod se u tom vremenu pomaknula samo za 0 cm; to je daleko izvan uobičajenog raspona letve"
	if got := Opis(k[0], imena); got != want {
		t.Errorf("opis\n %q\nželim\n %q", got, want)
	}
	val := niz(0, spoji(od(500, 20, 10), od(700, -20, 10))...)
	if k := Prepoznaj(val, s, nil); len(k) != 0 {
		t.Errorf("velika voda: %+v", k)
	}
	if smjer(30) != "skočilo" {
		t.Error("smjer")
	}
}

// Kvar koji je počeo prije najstarijeg pročitanog očitanja kaže „barem od”.
func TestBaremOd(t *testing.T) {
	k := Kvar{Vrsta: Zaglavljen, Od: sat(4), Do: sat(34), Cm: 19, Traje: true, BaremOd: true}
	if got := Opis(k, imena); got != "mjerenje stoji na 19 cm barem od 20. 10. u 4 h, već više od 30 h" {
		t.Errorf("opis %q", got)
	}
	k = Kvar{Vrsta: Odskocio, Od: sat(4), Do: sat(34), Cm: 1618, Traje: true, BaremOd: true}
	if got := Opis(k, imena); got != "mjerenje barem od 20. 10. u 4 h stoji oko 1618 cm, već više od 30 h" {
		t.Errorf("opis %q", got)
	}
}

// Zaglavljena vrijednost daleko izvan raspona je kvar već nakon pola
// najduljeg normalnog trajanja (najmanje NajkraceTrajanje); kraće stajanje
// daleko izvan raspona nije, a ni dulje unutar raspona.
func TestDalekoKraceOdTrajanja(t *testing.T) {
	s := svojstva()
	s.Trajanje, s.Susjedi = 40*time.Hour, nil
	if k := Prepoznaj(niz(0, spoji(od(560, 40, 10), isto(940, 21))...), s, nil); len(k) != 1 || !k[0].Izvan || k[0].Vrsta != Zaglavljen {
		t.Errorf("daleko: %+v", k)
	}
	if k := Prepoznaj(niz(0, spoji(od(560, 40, 10), isto(940, 12), od(950, 10, 4))...), s, nil); len(k) != 0 {
		t.Errorf("kratko daleko: %+v", k)
	}
	if k := Prepoznaj(niz(0, isto(300, 30)...), s, nil); len(k) != 0 {
		t.Errorf("unutar raspona: %+v", k)
	}
	s.Trajanje = 8 * time.Hour
	if k := Prepoznaj(niz(0, spoji(od(560, 40, 10), isto(940, 7), od(950, 10, 3))...), s, nil); len(k) != 1 {
		t.Errorf("najmanje šest sati: %+v", k)
	}
}

// Pomak susjeda: razlika 95. i 5. percentila, bez vrijednosti daleko izvan
// raspona; premalo očitanja ne zna se.
func TestPomakU(t *testing.T) {
	r := &Raspon{Od: -300, Do: 600}
	n := niz(0, spoji(od(0, 1, 20), []float64{900})...)
	if p, ima := PomakU(n, r, sat(0), sat(20)); !ima || p != 18 {
		t.Errorf("pomak %v %v", p, ima)
	}
	if _, ima := PomakU(n, r, sat(0), sat(100)); ima {
		t.Error("premalo očitanja za 100 h")
	}
	if p, ima := PomakU(n, nil, sat(10), sat(20)); !ima || math.Abs(p-890) > 0.1 {
		t.Errorf("bez raspona %v %v", p, ima)
	}
}

// Letva bez arhive koja izmjenjuje pravu razinu (48 cm, većina sati) i kvar
// (−372 cm): kvar je samo −372. Povratak na 48 cm nije ni skok u
// zaglavljenu vrijednost ni odskok.
func TestBezArhiveKvarJeDaljiOdSredine(t *testing.T) {
	s := Svojstva{Trajanje: 12 * time.Hour, Prag: 100}
	letva := niz(0, spoji(isto(48, 20), isto(-372, 14), isto(48, 16), isto(-372, 3), isto(48, 20))...)
	k := Prepoznaj(letva, s, nil)
	if len(k) != 2 || k[0].Cm != -372 || k[1].Cm != -372 {
		t.Fatalf("kvarovi %+v", k)
	}
	for _, x := range k {
		if x.U(sat(45)) || x.U(sat(60)) {
			t.Errorf("prava razina u kvaru: %+v", x)
		}
	}
}

// Prognoza ne uzima kvar koji potvrđuje susjed, osim na dnu mjerila.
func TestBezPrognoze(t *testing.T) {
	susjed := &Pomak{Letva: "gornji-brod", Cm: 40}
	if !(Kvar{Susjed: susjed}).BezPrognoze() || (Kvar{Susjed: susjed, NaDnu: true}).BezPrognoze() || (Kvar{Skokom: true}).BezPrognoze() {
		t.Error("BezPrognoze")
	}
}

// Stalna razina prije skoka: tri očitanja unutar praga i unutar dana.
func TestStalnaPrije(t *testing.T) {
	if !stalnaPrije(niz(0, 48, 49, 48, -372), 3, 100) {
		t.Error("stalna")
	}
	if stalnaPrije(niz(0, 48, 49, -372), 2, 100) || stalnaPrije(niz(0, 1, 145, 127, 1), 3, 100) {
		t.Error("premalo ili nestalna")
	}
	rupa := append(niz(0, 48), niz(30, 48, 48, -372)...)
	if stalnaPrije(rupa, 3, 100) {
		t.Error("starija od dana")
	}
}
