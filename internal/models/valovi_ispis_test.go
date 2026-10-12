package models

import (
	"testing"
	"time"
)

// Zaključava ispis trenutka vala: u domaćem vremenu, satni niz sa satom,
// dnevni samo s danom (sat koji podaci ne znaju ne ispisuje se), a prazan
// trenutak kao crtica.
func TestTrenutakValaUDomacemVremenu(t *testing.T) {
	// 22 h po UTC-u u lipnju je ponoć idućeg dana u Zagrebu
	kad := time.Date(2024, 6, 10, 22, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		ime   string
		kad   time.Time
		korak string
		want  string
	}{
		{"satni", kad, "satni", "11.6.2024. 00h"},
		{"dnevni", kad, "dnevni", "11.6.2024."},
		{"bez koraka kao dnevni", kad, "", "11.6.2024."},
		{"zimi", time.Date(2013, 1, 5, 7, 0, 0, 0, Zagreb), "satni", "5.1.2013. 07h"},
		{"prazan trenutak", time.Time{}, "satni", "—"},
	} {
		if got := trenutakHR(s.kad, s.korak); got != s.want {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.want)
		}
	}
}

// Zaključava ispis vala i stupnja: trenuci u razlučivosti vala, trajanje kako
// se čita, puni sati zaokruženi kao u obrascu obrane, vrh zaokružen na cijeli
// centimetar s predznakom i godina početka.
func TestIspisValaIStupnja(t *testing.T) {
	poc := time.Date(2024, 6, 3, 14, 0, 0, 0, Zagreb)
	s := StupanjVala{
		Stupanj:       PhaseRegular,
		Pocelo:        poc,
		Zavrsilo:      poc.Add(20 * 24 * time.Hour),
		Trajanje:      431*time.Hour + 29*time.Minute,
		TrajanjeIznad: 30 * time.Hour,
		Korak:         "satni",
	}
	if s.Sati() != 431 || s.TrajanjeHR() != "18 dana" || s.TrajanjeIznadHR() != "30 sati" {
		t.Errorf("trajanje: %d h, %q, iznad %q", s.Sati(), s.TrajanjeHR(), s.TrajanjeIznadHR())
	}
	s.Trajanje = 431*time.Hour + 30*time.Minute
	if s.Sati() != 432 {
		t.Errorf("pola sata zaokružuje se naviše: %d h", s.Sati())
	}
	if s.PoceloHR() != "3.6.2024. 14h" || s.ZavrsiloHR() != "23.6.2024. 14h" {
		t.Errorf("trenuci stupnja: %q – %q", s.PoceloHR(), s.ZavrsiloHR())
	}
	if s.Raspon() != 20*24*time.Hour {
		t.Errorf("raspon stupnja %v", s.Raspon())
	}

	v := Val{
		Pocelo:    poc,
		Zavrsilo:  poc.Add(36 * time.Hour),
		VrhCm:     812.5,
		VrhKad:    poc.Add(20 * time.Hour),
		Korak:     "dnevni",
		Stupnjevi: []StupanjVala{{Stupanj: PhasePrep, TrajanjeIznad: 36 * time.Hour}, s},
	}
	for _, p := range []struct{ ime, got, want string }{
		{"početak", v.PoceloHR(), "3.6.2024."},
		{"kraj", v.ZavrsiloHR(), "5.6.2024."},
		{"vrh kad", v.VrhKadHR(), "4.6.2024."},
		{"vrh", v.VrhLabel(), "+813 cm"},
		{"trajanje", v.TrajanjeHR(), "36 sati"},
	} {
		if p.got != p.want {
			t.Errorf("%s: %q, očekivano %q", p.ime, p.got, p.want)
		}
	}
	if v.Trajanje() != 36*time.Hour || v.Raspon() != 36*time.Hour {
		t.Errorf("trajanje %v, raspon %v", v.Trajanje(), v.Raspon())
	}
	if v.NajviseDosegnuto() != PhaseRegular || v.Godina() != 2024 {
		t.Errorf("najviše %v, godina %d", v.NajviseDosegnuto(), v.Godina())
	}
	v.VrhCm = 812.4
	if v.VrhLabel() != "+812 cm" {
		t.Errorf("vrh %q", v.VrhLabel())
	}

	// val bez stupnjeva nije bio iznad pripremnog stanja
	prazan := Val{}
	if prazan.Trajanje() != 0 || prazan.TrajanjeHR() != "—" || prazan.NajviseDosegnuto() != PhaseNormal {
		t.Errorf("prazan val: %v, %q, %v", prazan.Trajanje(), prazan.TrajanjeHR(), prazan.NajviseDosegnuto())
	}
	if prazan.PoceloHR() != "—" {
		t.Errorf("prazan val bez početka: %q", prazan.PoceloHR())
	}
}

// Zaključava oznaku vala na rubu niza: val je na rubu čim mu bilo koji
// stupanj počinje ili završava na rubu podataka.
func TestValNaRubuAkoIjedanStupanjJest(t *testing.T) {
	for _, s := range []struct {
		ime       string
		stupnjevi []StupanjVala
		naRubu    bool
	}{
		{"bez stupnjeva", nil, false},
		{"nijedan na rubu", []StupanjVala{{}, {}}, false},
		{"viši počinje na rubu", []StupanjVala{{}, {PocetakNaRubu: true}}, true},
		{"niži završava na rubu", []StupanjVala{{KrajNaRubu: true}, {}}, true},
	} {
		if got := (Val{Stupnjevi: s.stupnjevi}).NaRubu(); got != s.naRubu {
			t.Errorf("%s: na rubu %v, očekivano %v", s.ime, got, s.naRubu)
		}
	}
}

// Zaključava udio razdoblja pod stupnjem: s decimalnim zarezom, vrlo mali ali
// postojeći udio kao „<0,1 %” umjesto nule, a bez razdoblja prazno.
func TestUdioStupnjaURazdoblju(t *testing.T) {
	for _, s := range []struct {
		ime       string
		iznad     time.Duration
		razdoblje time.Duration
		want      string
	}{
		{"desetina", 24 * time.Hour, 240 * time.Hour, "10,0 %"},
		{"trećina", time.Hour, 3 * time.Hour, "33,3 %"},
		{"cijelo razdoblje", 5 * time.Hour, 5 * time.Hour, "100,0 %"},
		{"vrlo malo", time.Hour, 10000 * time.Hour, "<0,1 %"},
		{"ništa", 0, 240 * time.Hour, "0,0 %"},
		{"bez razdoblja", time.Hour, 0, ""},
		{"negativno razdoblje", time.Hour, -time.Hour, ""},
	} {
		if got := (ZbrojStupnja{UkupnoIznad: s.iznad}).UdioHR(s.razdoblje); got != s.want {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.want)
		}
	}
	if got := zarez("1.234.5"); got != "1,234,5" {
		t.Errorf("zarez mijenja svaku točku: %q", got)
	}
}

// Zaključava ispis zbroja stupnja: ukupno u stanju, ukupno iznad praga i
// najdulje, svako kako se trajanje čita.
func TestIspisZbrojaStupnja(t *testing.T) {
	z := ZbrojStupnja{Ukupno: 433 * time.Hour, UkupnoIznad: 864 * time.Hour, Najdulji: 12 * time.Hour}
	if z.UkupnoHR() != "18 dana" || z.UkupnoIznadHR() != "36 dana" || z.NajduljeHR() != "12 sati" {
		t.Errorf("zbroj: %q, %q, %q", z.UkupnoHR(), z.UkupnoIznadHR(), z.NajduljeHR())
	}
}

// Zaključava razdoblje koje niz pokriva: trajanje, broj godina zaokružen na
// cijelu godinu i to da prazno ili obrnuto razdoblje ne pokriva ništa.
func TestRazdobljeNiza(t *testing.T) {
	od := time.Date(2013, 1, 1, 0, 0, 0, 0, Zagreb)
	do := time.Date(2023, 1, 1, 0, 0, 0, 0, Zagreb)
	r := RazdobljeNiza{Od: od, Do: do, Zapisa: 3653}
	if !r.Ima() || r.Trajanje() != do.Sub(od) || r.Godina() != 10 {
		t.Errorf("razdoblje: ima %v, trajanje %v, godina %d", r.Ima(), r.Trajanje(), r.Godina())
	}
	if g := (RazdobljeNiza{Od: od, Do: od.AddDate(1, 5, 0)}).Godina(); g != 1 {
		t.Errorf("godina i pet mjeseci: %d godina, očekivana 1", g)
	}
	if g := (RazdobljeNiza{Od: od, Do: od.AddDate(1, 7, 0)}).Godina(); g != 2 {
		t.Errorf("godina i sedam mjeseci: %d godina, očekivane 2", g)
	}
	for _, s := range []struct {
		ime string
		r   RazdobljeNiza
	}{
		{"prazno", RazdobljeNiza{}},
		{"bez početka", RazdobljeNiza{Do: do}},
		{"isti trenutak", RazdobljeNiza{Od: od, Do: od}},
		{"kraj prije početka", RazdobljeNiza{Od: do, Do: od}},
	} {
		if s.r.Ima() {
			t.Errorf("%s: razdoblje ne smije ništa pokrivati", s.ime)
		}
	}
}

// Zaključava presjek s pragom: točka prijelaza nikad ne izlazi izvan para
// mjerenja, a kad se vodostaj između njih ne mijenja, prijelaz je na prvom.
func TestPrijelazOstajeIzmeduMjerenja(t *testing.T) {
	a := time.Date(2013, 6, 1, 0, 0, 0, 0, Zagreb)
	b := a.Add(24 * time.Hour)
	for _, s := range []struct {
		ime    string
		va, vb float64
		prag   float64
		want   time.Time
	}{
		{"na četvrtini", 200, 600, 300, a.Add(6 * time.Hour)},
		{"pad na pola", 400, 200, 300, a.Add(12 * time.Hour)},
		{"ravno", 300, 300, 300, a},
		{"prag ispod oba", 400, 600, 300, a},
		{"prag iznad oba", 200, 250, 300, b},
	} {
		got := izmedu(HidroTocka{Kad: a, Vrijednost: s.va}, HidroTocka{Kad: b, Vrijednost: s.vb}, s.prag)
		if !got.Equal(s.want) {
			t.Errorf("%s: %v, očekivano %v", s.ime, got, s.want)
		}
	}
}

// Zaključava rub kad u rasponu nema mjerenja iznad praga: prijelaz naviše je
// na prvom, a pad na zadnjem mjerenju raspona, oba označena kao rub.
func TestPrijelazBezMjerenjaIznadPraga(t *testing.T) {
	p := time.Date(2013, 6, 1, 0, 0, 0, 0, Zagreb)
	niz := dnevni(p, 100, 200, 250, 200, 100)
	if kad, rub := prijelazGore(niz, 1, 3, 300); !kad.Equal(niz[1].Kad) || !rub {
		t.Errorf("naviše: %v, rub %v", kad, rub)
	}
	if kad, rub := prijelazDolje(niz, 1, 3, 300); !kad.Equal(niz[3].Kad) || !rub {
		t.Errorf("naniže: %v, rub %v", kad, rub)
	}
}

// Zaključava riječi uz broj sati i dana i ispis kratkih trajanja: 11 do 14
// idu s „sati” i „dana”, i kad završavaju na jedan do četiri; trajanje kraće
// od pola sata je „<1 sat”, a negativno se ne ispisuje.
func TestTrajanjeRijeciIKratkaTrajanja(t *testing.T) {
	for _, s := range []struct {
		d    time.Duration
		want string
	}{
		{20 * time.Minute, "<1 sat"},
		{-time.Hour, "—"},
		{11 * time.Hour, "11 sati"},
		{12 * time.Hour, "12 sati"},
		{14 * time.Hour, "14 sati"},
		{22 * time.Hour, "22 sata"},
		{24 * 11 * time.Hour, "11 dana"},
		{24 * 13 * time.Hour, "13 dana"},
		{24 * 101 * time.Hour, "101 dan"},
		{24 * 111 * time.Hour, "111 dana"},
		{24 * 1021 * time.Hour, "1.021 dan"},
	} {
		if got := trajanjeHR(s.d); got != s.want {
			t.Errorf("%v → %q, očekivano %q", s.d, got, s.want)
		}
	}
	for _, s := range []struct {
		n    int
		want string
	}{
		{-5, "-5"}, {-14966, "-14.966"}, {-1000000, "-1.000.000"}, {0, "0"},
	} {
		if got := grupiraj(s.n); got != s.want {
			t.Errorf("grupiraj(%d) = %q, očekivano %q", s.n, got, s.want)
		}
	}
}
