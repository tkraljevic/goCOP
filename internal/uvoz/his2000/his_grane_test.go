package his2000

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Zaključava odbijanje datoteka koje se ne daju pročitati (prazna, krivulje
// bez razdoblja, nepoznata veličina, snimka s premalo točaka), dnevni datum
// s mjesecom na prvom mjestu, SYLK ćeliju bez vrijednosti i niz koji se ne
// da zapisati.

func TestProcitajOdbija(t *testing.T) {
	profil := "Mjerenje poprečnog profila korita;\r\nŠifra;Podsifra;Stare šifre;Naziv;Vodotok;Kota \"0\";Širina;Dužina;Vodostaj;Datum mjerenja;\r\n" +
		"5170;1;;BATINA;DUNAV;80.45;45 50 45;18 51 17;174;3/22/2010;\r\n0,00;90,113;\r\n6,74;87,593;\r\n"
	for ime, s := range map[string]struct{ sadrzaj, greska string }{
		"prazna":             {"", "datoteka je prazna"},
		"krivulje":           {"Krivulje protoka\r\nTip;H1;H2;A;B;C;D;\r\n1;100,0000;500,0000;43,2420;349,3200;700,0000;0,0000;\r\n", "u datoteci nema nijedne krivulje"},
		"satni brzina":       {"Satni podaci postaje DALJ  za godinu 2001,  BRZINA  (m/s)\r\n 1. 1.2001  0;1;\r\n", `nepoznata veličina "BRZINA"`},
		"dnevni brzina":      {"Dnevni podaci postaje DALJ - DUNAV,  BRZINA  (m/s)\r\n01.01.2001;1;\r\n", `nepoznata veličina "BRZINA"`},
		"snimka dvije točke": {profil, "snimka ima samo 2 točaka"},
	} {
		if _, err := Procitaj(ime+".csv", cp(s.sadrzaj)); err == nil || err.Error() != s.greska {
			t.Errorf("%s: %v", ime, err)
		}
	}
}

// Kad neki redak ima više od 12 na drugom mjestu, cijela datoteka piše
// mjesec prije dana; nemogući dan se preskače.
func TestDnevniMjesecPrijeDana(t *testing.T) {
	sadrzaj := "Dnevni podaci postaje BELIŠĆE - DRAVA,  VODOSTAJ  (cm)\r\n" +
		"01/13/1962;176;\r\n02/01/1962;186;\r\n13/32/1962;5;\r\n"
	s, err := Procitaj("Dnevni.csv", cp(sadrzaj))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Niz) != 2 || s.Niz[0].Kad.Format("2006-01-02") != "1962-01-13" || s.Niz[1].Kad.Format("2006-01-02") != "1962-02-01" || s.Niz[1].V != "186" {
		t.Errorf("niz: %+v", s.Niz)
	}
}

// SYLK zapis ćelije bez vrijednosti samo pomakne položaj.
func TestMjerenjaCelijaBezVrijednosti(t *testing.T) {
	sylk := "ID;PWXL;N;E\r\n" +
		"C;Y1;X1;K\"Datum mjerenja\"\r\nC;X4;K\"Protok\"\r\nC;X5;K\"Komentar\"\r\n" +
		"C;Y2;X5\r\nC;X1;K44197\r\nC;X4;K250.04\r\nE\r\n"
	m, err := ProcitajMjerenja([]byte(sylk))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || !m[0].Datum.Equal(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)) || m[0].Protok != "250,0" || m[0].Metoda != "" {
		t.Errorf("mjerenja: %+v", m)
	}
}

// Niz koji se ne da zapisati (ime dulje nego što sustav datoteka prima)
// javlja grešku i ne broji se kao zapisan.
func TestPosaoNizSeNeDaZapisati(t *testing.T) {
	cilj := t.TempDir()
	s := &Sadrzaj{Vrsta: Vrsta{Velicina: "vodostaj", Gustoca: "srednjak", Stupac: "vodostaj_cm"},
		Niz: []Vrijednost{{Kad: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Dan: true, V: "120"}}}
	p := &Posao{Cilj: cilj, Letva: strings.Repeat("dalj", 70), Izvor: "his2000"}
	if err := p.Niz(s); err == nil || p.Zapisano != 0 {
		t.Errorf("predugo ime: %v, zapisano %d", err, p.Zapisano)
	}
	if stavke, _ := os.ReadDir(filepath.Clean(cilj)); len(stavke) != 0 {
		t.Errorf("u cilju je %d stavki", len(stavke))
	}
}
