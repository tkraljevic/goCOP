package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
)

// Datoteka stiže u kakvom god obliku izvor daje. Vrata je moraju prepoznati,
// jer je jedina druga mogućnost da netko svaki put piše novi pretvarač.
func TestVrataPrepoznajuRazliciteOblike(t *testing.T) {
	for _, sluc := range []struct {
		ime      string
		sadrzaj  string
		razdjeln string
		vrijeme  int
		vrijedn  int
		redaka   int
	}{
		{"nase.csv", "vrijeme_utc;vodostaj_cm\n2013-06-14 05:00:00;776\n2013-06-14 06:00:00;774\n", ";", 0, 1, 2},
		{"zarez.csv", "datum,vodostaj\n2013-06-14,776\n2013-06-15,774\n", ",", 0, 1, 2},
		{"tab.csv", "vrijeme\tvodostaj\n2013-06-14 05:00\t776\n2013-06-14 06:00\t774\n", "\t", 0, 1, 2},
		{"hrvatski.csv", "Datum;Vodostaj (cm)\n14.06.2013.;776,0\n15.06.2013.;774,5\n", ";", 0, 1, 2},
		{"visestupaca.csv", "postaja;datum;vodostaj;protok\nBatina;2013-06-14;776;5100\nBatina;2013-06-15;774;5050\n", ";", 1, 2, 2},
		{"komentar.csv", "# izvoz iz sustava\nvrijeme_utc;vodostaj_cm\n2013-06-14 05:00:00;776\n", ";", 0, 1, 1},
	} {
		t.Run(sluc.ime, func(t *testing.T) {
			p, tijelo, err := pogodi(sluc.ime, []byte(sluc.sadrzaj))
			if err != nil {
				t.Fatal(err)
			}
			if p.Razdjelnik != sluc.razdjeln {
				t.Errorf("razdjelnik %q, očekivan %q", p.Razdjelnik, sluc.razdjeln)
			}
			if p.StupacVrijeme != sluc.vrijeme || p.StupacVrijednost != sluc.vrijedn {
				t.Errorf("stupci: vrijeme %d, vrijednost %d; očekivano %d i %d",
					p.StupacVrijeme, p.StupacVrijednost, sluc.vrijeme, sluc.vrijedn)
			}
			if len(tijelo) != sluc.redaka {
				t.Errorf("redaka %d, očekivano %d", len(tijelo), sluc.redaka)
			}
		})
	}
}

// Zona se primjenjuje na sat, a ne na dan: dan je dan bez obzira odakle se
// gleda. Ovo je razlika koja na naglom porastu vrijedi i po metar.
func TestZonaSeTiceSataANeDana(t *testing.T) {
	_, tijelo, err := pogodi("x.csv", []byte("vrijeme;vodostaj\n2013-06-14 05:00:00;776\n"))
	if err != nil {
		t.Fatal(err)
	}
	u := UvozNiza{Velicina: "vodostaj", Zona: "Europe/Zagreb", StupacVrijeme: 0, StupacVrijednost: 1}
	redci, _, err := pretvori(tijelo, u)
	if err != nil {
		t.Fatal(err)
	}
	// Ljeti je Zagreb UTC+2, pa 05:00 po Zagrebu je 03:00 UTC.
	if got := redci[0].Vrijeme.UTC().Format("2006-01-02 15:04"); got != "2013-06-14 03:00" {
		t.Errorf("sat u zoni izvora: %s", got)
	}

	_, tijeloDan, _ := pogodi("d.csv", []byte("datum;vodostaj\n2013-06-14;776\n"))
	redci, _, err = pretvori(tijeloDan, u)
	if err != nil {
		t.Fatal(err)
	}
	if got := redci[0].Vrijeme.UTC().Format("2006-01-02 15:04"); got != "2013-06-14 00:00" {
		t.Errorf("dan se ne pomiče zonom: %s", got)
	}
	if !redci[0].PoDanu {
		t.Error("redak samo s datumom mora biti označen kao dnevni")
	}
}

// Vrata odbijaju ono što bi gradnja ionako odbacila — po istom pravilu, da
// čovjek ne upiše nešto što se poslije tiho izgubi.
func TestVrataOdbijajuNemoguceINeprocitljivo(t *testing.T) {
	_, tijelo, err := pogodi("x.csv", []byte(
		"vrijeme;vodostaj\n2013-06-14 05:00:00;776\n2013-06-14 06:00:00;99999\nnedatum;12\n2013-06-14 07:00:00;xyz\n2013-06-14 05:00:00;700\n"))
	if err != nil {
		t.Fatal(err)
	}
	redci, preskoceno, err := pretvori(tijelo, UvozNiza{Velicina: "vodostaj", StupacVrijeme: 0, StupacVrijednost: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Od pet redaka prolazi jedan: 99999 cm nije moguć vodostaj, "nedatum" se
	// ne čita kao vrijeme, "xyz" ne kao broj, a zadnji ponavlja trenutak koji
	// je već uzet.
	if len(redci) != 1 || redci[0].Vrijednost != 776 {
		t.Errorf("primljeno %d redaka, očekivan samo onaj od 776 cm", len(redci))
	}
	if preskoceno != 4 {
		t.Errorf("preskočeno %d, očekivano 4", preskoceno)
	}
}

// Upis slaže kanonsko ime i miče staru datoteku istog niza, jer bi ju gradnja
// inače pročitala kao dodatne godine.
func TestUpisSlazeKanonskoImeIMicePrijasnju(t *testing.T) {
	koren := t.TempDir()
	stara := filepath.Join(koren, "dunav", "batina")
	if err := os.MkdirAll(stara, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stara, "batina_dhmz_vodostaj_satni_2013.csv"), []byte("staro"), 0o644); err != nil {
		t.Fatal(err)
	}
	redci := []arhiva.Redak{
		{Vrijeme: time.Date(2013, 6, 14, 5, 0, 0, 0, time.UTC), Vrijednost: 776},
		{Vrijeme: time.Date(2014, 1, 2, 6, 0, 0, 0, time.UTC), Vrijednost: 774.5},
	}
	put, err := arhiva.Upisi(koren, "dunav", "batina", "dhmz", "vodostaj", "satni", redci)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(put) != "batina_dhmz_vodostaj_satni_2013-2014.csv" {
		t.Errorf("ime %q", filepath.Base(put))
	}
	if _, err := os.Stat(filepath.Join(stara, "batina_dhmz_vodostaj_satni_2013.csv")); err == nil {
		t.Error("prijašnja datoteka istog niza je ostala — gradnja bi ju čitala kao dodatne godine")
	}
	b, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := "vrijeme_utc;vodostaj_cm\n2013-06-14 05:00:00;776\n2014-01-02 06:00:00;774.5\n"
	if string(b) != ocekivano {
		t.Errorf("zapisano:\n%q\nočekivano:\n%q", string(b), ocekivano)
	}
}

// Donja crta razlomila bi ugovor o nazivu, pa se ne prima ni u jednom dijelu.
func TestNazivNePrimaDonjuCrtu(t *testing.T) {
	if err := arhiva.ProvjeriDjelove("donji_miholjac", "dhmz", "vodostaj", "satni"); err == nil {
		t.Error("donja crta u nazivu letve je prošla")
	}
	if err := arhiva.ProvjeriDjelove("batina", "dhmz", "razina", "satni"); err == nil {
		t.Error("nepoznata veličina je prošla")
	}
	if err := arhiva.ProvjeriDjelove("batina", "dhmz", "vodostaj", "satni"); err != nil {
		t.Errorf("ispravni dijelovi odbijeni: %v", err)
	}
	if !strings.Contains(strings.Join(arhiva.Velicine, ","), "vodostaj") {
		t.Error("popis veličina ne sadrži vodostaj")
	}
}

// HIS2000 poravnava stupce razmacima i daje sat bez minuta, a iznad podataka
// stavlja naslov bez razdjelnika. Prva izvedba je zbog tog naslova odbacila
// točku-zarez i uzela zarez, pa se nije čitalo ništa.
func TestVrataCitajuHIS2000(t *testing.T) {
	sadrzaj := "Satni podaci postaje VUKOVAR  za godinu 2001,  VODOSTAJ  (cm)\r\n" +
		" 1. 1.2001  0;117;\r\n 1. 1.2001  1;118;\r\n 1. 1.2001  2;119;\r\n" +
		"31.12.2026 23;;\r\n"
	p, tijelo, err := pogodi("satni.csv", []byte(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	if p.Razdjelnik != ";" {
		t.Errorf("razdjelnik %q — naslov bez razdjelnika ne smije odlučivati", p.Razdjelnik)
	}
	if p.StupacVrijeme != 0 || p.StupacVrijednost != 1 {
		t.Errorf("stupci %d i %d", p.StupacVrijeme, p.StupacVrijednost)
	}
	redci, presk, err := pretvori(tijelo, UvozNiza{Velicina: "vodostaj", Zona: "Europe/Zagreb",
		StupacVrijeme: 0, StupacVrijednost: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Zadnji redak ima vrijeme bez vrijednosti: mjerenja nema, nije greška.
	if len(redci) != 3 || presk != 0 {
		t.Fatalf("redaka %d, preskočeno %d", len(redci), presk)
	}
	if got := redci[0].Vrijeme.UTC().Format("2006-01-02 15:04"); got != "2000-12-31 23:00" {
		t.Errorf("prvi zapis %s — 1.1.2001. u 0 h po Zagrebu je 31.12.2000. u 23 h UTC", got)
	}
}

// Dnevni izvoz HIS2000 iznad podataka ima naslov, prazan redak i blok s
// metapodacima. Početak podataka mora se naći, a ne pretpostaviti.
func TestVrataNalazePocetakIspodMetapodataka(t *testing.T) {
	sadrzaj := "Dnevni podaci postaje VUKOVAR - DUNAV,  VODOSTAJ  (cm)\r\n\r\n" +
		"Šifra;Naziv;Vodotok;Godina podataka;\r\n5070;VUKOVAR;DUNAV;1900-2026;\r\n" +
		"01.01.1900;120;\r\n02.01.1900;138;\r\n03.01.1900;170;\r\n"
	p, tijelo, err := pogodi("dnevni.csv", []byte(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	if len(tijelo) != 3 {
		t.Fatalf("tijelo ima %d redaka, očekivana tri", len(tijelo))
	}
	if p.StupacVrijeme != 0 || p.StupacVrijednost != 1 {
		t.Errorf("stupci %d i %d", p.StupacVrijeme, p.StupacVrijednost)
	}
	redci, _, err := pretvori(tijelo, UvozNiza{Velicina: "vodostaj",
		StupacVrijeme: 0, StupacVrijednost: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 3 || !redci[0].PoDanu {
		t.Errorf("pročitano %d redaka, poDanu=%v", len(redci), redci[0].PoDanu)
	}
}

// Vrsta bilješke ima posljedicu na brojke, pa se prima samo ono što je na
// zatvorenom popisu — ne što god stigne iz obrasca.
func TestVrstaBiljeskeSePrimaSamoSPopisa(t *testing.T) {
	for _, v := range []string{"VRH", "dno", " Procjena ", "NEPOUZDANO"} {
		if vrstaBiljeskeIz(v) == "" {
			t.Errorf("%q je s popisa a odbijeno", v)
		}
	}
	for _, v := range []string{"", "REKORD", "'; DROP TABLE readings", "vrh vala"} {
		if got := vrstaBiljeskeIz(v); got != "" {
			t.Errorf("%q je primljeno kao %q", v, got)
		}
	}
}
