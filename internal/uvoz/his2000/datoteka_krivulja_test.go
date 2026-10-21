package his2000

import (
	"errors"
	"strings"
	"testing"
)

// Krivulje druge službe stižu već kao datoteka krivulja, sa svojim izvorom i
// napomenom. Brojke i službe su izmišljene.

const izvorSusjeda = "Susjedna vodoprivreda"

// redakSusjeda je redak datoteke krivulja susjedne službe.
func redakSusjeda(od, do, p1 string) string {
	return od + ";" + do + ";-100;600;polinom;" + p1 + ";200;300;;" + izvorSusjeda + ";probna napomena\n"
}

// Datoteka krivulja prepoznaje se po zaglavlju, i s oznakom BOM koju stavi
// Excel. Izvoz HIS-a, zaglavlje bez obaveznog stupca i prazna datoteka nisu
// datoteka krivulja.
func TestJeDatotekaKrivulja(t *testing.T) {
	for _, s := range []struct {
		sadrzaj string
		zeli    bool
	}{
		{ZaglavljeKrivulja + "\n" + redakSusjeda("2001-01-01", "", "10"), true},
		{"\ufeff" + ZaglavljeKrivulja + "\n", true},
		{"vrijedi_od;vrijedi_do;od_cm;do_cm;p1;p2;p3\n", true},
		{"Krivulje protoka\r\n\x8aifra;Podsifra;Kota \"0\";\r\n", false},
		{"vrijedi_od;vrijedi_do;od_cm;do_cm\n", false},
		{"datum;vodostaj\n", false},
		{"", false},
	} {
		if got := JeDatotekaKrivulja([]byte(s.sadrzaj)); got != s.zeli {
			t.Errorf("%q: %v, očekivano %v", s.sadrzaj, got, s.zeli)
		}
	}
}

// Datoteka krivulja spaja se sa zatečenim krivuljama kao izvoz: isto
// razdoblje uzima se iz datoteke, a zatečeno razdoblje kojeg u datoteci nema
// ostaje. Izvor i napomena ostaju iz datoteke, a zadnja krivulja ostaje
// otvorena i kad joj datoteka upiše kraj.
func TestDatotekaKrivuljaZadrzavaIzvorINapomenu(t *testing.T) {
	zateceno := ZaglavljeKrivulja + "\n" +
		redakKrivulje("1990-01-01", "2000-12-31", "10") +
		redakKrivulje("2001-01-01", "", "20")
	poslano := ZaglavljeKrivulja + "\n" +
		redakSusjeda("2010-01-01", "2012-12-31", "40") +
		redakSusjeda("2001-01-01", "2009-12-31", "21")
	s, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(poslano))
	if err != nil {
		t.Fatal(err)
	}
	sp, err := SpojiKrivulje([]byte(zateceno), s)
	if err != nil {
		t.Fatal(err)
	}
	zeli := ZaglavljeKrivulja + "\n" +
		redakKrivulje("1990-01-01", "2000-12-31", "10") +
		redakSusjeda("2001-01-01", "2009-12-31", "21") +
		redakSusjeda("2010-01-01", "", "40")
	if string(sp.CSV) != zeli {
		t.Errorf("spojene krivulje:\n%s", sp.CSV)
	}
	if sp.Krivulja != 3 || strings.Join(sp.Razlikuju, ",") != "2001-01-01" || strings.Join(sp.Ostaju, ",") != "1990-01-01" {
		t.Errorf("spajanje: %+v", sp)
	}
	if got := strings.Join(s.IzvoriKrivulja(), "|"); got != izvorSusjeda {
		t.Errorf("izvori krivulja: %q", got)
	}
	// Pregled vidi krivulje po redu, s krajem kakav piše u datoteci.
	if len(s.Krivulje) != 2 || !s.Krivulje[0].Od.Equal(dan(2001, 1, 1)) || !s.Krivulje[1].Do.Equal(dan(2012, 12, 31)) {
		t.Fatalf("krivulje za pregled: %+v", s.Krivulje)
	}
	if o := s.Krivulje[0].Odsjecci[0]; o.OdCm != -100 || o.DoCm != 600 || o.Oblik != "polinom" || o.P1 != "21" || o.P3 != "300" {
		t.Errorf("odsječak za pregled: %+v", o)
	}
}

// Zadnja krivulja datoteke nema stvaran kraj, pa zadrži kraj zatečene
// krivulje istog početka, kao i kod izvoza. Krivulja bez kraja u datoteci
// u pregledu nema kraj, a zatečena koja počinje usred nje javlja se.
func TestDatotekaKrivuljaZadnjaKaoIzvoz(t *testing.T) {
	zateceno := ZaglavljeKrivulja + "\n" +
		redakKrivulje("2001-01-01", "2009-12-31", "20") +
		redakKrivulje("2010-01-01", "", "30")
	s, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(ZaglavljeKrivulja+"\n"+redakSusjeda("2001-01-01", "2003-12-31", "21")))
	if err != nil {
		t.Fatal(err)
	}
	sp, err := SpojiKrivulje([]byte(zateceno), s)
	if err != nil {
		t.Fatal(err)
	}
	zeli := ZaglavljeKrivulja + "\n" +
		redakSusjeda("2001-01-01", "2009-12-31", "21") +
		redakKrivulje("2010-01-01", "", "30")
	if string(sp.CSV) != zeli {
		t.Errorf("spojene krivulje:\n%s", sp.CSV)
	}

	otvorena, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(ZaglavljeKrivulja+"\n"+redakSusjeda("2001-01-01", "", "21")))
	if err != nil {
		t.Fatal(err)
	}
	if !otvorena.Krivulje[0].Do.IsZero() {
		t.Errorf("otvorena krivulja u pregledu ima kraj %v", otvorena.Krivulje[0].Do)
	}
	sp, err = SpojiKrivulje([]byte(zateceno), otvorena)
	if err != nil || strings.Join(sp.Usred, ",") != "2010-01-01" {
		t.Errorf("zatečena krivulja usred otvorene: %v, %v", sp.Usred, err)
	}
}

// Redak koji gradnja ne bi pročitala odbija cijelu datoteku i kaže koji je.
func TestDatotekaKrivuljaNecitljivRedak(t *testing.T) {
	_, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(ZaglavljeKrivulja+"\n"+
		redakSusjeda("2001-01-01", "", "21")+"2010-01-01;;nula;600;polinom;1;2;3;;"+izvorSusjeda+";\n"))
	var nr *NecitljivRedak
	if !errors.As(err, &nr) || nr.Redak != 3 || !strings.Contains(nr.Razlog, "od_cm") {
		t.Errorf("nečitljiv redak: %v", err)
	}
}

// Poslana datoteka odbija se i zbog onoga što gradnja propušta: nije UTF-8
// (Excel na hrvatskom Windowsu sprema CSV kao Windows-1250), nema nijedne
// krivulje, kraj nije dan ili je prije početka, krivulja nema izvor. Bez
// izvora krivulja druge službe ne smije ući, ni pod DHMZ ni prazna.
func TestDatotekaKrivuljaOdbija(t *testing.T) {
	bezStupcaIzvora := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4\n2001-01-01;;-100;600;polinom;10;200;300;\n"
	for _, s := range []struct {
		opis, sadrzaj, poruka string
	}{
		{"Windows-1250", ZaglavljeKrivulja + "\n2001-01-01;;-100;600;polinom;10;200;300;;Ma\xf0arska;\n", "UTF-8"},
		{"samo zaglavlje", ZaglavljeKrivulja + "\n", "nema nijedne krivulje"},
		{"kraj nije dan", ZaglavljeKrivulja + "\n" + redakSusjeda("2001-01-01", "31.12.2005", "10"), "od 2001-01-01 ima kraj „31.12.2005”"},
		{"kraj prije početka", ZaglavljeKrivulja + "\n" + redakSusjeda("2001-01-01", "2000-12-31", "10"), "ima kraj „2000-12-31”"},
		{"prazan izvor", ZaglavljeKrivulja + "\n" + redakSusjeda("2001-01-01", "", "10") +
			"2010-01-01;;-100;600;polinom;20;200;300;;;napomena bez izvora\n", "od 2010-01-01 nema izvor"},
		{"bez stupca izvor", bezStupcaIzvora, "od 2001-01-01 nema izvor"},
	} {
		if _, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(s.sadrzaj)); err == nil || !strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: %v, očekivano %q", s.opis, err, s.poruka)
		}
	}
	// Kraj na dan početka je krivulja od jednog dana, i prolazi.
	if _, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(ZaglavljeKrivulja+"\n"+redakSusjeda("2001-01-01", "2001-01-01", "10"))); err != nil {
		t.Errorf("kraj na dan početka: %v", err)
	}
}

// Krivulja bez kraja koja u datoteci nije zadnja vrijedi do sljedeće iz
// datoteke, a ne do kraja zatečene krivulje istog početka. Zatečena krivulja
// koja počinje usred nje ostaje i javlja se u pregledu.
func TestDatotekaKrivuljaSrednjaBezKraja(t *testing.T) {
	zateceno := ZaglavljeKrivulja + "\n" +
		redakKrivulje("2001-01-01", "2003-12-31", "20") +
		redakKrivulje("2008-01-01", "", "30")
	s, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(ZaglavljeKrivulja+"\n"+
		redakSusjeda("2010-01-01", "", "41")+redakSusjeda("2001-01-01", "", "21")))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Krivulje[0].Do.Equal(dan(2009, 12, 31)) || !s.Krivulje[1].Do.IsZero() {
		t.Errorf("krajevi u pregledu: %v, %v", s.Krivulje[0].Do, s.Krivulje[1].Do)
	}
	sp, err := SpojiKrivulje([]byte(zateceno), s)
	if err != nil {
		t.Fatal(err)
	}
	zeli := ZaglavljeKrivulja + "\n" +
		redakSusjeda("2001-01-01", "2009-12-31", "21") +
		redakKrivulje("2008-01-01", "2009-12-31", "30") +
		redakSusjeda("2010-01-01", "", "41")
	if string(sp.CSV) != zeli {
		t.Errorf("spojene krivulje:\n%s", sp.CSV)
	}
	if strings.Join(sp.Usred, ",") != "2008-01-01" {
		t.Errorf("zatečena krivulja usred poslane: %v", sp.Usred)
	}
}

// Izvoz HIS-2000 i dalje upisuje DHMZ kao izvor, bez napomene.
func TestIzvozHISUpisujeDHMZ(t *testing.T) {
	s := izvozKrivulja("9001", godina(2001, 2009, "21"))
	if got := strings.Join(s.IzvoriKrivulja(), "|"); got != "DHMZ, HIS-2000" {
		t.Errorf("izvori izvoza: %q", got)
	}
	if got := string(KrivuljeCSV(s)); got != ZaglavljeKrivulja+"\n"+redakKrivulje("2001-01-01", "", "21") {
		t.Errorf("datoteka iz izvoza:\n%s", got)
	}
}
