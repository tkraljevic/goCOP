package his2000

import (
	"reflect"
	"testing"
	"time"
)

// Zaključava pomoćne korake čitanja izvoza: pretvorbu iz cp1250, red dana i
// mjeseca u datumu, raspon godina datoteke, stupce ispisa po mjesecima i
// usporedbu zatečenog niza s novim izvozom.

// Bajt koji tablica cp1250 ne zna postaje upitnik, a ne nestaje.
func TestIzCP1250NepoznatiBajt(t *testing.T) {
	if got := izCP1250([]byte{'D', 'a', 'l', 'j', ' ', 0x8A, 0x81, 0xB0}); got != "Dalj Š?°" {
		t.Errorf("pretvoreno %q", got)
	}
}

// Red dana i mjeseca utvrđuje se iz podataka: broj veći od 12 na prvom
// mjestu je dan, a na drugom mjesec; kad se ne vidi, dan ide prvi.
func TestDanIdePrvi(t *testing.T) {
	slucajevi := []struct {
		polja [][4]string
		prvi  bool
	}{
		{nil, true},
		{[][4]string{{"01", "02"}, {"13", "01"}}, true},
		{[][4]string{{"01", "02"}, {"03", "25"}}, false},
		{[][4]string{{"01", "02"}, {"12", "12"}}, true},
	}
	for _, s := range slucajevi {
		if got := danIdePrvi(s.polja); got != s.prvi {
			t.Errorf("%v → %v", s.polja, got)
		}
	}
}

// Raspon godina čita se iz redaka koji odgovaraju uzorku; godina koja nije
// broj veći od nule ne ulazi u raspon.
func TestRazdobljeDatoteke(t *testing.T) {
	redci := []string{
		"Satni podaci postaje DALJ  za godinu 2001,  VODOSTAJ  (cm)",
		" 1. 1.0000  0;117;",
		"31.12.2003 23;;",
		" 1. 1.2001  0;117;",
		"nešto drugo",
	}
	if od, do := razdobljeSatnih(redci); od != 2001 || do != 2003 {
		t.Errorf("raspon %d-%d, očekivano 2001-2003", od, do)
	}
	if od, do := razdobljeDnevnih([]string{"bez redaka"}); od != 0 || do != 0 {
		t.Errorf("bez redaka %d-%d", od, do)
	}
}

// Mjesec se traži kao riječ omeđena razmacima, da III ne pogodi u VIII.
func TestNadjiRijecUIspisu(t *testing.T) {
	slucajevi := []struct {
		s, rijec string
		od, i    int
	}{
		{"VIII III", "III", 0, 5},
		{"IIII", "III", 0, -1},
		{"  I  II", "II", 0, 5},
		{"  I  II", "I", 3, -1},
		{"I", "I", 0, 0},
		{"", "I", 0, -1},
	}
	for _, s := range slucajevi {
		if got := nadjiRijec(s.s, s.rijec, s.od); got != s.i {
			t.Errorf("nadjiRijec(%q, %q, %d) = %d, očekivano %d", s.s, s.rijec, s.od, got, s.i)
		}
	}
	if got := krajeviMjeseci("      2018      I     II    III"); got != nil {
		t.Errorf("zaglavlje bez svih mjeseci: %v", got)
	}
}

// Redak zna biti kraći od zaglavlja kad zadnji mjeseci nemaju podatak.
func TestIsjecakStupca(t *testing.T) {
	slucajevi := []struct {
		r       string
		od, do  int
		isjecak string
	}{
		{"   1   20.6   37.5", 4, 11, "20.6"},
		{"   1   20.6", 7, 30, "20.6"},
		{"   1", 10, 20, ""},
		{"   1   20.6", -1, 4, ""},
		{"   1   20.6", 6, 6, ""},
	}
	for _, s := range slucajevi {
		if got := isjecak(s.r, s.od, s.do); got != s.isjecak {
			t.Errorf("isjecak(%q, %d, %d) = %q, očekivano %q", s.r, s.od, s.do, got, s.isjecak)
		}
	}
}

// Ispis po mjesecima prepoznaje se po zaglavlju s godinom i mjesecima ili po
// satnom ispisu koji počinje danom i mjesecom.
func TestImaIspisPoMjesecima(t *testing.T) {
	slucajevi := map[string]struct {
		redci []string
		ima   bool
	}{
		"zaglavlje godine": {[]string{"", "      2018      I     II    III     IV"}, true},
		"satni ispis":      {[]string{"             1     2", " 1.  1."}, true},
		"popis redaka":     {[]string{"01.01.2001;176;"}, false},
		"prazno":           {nil, false},
	}
	for ime, s := range slucajevi {
		if got := imaIspisPoMjesecima(s.redci); got != s.ima {
			t.Errorf("%s: %v", ime, got)
		}
	}
}

// Redak s danom izvan 1–31 i dan kojeg u mjesecu nema ne ulaze u niz.
func TestIspisPreskaceNemoguceDane(t *testing.T) {
	redci := []string{
		"         1      2.0",
		"      2018      I     II    III     IV      V     VI    VII   VIII     IX      X     XI    XII",
		"         0      1.0",
		"        45      1.0",
		"        30    3.0    4.0",
		"        31    5.0    6.0           7.0",
	}
	v := citajIspisDnevni(redci)
	ocekivano := []Vrijednost{
		{Kad: dan(2018, 1, 30), Dan: true, V: "3,0"},
		{Kad: dan(2018, 1, 31), Dan: true, V: "5,0"},
	}
	if !reflect.DeepEqual(v, ocekivano) {
		t.Errorf("pročitano %+v", v)
	}
}

// Zatečena vrijednost kojoj nema para u novom izvozu je gubitak, osim kad je
// to sat koji lokalno ne postoji; zapis koji nije broj uspoređuje se kao
// tekst.
func TestUsporedbaZatecenogNiza(t *testing.T) {
	prvi := dan(2026, 3, 28)
	nepostojeci := time.Date(2026, 3, 29, 2, 0, 0, 0, time.UTC)
	stari := map[time.Time]string{
		prvi:             "200",
		dan(2026, 3, 27): "190",
		nepostojeci:      "201",
		dan(2026, 3, 30): "n/a",
		dan(2026, 3, 31): "n/a",
		dan(2026, 3, 26): "1919,000",
	}
	novi := []Vrijednost{
		{Kad: prvi, V: "200"},
		{Kad: dan(2026, 3, 30), V: "n/a"},
		{Kad: dan(2026, 3, 31), V: "nema"},
		{Kad: dan(2026, 3, 26), V: "1919"},
	}
	sukoba, samoStari, nepostoji := Usporedi(stari, novi)
	if sukoba != 1 || samoStari != 1 || nepostoji != 1 {
		t.Errorf("sukoba %d, samo u starom %d, nepostojećih sati %d — očekivano po jedan", sukoba, samoStari, nepostoji)
	}
	for _, s := range []struct {
		a, b string
		isti bool
	}{
		{"1919,000", "1919", true},
		{" 1,5", "1.5", true},
		{"abc", "abc", true},
		{"abc", "1", false},
		{"1", "", false},
	} {
		if got := istiBroj(s.a, s.b); got != s.isti {
			t.Errorf("istiBroj(%q, %q) = %v", s.a, s.b, got)
		}
	}
}
