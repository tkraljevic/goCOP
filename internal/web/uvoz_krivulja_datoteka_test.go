package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Uz izvoz HIS-2000 obrazac krivulja prima i datoteku krivulja u obliku
// arhive, npr. od druge vodoprivrede. Brojke i služba su izmišljene.

const zaglavljeKrivuljaTesta = "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n"

// krivuljeSusjeda su dvije krivulje susjedne službe; zadnja nema kraj.
const krivuljeSusjeda = zaglavljeKrivuljaTesta +
	"2001-01-01;2009-12-31;100;500;polinom;41;300;600;;Susjedna vodoprivreda;probna napomena\n" +
	"2001-01-01;2009-12-31;0;100;potencija;50;2;0.5;10;Susjedna vodoprivreda;probna napomena\n" +
	"2010-01-01;;0;300;polinom;42;300;600;;Susjedna vodoprivreda;\n"

// Pregled datoteke krivulja kaže izvor iz datoteke i spaja je sa zatečenim
// krivuljama kao izvoz: isto razdoblje se mijenja, ostala ostaju. Pregled
// izvoza HIS-2000 kaže DHMZ.
func TestPregledKrivuljaIzDatotekeKrivulja(t *testing.T) {
	o := novaOkolinaVrata(t)
	hq := filepath.Join(o.koren, "primjerica", "primjerovo", "hq")
	if err := os.MkdirAll(hq, 0o755); err != nil {
		t.Fatal(err)
	}
	zateceno := zaglavljeKrivuljaTesta +
		"1990-01-01;2000-12-31;0;400;polinom;30;200;500;;DHMZ, HIS-2000;\n" +
		"2001-01-01;;100;500;polinom;40;300;600;;DHMZ, HIS-2000;\n"
	if err := os.WriteFile(filepath.Join(hq, "primjerovo_hq_krivulje.csv"), []byte(zateceno), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := pregledKrivulja("susjed.csv", []byte(krivuljeSusjeda), "primjerovo", o.koren)
	if err != nil {
		t.Fatal(err)
	}
	if p.Izvor != "Susjedna vodoprivreda" || p.Krivulja != 2 || p.Odsjecaka != 3 || p.Potencija != 1 ||
		p.Od != "2001-01-01" || p.Do != "" || p.KotaNule != "" {
		t.Errorf("pregled: %+v", p)
	}
	if p.Ukupno != 3 || strings.Join(p.Razlikuju, ",") != "2001-01-01" || strings.Join(p.Ostaju, ",") != "1990-01-01" {
		t.Errorf("spajanje: ukupno %d, razlikuju %v, ostaju %v", p.Ukupno, p.Razlikuju, p.Ostaju)
	}
	if len(p.Popis) != 2 || p.Popis[0].Do != "2009-12-31" || p.Popis[1].Do != "" || p.Popis[0].Odsjecci[1].P3 != 0.5 {
		t.Errorf("popis krivulja: %+v", p.Popis)
	}

	his, err := pregledKrivulja("krivulje.csv", []byte(krivuljePrimjerova), "drugovo", o.koren)
	if err != nil || his.Izvor != "DHMZ, HIS-2000" {
		t.Errorf("izvor izvoza HIS-2000: %+v, %v", his, err)
	}

	_, err = pregledKrivulja("susjed.csv", []byte(zaglavljeKrivuljaTesta+"2001-01-01;;nula;100;polinom;1;2;3;;;\n"), "primjerovo", o.koren)
	if err == nil || !strings.HasPrefix(err.Error(), "datoteka krivulja ne da se pročitati: redak 2: ") {
		t.Errorf("nečitljiv redak poslane datoteke: %v", err)
	}
}

// Potvrda zapisuje krivulje iz datoteke s njihovim izvorom i napomenom, i
// letvi koja nema nijedan niz. Stranica s pregledom pokazuje izvor.
func TestKrivuljeIzDatotekePregledPaUpis(t *testing.T) {
	o := novaOkolinaVrata(t)
	r := viseDijelni(t, "/administracija/uvoz-krivulja/pregled", map[string]string{"letva": "primjerovo"},
		datotekaObrasca{"datoteka", "susjed.csv", krivuljeSusjeda})
	html := odgovor(o.h.PregledUvozaKrivulja, r, o.pero, o.peroOvl).Body.String()
	if !strings.Contains(htmlUTekst(html), "Susjedna vodoprivreda") {
		t.Errorf("pregled ne kaže izvor:\n%s", html[max(0, len(html)-2000):])
	}
	id := regexp.MustCompile(`name="id" value="([^"]+)"`).FindStringSubmatch(html)
	if id == nil {
		t.Fatal("pregled nije dao broj za potvrdu")
	}

	w := odgovor(o.h.UpisiKrivulje, obicniObrazac("/administracija/uvoz-krivulja/upisi", "letva=primjerovo&id="+id[1]), o.pero, o.peroOvl)
	p := posaoSaStranice(t, o.h.poslovi, htmlUTekst(w.Body.String()), o.pero)
	if p.Greska() != "" {
		t.Fatalf("posao upisa krivulja: %q", p.Greska())
	}
	hq := filepath.Join(o.koren, "primjerica", "primjerovo", "hq")
	if got := procitaj(t, filepath.Join(hq, "primjerovo_hq_krivulje.csv")); got != krivuljeSusjeda {
		t.Errorf("zapisane krivulje:\n%s", got)
	}
	if got := procitaj(t, filepath.Join(hq, "izvornik", "susjed.csv")); got != krivuljeSusjeda {
		t.Error("izvornik nije spremljen uz krivulje")
	}
}
