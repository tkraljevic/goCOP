package models

import (
	"math"
	"testing"
)

func TestJedinicaINazivVelicine(t *testing.T) {
	for _, s := range []struct{ velicina, jedinica, naziv string }{
		{"vodostaj", "cm", "Vodostaj"},
		{"protok", "m³/s", "Protok"},
		{"temperatura", "°C", "Temperatura vode"},
		{"koncentracija", "g/m³", "Koncentracija nanosa"},
		{"pronos", "t", "Pronos nanosa"},
		{"oborina", "mm", "Oborina"},
		{"snijeg", "mm", "Snijeg (vodeni ekvivalent)"},
		{"visina-snijega", "cm", "Visina snijega"},
		{"temperatura-zraka", "°C", "Temperatura zraka"},
		{"vlaga", "", "vlaga"},
	} {
		if got := JedinicaVelicine(s.velicina); got != s.jedinica {
			t.Errorf("JedinicaVelicine(%q) = %q, očekivano %q", s.velicina, got, s.jedinica)
		}
		if got := NazivVelicine(s.velicina); got != s.naziv {
			t.Errorf("NazivVelicine(%q) = %q, očekivano %q", s.velicina, got, s.naziv)
		}
		if got := (HidroNiz{Velicina: s.velicina}).Jedinica(); got != s.jedinica {
			t.Errorf("HidroNiz %q: jedinica %q", s.velicina, got)
		}
		sv := SazetakVelicine{Velicina: s.velicina}
		if sv.Jedinica() != s.jedinica || sv.Naziv() != s.naziv {
			t.Errorf("SazetakVelicine %q: %q, %q", s.velicina, sv.Jedinica(), sv.Naziv())
		}
	}
}

// Skupina slijedi red koji uređuje administrator; preračun je uvijek svoja
// skupina, kakav god mu red bio.
func TestSkupinaIzvoraPoRedu(t *testing.T) {
	for _, s := range []struct {
		naziv string
		red   int
		want  string
	}{
		{"preracun-mohacs", 1, SkupinaPreracun},
		{"his2000", 1, SkupinaOvjereno},
		{"his2000", 10, SkupinaOvjereno},
		{"letva-hv", 11, SkupinaSLetve},
		{"letva-hv", 19, SkupinaSLetve},
		{"cop", 20, SkupinaOperativno},
		{"preracun", 30, SkupinaOperativno},
	} {
		if got := SkupinaIzvora(s.naziv, s.red); got != s.want {
			t.Errorf("SkupinaIzvora(%q, %d) = %q, očekivano %q", s.naziv, s.red, got, s.want)
		}
	}
}

func TestNazivIzvoraPreracunaINazivLetve(t *testing.T) {
	for _, s := range []struct{ ulaz, want string }{
		{"preracun-hq", "preračunato iz krivulje protoka"},
		{"preracun-hq-izvan", "preračunato iz krivulje protoka, izvan mjerenog odnosa"},
		{"preracun-sotin", "preračunato iz Sotin"},
	} {
		if got := NazivIzvora(s.ulaz); got != s.want {
			t.Errorf("NazivIzvora(%q) = %q, očekivano %q", s.ulaz, got, s.want)
		}
	}
	for _, s := range []struct{ ulaz, want string }{
		{"mohacs", "Mohácsa"}, {"belisce", "Belišća"}, {"sotin", "Sotin"}, {"x", "X"}, {"", ""},
	} {
		if got := NazivLetve(s.ulaz); got != s.want {
			t.Errorf("NazivLetve(%q) = %q, očekivano %q", s.ulaz, got, s.want)
		}
	}
}

func TestJeIzmjerenoINazivVrste(t *testing.T) {
	for _, s := range []struct {
		izvor string
		want  bool
	}{
		{"his2000", true}, {"cop", true}, {"preracun", true}, {"preracun-mohacs", false}, {"preracun-hq-izvan", false},
	} {
		if got := (HidroNiz{Izvor: s.izvor}).JeIzmjereno(); got != s.want {
			t.Errorf("JeIzmjereno(%q) = %v", s.izvor, got)
		}
	}
	for _, s := range []struct{ ulaz, want string }{
		{"satni", "svaki sat"}, {"dvokratni", "dva puta dnevno"}, {"jutarnji", "jutarnje očitanje"},
		{"srednjak", "dnevni srednjak"}, {"dnevni", "jednom dnevno"}, {"tjedni", "tjedni"},
	} {
		if got := NazivVrste(s.ulaz); got != s.want {
			t.Errorf("NazivVrste(%q) = %q, očekivano %q", s.ulaz, got, s.want)
		}
	}
}

func TestTocnostSpojeneVrijednostiIDijela(t *testing.T) {
	for _, s := range []struct {
		tocnost       float64
		label, oznaka string
	}{
		{0, "ovjereno", "mjerilo"},
		{-1, "ovjereno", "mjerilo"},
		{3, "±3", "±3 cm"},
		{3.6, "±4", "±4 cm"},
	} {
		if got := (SpojenaVrijednost{Tocnost: s.tocnost}).TocnostLabel(); got != s.label {
			t.Errorf("TocnostLabel(%v) = %q, očekivano %q", s.tocnost, got, s.label)
		}
		if got := (SpojDio{Tocnost: s.tocnost}).TocnostOznaka(); got != s.oznaka {
			t.Errorf("TocnostOznaka(%v) = %q, očekivano %q", s.tocnost, got, s.oznaka)
		}
	}
}

func TestPreracunateKrajnostiIDecimale(t *testing.T) {
	for _, s := range []struct {
		min, max      string
		pMin, pMax, i bool
	}{
		{"his2000", "his2000", false, false, false},
		{"preracun-mohacs", "his2000", true, false, true},
		{"his2000", "preracun-bezdan", false, true, true},
	} {
		sv := SazetakVelicine{MinIzvor: s.min, MaxIzvor: s.max}
		if sv.MinPreracunat() != s.pMin || sv.MaxPreracunat() != s.pMax || sv.ImaPreracunatih() != s.i {
			t.Errorf("%s/%s: %v %v %v", s.min, s.max, sv.MinPreracunat(), sv.MaxPreracunat(), sv.ImaPreracunatih())
		}
	}
	for v, want := range map[string]int{"temperatura": 1, "koncentracija": 1, "vodostaj": 0, "protok": 0} {
		if got := (SazetakVelicine{Velicina: v}).Decimala(); got != want {
			t.Errorf("Decimala(%q) = %d, očekivano %d", v, got, want)
		}
	}
}

// Udio koji postoji, a zaokruži se na nulu, piše se kao „<1 %".
func TestUdioDijelaSpoja(t *testing.T) {
	for _, s := range []struct {
		zapisa, ukupno int
		udio           int
		hr             string
	}{
		{25, 200, 12, "12 %"},
		{50, 200, 25, "25 %"},
		{1, 1000, 0, "<1 %"},
		{0, 1000, 0, "0 %"},
		{5, 0, 0, "<1 %"},
		{200, 200, 100, "100 %"},
	} {
		d := SpojDio{Zapisa: s.zapisa}
		if got := d.Udio(s.ukupno); got != s.udio {
			t.Errorf("Udio(%d/%d) = %d, očekivano %d", s.zapisa, s.ukupno, got, s.udio)
		}
		if got := d.UdioHR(s.ukupno); got != s.hr {
			t.Errorf("UdioHR(%d/%d) = %q, očekivano %q", s.zapisa, s.ukupno, got, s.hr)
		}
	}
}

func TestMjesecImeISeZbraja(t *testing.T) {
	for m, want := range map[int]string{0: "", 1: "siječanj", 2: "veljača", 6: "lipanj", 12: "prosinac", 13: "", -1: ""} {
		if got := MjesecIme(m); got != want {
			t.Errorf("MjesecIme(%d) = %q, očekivano %q", m, got, want)
		}
	}
	if got := (HidroMjesec{Mjesec: 3}).Naziv(); got != "ožujak" {
		t.Errorf("HidroMjesec 3: %q", got)
	}
	if !SeZbraja("pronos") || SeZbraja("vodostaj") || SeZbraja("protok") {
		t.Error("zbraja se samo pronos")
	}
}

func TestSirinaDnoIDubinaProfila(t *testing.T) {
	var prazan ProfilKorita
	if prazan.Sirina() != 0 || prazan.Dno() != 0 {
		t.Error("prazan profil ima širinu ili dno")
	}
	jedna := ProfilKorita{Tocke: []TockaProfila{{Stacionaza: 10, Visina: 80}}}
	if jedna.Sirina() != 0 || jedna.Dno() != 80 {
		t.Errorf("jedna točka: širina %v, dno %v", jedna.Sirina(), jedna.Dno())
	}
	p := ProfilKorita{KotaNule: 80, Tocke: []TockaProfila{
		{Stacionaza: 10, Visina: 84}, {Stacionaza: 60, Visina: 77.2}, {Stacionaza: 120, Visina: 75.3}, {Stacionaza: 250, Visina: 83.9},
	}}
	if got := p.Sirina(); got != 240 {
		t.Errorf("širina %v, očekivano 240", got)
	}
	if got := p.Dno(); got != 75.3 {
		t.Errorf("dno %v, očekivano 75,3", got)
	}
	if got := p.DubinaPri(250); math.Abs(got-7.2) > 1e-9 {
		t.Errorf("dubina pri 250 cm %v, očekivano 7,2", got)
	}
	if got := p.DubinaPri(-100); math.Abs(got-3.7) > 1e-9 {
		t.Errorf("dubina pri -100 cm %v, očekivano 3,7", got)
	}
}

// Odsječak se ispisuje s decimalnim zarezom i pravim minusom; nulti
// kvadratni ili slobodni član se ne ispisuje.
func TestZapisOdsjecka(t *testing.T) {
	for _, s := range []struct {
		ime  string
		o    HQOdsjecak
		want string
	}{
		{"parabola", HQOdsjecak{P1: 12.5, P2: -3.25, P3: 4}, "Q = 12,5·H² − 3,25·H + 4"},
		{"parabola s negativnim članom", HQOdsjecak{P1: 1, P2: 2, P3: -0.5}, "Q = 1·H² + 2·H − 0,5"},
		{"pravac", HQOdsjecak{P2: 100}, "Q = 100·H"},
		{"pravac s pomakom", HQOdsjecak{P2: 50.123456, P3: -2}, "Q = 50,1235·H − 2"},
		{"potencija", HQOdsjecak{Oblik: OblikPotencija, P1: 30.5, P2: 1.6, P3: 0.25}, "Q = 30,5 · (H + 0,25)^1,6"},
		{"potencija s dodatkom", HQOdsjecak{Oblik: OblikPotencija, P1: 30.5, P2: 1.6, P3: 0.25, P4: 12}, "Q = 30,5 · (H + 0,25)^1,6 + 12"},
		{"potencija s odbitkom", HQOdsjecak{Oblik: OblikPotencija, P1: 30.5, P2: 1.6, P3: 0.257, P4: -3.5}, "Q = 30,5 · (H + 0,26)^1,6 − 3,5"},
	} {
		if got := s.o.Zapis(); got != s.want {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.want)
		}
	}
}

func TestRasponIGranicaOdsjecka(t *testing.T) {
	o := HQOdsjecak{OdCm: -50, DoCm: 300}
	if got := o.Raspon(); got != "-50 do 300 cm" {
		t.Errorf("raspon %q", got)
	}
	if got := o.Granica(true); got != "-50 ≤ H ≤ 300 cm" {
		t.Errorf("prvi odsječak %q", got)
	}
	if got := o.Granica(false); got != "-50 < H ≤ 300 cm" {
		t.Errorf("ostali odsječci %q", got)
	}
}

func TestRasponIZapisKrivulje(t *testing.T) {
	if _, _, ok := (HQKrivulja{}).Raspon(); ok {
		t.Error("prazna krivulja ima raspon")
	}
	if got := (HQKrivulja{}).Zapis(); got != "" {
		t.Errorf("prazna krivulja: %q", got)
	}
	k := HQKrivulja{Odsjecci: []HQOdsjecak{
		{OdCm: 100, DoCm: 300, P2: 102},
		{OdCm: -20, DoCm: 100, P2: 100},
	}}
	if od, do, ok := k.Raspon(); !ok || od != -20 || do != 300 {
		t.Errorf("raspon %d–%d %v, očekivano -20–300", od, do, ok)
	}
	if got := k.Zapis(); got != "Q = 102·H (100 do 300 cm); Q = 100·H (-20 do 100 cm)" {
		t.Errorf("zapis %q", got)
	}
}

// Skok na granici računa se samo ondje gdje oba odsječka daju protok, i
// kad prethodni nije nula.
func TestNajveciSkokKrivulje(t *testing.T) {
	if got := (HQKrivulja{Odsjecci: []HQOdsjecak{{OdCm: 0, DoCm: 100, P2: 100}}}).NajveciSkok(); got != 0 {
		t.Errorf("jedan odsječak: %v", got)
	}
	k := HQKrivulja{Odsjecci: []HQOdsjecak{
		{OdCm: 0, DoCm: 100, P2: 100},
		{OdCm: 100, DoCm: 200, P2: 102},
		{OdCm: 200, DoCm: 300, P2: 102 * 1.01, P3: 0},
	}}
	if got := k.NajveciSkok(); math.Abs(got-2) > 1e-9 {
		t.Errorf("najveći skok %v %%, očekivano 2 %%", got)
	}
	preskoci := HQKrivulja{Odsjecci: []HQOdsjecak{
		{OdCm: -100, DoCm: 0, P2: 100},                                      // na 0 cm daje 0
		{OdCm: 0, DoCm: 100, P2: 100, P3: 50},                               // prethodni je nula: preskače se
		{OdCm: 100, DoCm: 200, Oblik: OblikPotencija, P1: 1, P2: 1, P3: -1}, // potencija bez protoka na rubu
	}}
	if got := preskoci.NajveciSkok(); got != 0 {
		t.Errorf("granice bez usporedbe dale su skok %v", got)
	}
}

// Obrat bira bliži od dvaju susjednih centimetara; pravac Q = 100·H daje
// točno 1 m³/s po centimetru.
func TestVodostajBiraBliziCentimetar(t *testing.T) {
	k := HQKrivulja{Odsjecci: []HQOdsjecak{{OdCm: 0, DoCm: 300, P2: 100}}}
	for _, s := range []struct {
		protok float64
		cm     int
	}{{150.3, 150}, {150.7, 151}, {150, 150}} {
		cm, izvan, ok := k.Vodostaj(s.protok)
		if !ok || izvan || cm != s.cm {
			t.Errorf("Vodostaj(%v) = %d (izvan %v, ok %v), očekivano %d", s.protok, cm, izvan, ok, s.cm)
		}
	}
}

// Krivulja s rupom između odsječaka ili s potencijom bez protoka unutar
// svojeg raspona nema obrata.
func TestVodostajNaNeispravnojKrivulji(t *testing.T) {
	rupa := HQKrivulja{Odsjecci: []HQOdsjecak{{OdCm: 0, DoCm: 100, P2: 100}, {OdCm: 200, DoCm: 300, P2: 100}}}
	if cm, _, ok := rupa.Vodostaj(150); ok {
		t.Errorf("krivulja s rupom dala je %d cm", cm)
	}
	potencija := HQKrivulja{Odsjecci: []HQOdsjecak{{OdCm: 0, DoCm: 100, Oblik: OblikPotencija, P1: 10, P2: 1.5, P3: -0.5}}}
	if cm, _, ok := potencija.Vodostaj(5); ok {
		t.Errorf("potencija bez protoka na donjem rubu dala je %d cm", cm)
	}
	if _, _, ok := (HQKrivulja{}).ProtokProsiren(100); ok {
		t.Error("prazna krivulja ima produljeni protok")
	}
}

// Rubni odsječci traže se po rasponu, ne po redoslijedu upisa.
func TestRubniOdsjecciNeovisniORedoslijedu(t *testing.T) {
	redom := HQKrivulja{Odsjecci: []HQOdsjecak{{OdCm: 0, DoCm: 100, P2: 100}, {OdCm: 100, DoCm: 300, P2: 100}}}
	obrnuto := HQKrivulja{Odsjecci: []HQOdsjecak{redom.Odsjecci[1], redom.Odsjecci[0]}}
	for ime, k := range map[string]HQKrivulja{"redom": redom, "obrnuto": obrnuto} {
		if od, do, ok := k.Raspon(); !ok || od != 0 || do != 300 {
			t.Errorf("%s: raspon %d–%d", ime, od, do)
		}
		if dolje, gore, ok := k.RasponProduljenja(); !ok || dolje != 0 || gore != 320 {
			t.Errorf("%s: produljenje %d–%d, očekivano 0–320", ime, dolje, gore)
		}
		if q, izvan, ok := k.ProtokProsiren(310); !ok || !izvan || math.Abs(q-310) > 1e-9 {
			t.Errorf("%s: iznad krivulje %v %v %v", ime, q, izvan, ok)
		}
	}
}

func TestSpojiProfileRubniSlucajevi(t *testing.T) {
	if p := SpojiProfile(nil); p.Datum != "" || p.Tocke != nil || p.Spojen() {
		t.Errorf("bez snimaka: %+v", p)
	}
	jedna := ProfilKorita{Datum: "2020-05-01", PomakM: 3, Tocke: []TockaProfila{{Stacionaza: 0, Visina: 80}, {Stacionaza: 10, Visina: 76}}}
	if p := SpojiProfile([]ProfilKorita{jedna}); p.Datum != jedna.Datum || p.PomakM != 3 || len(p.Tocke) != 2 || p.Spojen() {
		t.Errorf("jedna snimka se vraća kakva jest: %+v", p)
	}
	// starija snimka bez točaka ne dodaje ništa
	prazna := ProfilKorita{Datum: "2015-01-01"}
	p := SpojiProfile([]ProfilKorita{prazna, jedna})
	if p.Datum != "2020-05-01" || len(p.Tocke) != 2 || p.Tocke[0].Stacionaza != 3 || p.Tocke[1].Stacionaza != 13 ||
		len(p.Sastav) != 1 || p.Sastav[0].Datum != "2020-05-01" || p.Sastav[0].Tocaka != 2 {
		t.Errorf("starija snimka bez točaka: %+v", p)
	}
}

// Krilo starije snimke nastavlja se samo kad dodiruje nacrtanu liniju i kad
// visinski korak na spoju odgovara nagibu terena.
func TestNastavljaSeKrilo(t *testing.T) {
	nacrtano := []TockaProfila{{Stacionaza: 50, Visina: 80}, {Stacionaza: 60, Visina: 80}}
	for _, s := range []struct {
		ime   string
		krilo []TockaProfila
		want  bool
	}{
		{"desno, predaleko", []TockaProfila{{Stacionaza: 80, Visina: 80}, {Stacionaza: 82, Visina: 80}}, false},
		{"desno, stepenica", []TockaProfila{{Stacionaza: 61, Visina: 85}, {Stacionaza: 62, Visina: 85}}, false},
		{"desno, nastavlja", []TockaProfila{{Stacionaza: 61, Visina: 80.3}, {Stacionaza: 62, Visina: 80.5}}, true},
		{"lijevo, predaleko", []TockaProfila{{Stacionaza: 10, Visina: 80}, {Stacionaza: 12, Visina: 80}}, false},
		{"lijevo, stepenica", []TockaProfila{{Stacionaza: 47, Visina: 79}, {Stacionaza: 48, Visina: 85}}, false},
		{"lijevo, nastavlja", []TockaProfila{{Stacionaza: 46, Visina: 80}, {Stacionaza: 48, Visina: 80.1}}, true},
	} {
		if got := nastavljaSe(nacrtano, s.krilo, 0, len(s.krilo)-1); got != s.want {
			t.Errorf("%s: %v, očekivano %v", s.ime, got, s.want)
		}
	}
	if nastavljaSe(nil, []TockaProfila{{Stacionaza: 1, Visina: 80}}, 0, 0) {
		t.Error("krilo koje ništa ne dodiruje")
	}
}

func TestPrilijezeIGraniceNagiba(t *testing.T) {
	dvije := []TockaProfila{{Stacionaza: 0}, {Stacionaza: 2}}
	if !prilijeze(dvije, 1, 1, 3) || prilijeze(dvije, 1, 1, 5) {
		t.Error("na kraju snimke korak se mjeri unatrag")
	}
	jedna := []TockaProfila{{Stacionaza: 5}}
	if !prilijeze(jedna, 0, 1, 5) || prilijeze(jedna, 0, 1, 6) {
		t.Error("snimka od jedne točke priliježe samo na istu stacionažu")
	}
	zid := []TockaProfila{{Stacionaza: 0, Visina: 80}, {Stacionaza: 2, Visina: 86}}
	if got := nagibDo(zid, 0, 1); got != najveciNagib {
		t.Errorf("nagib zida %v, očekivano %v", got, najveciNagib)
	}
	if got := nagibDo([]TockaProfila{{Stacionaza: 0}, {Stacionaza: 0.5, Visina: 9}}, 0, 1); got != blagiNagib {
		t.Errorf("bez točke na metar razmaka %v, očekivano %v", got, blagiNagib)
	}
}
