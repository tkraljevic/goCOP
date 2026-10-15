package models

import (
	"strings"
	"testing"
)

// Zaključava koji je prvi redak zaglavlje datoteke krivulja: imena se
// uspoređuju bez oznake BOM, razmaka i velikih slova, a p4, oblik, izvor i
// napomena smiju nedostajati. Prazno ime smije biti, jer Excel i LibreOffice
// redak znaju završiti točkom-zarezom. Ime koje nije ime stupca krivulje,
// stupac bez kojeg se krivulja ne čita, razdoblje koje nije u prva dva
// stupca i znak CR u imenu znače da to nije zaglavlje. Poruka navodi prvi
// redak bez oznake BOM, a CR i LF u njemu kao razmak, da se vidi kad su
// redci u jednom retku; dulji redak skrati na 80 znakova.
func TestZastoNijeZaglavljeKrivulja(t *testing.T) {
	const morajuStupci = "”). Prvi redak mora imati imena stupaca, npr. " + ZaglavljeDatotekeKrivulja +
		", a svaki redak mora biti u svom retku datoteke"
	dug := ZaglavljeDatotekeKrivulja + "\r2001-01-01;;-100;600;polinom;1;2;3;;DHMZ;\n2011-01-01"
	for _, s := range []struct{ ime, redak, zeli string }{
		{"današnje zaglavlje", ZaglavljeDatotekeKrivulja, ""},
		{"BOM, razmaci i velika slova", "\ufeffVrijedi_od; vrijedi_do ;OD_CM;do_cm;oblik;p1;p2;p3; P4 ;izvor;napomena", ""},
		{"samo obavezni stupci", "vrijedi_od;vrijedi_do;od_cm;do_cm;p1;p2;p3", ""},
		{"zaglavlje sa ; na kraju", ZaglavljeDatotekeKrivulja + ";", ""},
		{"prazno ime usred zaglavlja", "vrijedi_od;vrijedi_do;od_cm;do_cm; ;p1;p2;p3", ""},
		{"CR iza imena", ZaglavljeDatotekeKrivulja + "\r;;;",
			"nije zaglavlje kakvo treba („" + ZaglavljeDatotekeKrivulja + " ;;;" + morajuStupci},
		{"BOM ispred lošeg zaglavlja", "\ufeffvrijedi_od;vrijedi_do;od_cm;p1;p2;p3",
			"nije zaglavlje kakvo treba („vrijedi_od;vrijedi_do;od_cm;p1;p2;p3" + morajuStupci},
		{"jedno polje", "vrijedi_od", "nije zaglavlje kakvo treba („vrijedi_od" + morajuStupci},
		{"nepoznat stupac", "vrijedi_od;vrijedi_do;od_cm;do_cm;p1;p2;p3;p5",
			"nije zaglavlje kakvo treba („vrijedi_od;vrijedi_do;od_cm;do_cm;p1;p2;p3;p5" + morajuStupci},
		{"bez stupca do_cm", "vrijedi_od;vrijedi_do;od_cm;p1;p2;p3",
			"nije zaglavlje kakvo treba („vrijedi_od;vrijedi_do;od_cm;p1;p2;p3" + morajuStupci},
		{"kraj prije početka", "vrijedi_do;vrijedi_od;od_cm;do_cm;p1;p2;p3",
			"nije zaglavlje kakvo treba („vrijedi_do;vrijedi_od;od_cm;do_cm;p1;p2;p3" + morajuStupci},
		{"redci u jednom retku", dug,
			"nije zaglavlje kakvo treba („" + ZaglavljeDatotekeKrivulja + " 2001-01-01;;-" + "…" + morajuStupci},
		{"kratak redak s LF", "a\nb", "nije zaglavlje kakvo treba („a b" + morajuStupci},
	} {
		if got := ZastoNijeZaglavljeKrivulja(strings.Split(s.redak, ";")); got != s.zeli {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.zeli)
		}
	}
	if got := ZastoNijeZaglavljeKrivulja(nil); got != "nije zaglavlje kakvo treba („"+morajuStupci {
		t.Errorf("bez ijednog polja: %q", got)
	}
}

// Zaključava provjeru polja odsječka po stupcima iz zaglavlja: granice su
// cijeli brojevi, p1 do p4 brojevi s decimalnim zarezom ili točkom, ili
// prazni, a oblik polinom, potencija ili prazan. Stupac koji zaglavlje ili
// redak nema ne provjerava se. Razlog kaže prvi stupac koji ne valja i što u
// njemu stoji. Znak CR u bilo kojem polju znači da je u retku i sljedeći
// redak.
func TestZastoNijeOdsjecakKrivulje(t *testing.T) {
	stupci := map[string]int{}
	for i, ime := range strings.Split(ZaglavljeDatotekeKrivulja, ";") {
		stupci[ime] = i
	}
	bezP4 := map[string]int{"vrijedi_od": 0, "vrijedi_do": 1, "od_cm": 2, "do_cm": 3, "oblik": 4,
		"p1": 5, "p2": 6, "p3": 7, "izvor": 8, "napomena": 9}
	const zarez = ", npr. zbog zareza umjesto točke-zareza"
	for _, s := range []struct {
		ime    string
		stupci map[string]int
		redak  string
		zeli   string
	}{
		{"ispravan", stupci, "2001-01-01;; -130 ;146;polinom;5,71;214.14;-414,32;;DHMZ;", ""},
		{"potencija i prazni brojevi", stupci, "2001-01-01;;0;300;potencija;10;;;;DHMZ;", ""},
		{"prazan oblik, bez napomene", stupci, "2001-01-01;;0;300;;1;2;3;4", ""},
		{"bez stupca p4", bezP4, "2001-01-01;;0;300;polinom;1;2;3;DHMZ;", ""},
		{"od_cm sa zarezom", stupci, "2001-01-01;;-100,600;polinom;4;5;6;;DHMZ;",
			"u stupcu od_cm nema cijelog broja, nego „-100,600”" + zarez},
		{"prazan do_cm", stupci, "2001-01-01;;-100;;polinom;4;5;6;;DHMZ;",
			"u stupcu do_cm nema cijelog broja, nego je stupac prazan" + zarez},
		{"decimalni do_cm", stupci, "2001-01-01;;-100;600,5;polinom;4;5;6;;DHMZ;",
			"u stupcu do_cm nema cijelog broja, nego „600,5”" + zarez},
		{"oblik", stupci, "2001-01-01;;-100;600;Potencija;4;5;6;;DHMZ;",
			"u stupcu oblik ima „Potencija”, a smije imati samo polinom ili potencija, malim slovima"},
		{"CR u napomeni", stupci, "2001-01-01;;-100;600;polinom;4;5;6;;DHMZ;\r2011-01-01;;-100;600;polinom;4;5;6;;DHMZ;",
			"u sebi ima znak CR, pa je u jednom retku datoteke više redaka krivulje. Svaki redak mora biti u svom retku"},
		{"p1", stupci, "2001-01-01;;-100;600;polinom;1.234,5;5;6;;DHMZ;", "u stupcu p1 nema broja, nego „1.234,5”" + zarez},
		{"p2", stupci, "2001-01-01;;-100;600;polinom;4;5,6,7;6;;DHMZ;", "u stupcu p2 nema broja, nego „5,6,7”" + zarez},
		{"p3", bezP4, "2001-01-01;;-100;600;polinom;4,5;6;DHMZ;", "u stupcu p3 nema broja, nego „DHMZ”" + zarez},
		{"p4", stupci, "2001-01-01;;-100;600;polinom;4,5;6;;DHMZ;", "u stupcu p4 nema broja, nego „DHMZ”" + zarez},
	} {
		if got := ZastoNijeOdsjecakKrivulje(s.stupci, strings.Split(s.redak, ";")); got != s.zeli {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.zeli)
		}
	}
}

// Zaključava redak za poruku: polja odvojena točkom-zarezom, a CR i LF kao
// razmak, da poruka ostane u jednom retku.
func TestRedakKrivuljeZaPoruku(t *testing.T) {
	if got := RedakKrivuljeZaPoruku([]string{"2001-01-01", "DHMZ\r2011-01-01", "ručno\nprovjereno"}); got !=
		"2001-01-01;DHMZ 2011-01-01;ručno provjereno" {
		t.Errorf("%q", got)
	}
}

// Zaključava čitanje broja iz datoteke krivulja: decimalni zarez i točka
// čitaju se jednako, a tekst nije broj.
func TestBrojKrivulje(t *testing.T) {
	for _, s := range []struct {
		polje string
		zeli  float64
	}{{"645,83", 645.83}, {"645.83", 645.83}, {"-892", -892}} {
		if got, err := BrojKrivulje(s.polje); err != nil || got != s.zeli {
			t.Errorf("%q: %v (%v), očekivano %v", s.polje, got, err, s.zeli)
		}
	}
	if _, err := BrojKrivulje("DHMZ"); err == nil {
		t.Error("„DHMZ” pročitan je kao broj")
	}
}
