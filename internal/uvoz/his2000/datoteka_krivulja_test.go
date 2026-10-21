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
// Datoteka bez ijednog retka nema krivulja.
func TestDatotekaKrivuljaNecitljivRedak(t *testing.T) {
	_, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(ZaglavljeKrivulja+"\n"+
		redakSusjeda("2001-01-01", "", "21")+"2010-01-01;;nula;600;polinom;1;2;3;;"+izvorSusjeda+";\n"))
	var nr *NecitljivRedak
	if !errors.As(err, &nr) || nr.Redak != 3 || !strings.Contains(nr.Razlog, "od_cm") {
		t.Errorf("nečitljiv redak: %v", err)
	}
	s, err := ProcitajDatotekuKrivulja("susjed.csv", []byte(ZaglavljeKrivulja+"\n"))
	if err != nil || len(s.Krivulje) != 0 || s.IzvoriKrivulja() != nil {
		t.Errorf("datoteka bez redaka: %+v, %v", s, err)
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
