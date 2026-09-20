package main

import (
	"testing"
	"time"
)

// cp pretvara naš tekst u cp1250, kakav HIS i piše, da test ide kroz istu
// pretvorbu kroz koju ide i prava datoteka.
func cp(s string) []byte {
	obrnuto := map[rune]byte{}
	for b, r := range cp1250 {
		obrnuto[r] = b
	}
	var out []byte
	for _, r := range s {
		if r < 0x80 {
			out = append(out, byte(r))
			continue
		}
		if b, ok := obrnuto[r]; ok {
			out = append(out, b)
			continue
		}
		out = append(out, '?')
	}
	return out
}

// Datoteka se prepoznaje po zaglavlju, jer ime bira onaj tko izvozi: ista
// vrsta podatka dolazila je kao satni.csv, satniVODOSTAJI.csv i protoSatni.csv.
func TestPrepoznavanjePoZaglavlju(t *testing.T) {
	sluc := []struct {
		ime, sadrzaj      string
		velicina, gustoca string
		vrijednosti       int
	}{
		{"bilo.csv", "Satni podaci postaje DALJ  za godinu 2001,  VODOSTAJ  (cm)\r\n 1. 1.2001  0;117;\r\n 1. 1.2001  1;118;\r\n31.12.2001 23;;\r\n",
			"vodostaj", "satni", 2},
		{"drugo.csv", "Dnevni podaci postaje DALJ - DUNAV,  PROTOK  (m3/s)\r\n\r\nŠifra;Naziv;Vodotok;Godina podataka;\r\n5130;DALJ;DUNAV;2001-2001;\r\n01.01.2001;2223;\r\n02.01.2001;;\r\n",
			"protok", "srednjak", 1},
		{"trece.csv", "Dnevni podaci postaje BATINA - DUNAV,  KONCENTRACIJA  (g/m3)\r\n01.05.2018;12,4;\r\n",
			"koncentracija", "dnevni", 1},
	}
	for _, s := range sluc {
		sadrzaj, err := Procitaj(s.ime, cp(s.sadrzaj))
		if err != nil {
			t.Errorf("%s: %v", s.ime, err)
			continue
		}
		if sadrzaj.Vrsta.Velicina != s.velicina || sadrzaj.Vrsta.Gustoca != s.gustoca {
			t.Errorf("%s: %s/%s, očekivano %s/%s", s.ime,
				sadrzaj.Vrsta.Velicina, sadrzaj.Vrsta.Gustoca, s.velicina, s.gustoca)
		}
		if len(sadrzaj.Niz) != s.vrijednosti {
			t.Errorf("%s: %d vrijednosti, očekivano %d", s.ime, len(sadrzaj.Niz), s.vrijednosti)
		}
	}
}

// Sat koji u našoj zoni ne postoji — noć prelaska na ljetno vrijeme — HIS
// svejedno ispiše. Uzet doslovno, pri pretvorbi u UTC slio bi se sa sljedećim
// satom i pregazio ga, pa se preskače i prebroji.
func TestSatKojiNePostojiSePreskace(t *testing.T) {
	sadrzaj := "Satni podaci postaje DALJ  za godinu 2026,  VODOSTAJ  (cm)\r\n" +
		"29. 3.2026  1;200;\r\n29. 3.2026  2;201;\r\n29. 3.2026  3;208;\r\n"
	s, err := Procitaj("satni.csv", cp(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	if s.Preskoceno != 1 {
		t.Errorf("preskočeno %d, očekivan jedan sat", s.Preskoceno)
	}
	if len(s.Niz) != 2 || s.Niz[1].Kad.Hour() != 3 || s.Niz[1].V != "208" {
		t.Errorf("ostalo %+v — smiju ostati samo 1 h i 3 h", s.Niz)
	}
}

// Krivulje nose razdoblje valjanosti i odsječke po rasponu vodostaja, a uz
// njih stoji jedino mjesto u izvozu gdje HIS napiše kotu nule i koordinate.
func TestKrivuljeIZaglavljePostaje(t *testing.T) {
	sadrzaj := "Krivulje protoka\r\nŠifra;Podsifra;Stare šifre;Naziv;Vodotok;Kota \"0\";Širina;Dužina;\r\n" +
		"5130;1;;DALJ;DUNAV;75,20;45 29 28;18 59 42;\r\n\r\n" +
		"1/1/2001 - 12/31/2009\r\nTip;H1;H2;A;B;C;D;\r\n" +
		"1;100,0000;500,0000;43,2420;349,3200;700,0000;0,0000;\r\n" +
		"1;500,0000;1000,0000;39,8040;334,4700;860,2200;0,0000;\r\n\r\n" +
		"1/1/2010 - 12/31/2012\r\nTip;H1;H2;A;B;C;D;\r\n" +
		"1;0,0000;300,0000;43,2420;349,3200;700,0000;0,0000;\r\n"
	s, err := Procitaj("krivulje.csv", cp(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Krivulje) != 2 || len(s.Krivulje[0].Odsjecci) != 2 || len(s.Krivulje[1].Odsjecci) != 1 {
		t.Fatalf("krivulje: %+v", s.Krivulje)
	}
	prvi := s.Krivulje[0].Odsjecci[0]
	if prvi.OdCm != 100 || prvi.DoCm != 500 || prvi.P1 != "43,242" || prvi.P2 != "349,32" || prvi.P3 != "700" {
		t.Errorf("prvi odsječak: %+v — nule iza zareza ništa ne kažu i skidaju se", prvi)
	}
	if s.Postaja.KotaNule != "75,20" || s.Postaja.Sirina != "45 29 28" {
		t.Errorf("zaglavlje postaje: %+v", s.Postaja)
	}
	if !s.Krivulje[1].Od.Equal(time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("razdoblje druge krivulje: %s — datum je u obliku mjesec/dan/godina", s.Krivulje[1].Od)
	}
}

// Snimka korita nosi datum mjerenja i vodostaj pri mjerenju; bez njih se ne
// zna na što se visine odnose, pa se takva datoteka odbija.
func TestProfilKorita(t *testing.T) {
	sadrzaj := "Mjerenje poprečnog profila korita;\r\n" +
		"Šifra;Podsifra;Stare šifre;Naziv;Vodotok;Kota \"0\";Širina;Dužina;Vodostaj;Datum mjerenja;\r\n" +
		"5170;1;;BATINA;DUNAV;80.45;45 50 45;18 51 17;174;3/22/2010;\r\n\r\n" +
		"Stacionaža;Visina\r\n0,00;90,113;\r\n6,74;87,593;\r\n14,07;85,763;\r\n"
	s, err := Procitaj("profil1.csv", cp(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profil
	if p.Vodostaj != 174 || p.KotaNule != "80.45" {
		t.Errorf("zaglavlje snimke: %+v", p)
	}
	if !p.Datum.Equal(time.Date(2010, 3, 22, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("datum mjerenja: %s", p.Datum)
	}
	if len(p.Tocke) != 3 || p.Tocke[2].Stacionaza != "14,07" || p.Tocke[2].Visina != "85,763" {
		t.Errorf("točke: %+v", p.Tocke)
	}

	bezDatuma := "Mjerenje poprečnog profila korita;\r\n0,00;90,113;\r\n6,74;87,593;\r\n14,07;85,763;\r\n"
	if _, err := Procitaj("profil2.csv", cp(bezDatuma)); err == nil {
		t.Error("snimka bez datuma mjerenja mora biti odbijena")
	}
}

// Isto mjerenje zapisano s različito mnogo decimala nije nova vrijednost.
func TestUsporedbaIdePoBroju(t *testing.T) {
	kad := time.Date(2001, 3, 9, 0, 0, 0, 0, time.UTC)
	stari := map[time.Time]string{kad: "1919,000"}
	suk, samo, _ := usporedi(stari, []Vrijednost{{Kad: kad, V: "1919"}})
	if suk != 0 || samo != 0 {
		t.Errorf("sukoba %d, samo u starom %d — 1919,000 i 1919 isto su mjerenje", suk, samo)
	}
	suk, _, _ = usporedi(stari, []Vrijednost{{Kad: kad, V: "1920"}})
	if suk != 1 {
		t.Errorf("sukoba %d — prava razlika mora se vidjeti", suk)
	}

	// zatečena datoteka zna imati sat koji u našoj zoni ne postoji, jer je
	// nastala prije nego što se to znalo; njegov izostanak nije gubitak
	ljetni := time.Date(2026, 3, 29, 2, 0, 0, 0, time.UTC)
	pravi := time.Date(2026, 3, 29, 3, 0, 0, 0, time.UTC)
	_, samo, nepostojeci := usporedi(
		map[time.Time]string{ljetni: "207", pravi: "208"},
		[]Vrijednost{{Kad: pravi, V: "208"}})
	if samo != 0 || nepostojeci != 1 {
		t.Errorf("samo u starom %d, nepostojećih %d — očekivano 0 i 1", samo, nepostojeci)
	}
}

// Istu veličinu HIS zna dati i kao ispis za čitanje: dani u redcima, mjeseci
// u stupcima. Vrijednosti su poravnate desno prema nazivu mjeseca, pa se
// čitaju po stupcu; razmaci se ne smiju brojati, jer prazan mjesec nema ništa.
func TestIspisPoMjesecima(t *testing.T) {
	sadrzaj := "Dnevni podaci postaje DALJ - DUNAV,  KONCENTRACIJA  (g/m3)\r\n\r\n" +
		"Šifra postaje: 5130\r\n\r\n" +
		"      2018      I     II    III     IV      V     VI    VII   VIII     IX      X     XI    XII\r\n" +
		"         1                               20.6   37.5   38.6   27.7   29.9   13.6   20.9   17.4\r\n" +
		"         2                               30.7   40.4   38.9   19.2   36.2   12.1   28.4   6.40\r\n" +
		"        31                               33.3          20.0   45.5          20.2          39.3\r\n" +
		"        NK                               20.6   28.9   20.0   12.7   13.0   4.34   5.00   4.16\r\n"
	s, err := Procitaj("nanosdnevni.txt", cp(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	if s.Vrsta.Velicina != "koncentracija" || s.Vrsta.Gustoca != "dnevni" {
		t.Fatalf("vrsta: %+v", s.Vrsta)
	}
	zeli := map[string]string{
		"2018-05-01": "20,6", "2018-06-01": "37,5", "2018-12-01": "17,4",
		"2018-05-02": "30,7", "2018-12-02": "6,40",
		"2018-05-31": "33,3", "2018-07-31": "20,0", "2018-12-31": "39,3",
	}
	imamo := map[string]string{}
	for _, v := range s.Niz {
		imamo[v.Kad.Format("2006-01-02")] = v.V
	}
	for d, v := range zeli {
		if imamo[d] != v {
			t.Errorf("%s: %q, očekivano %q", d, imamo[d], v)
		}
	}
	// 31. lipnja nema, a sažetak NK nije mjerenje
	if _, ima := imamo["2018-06-31"]; ima {
		t.Error("dan kojeg u mjesecu nema ušao je u niz")
	}
	// tri dana puta osam mjeseci koji imaju podatak, bez praznih polja
	if len(imamo) != 21 {
		t.Errorf("vrijednosti %d, očekivano 21: %v", len(imamo), imamo)
	}
}

// Satni ispis ima sate u stupcima; iz njega se ne čita, jer bi krivo
// poravnanje pomaknulo cijeli niz. Mora se javiti, a ne tiho preskočiti.
func TestSatniIspisSeOdbija(t *testing.T) {
	sadrzaj := "Satni podaci postaje DALJ  za godinu 2018,  KONCENTRACIJA  (g/m3)\r\n\r\n" +
		"             1     2     3     4     5\r\n\r\n 1.  1.\r\n 2.  1.\r\n"
	if _, err := Procitaj("nanos.txt", cp(sadrzaj)); err == nil {
		t.Error("satni ispis mora biti odbijen s objašnjenjem")
	}
}
