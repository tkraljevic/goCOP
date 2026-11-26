package propisi

import (
	"errors"
	"testing"

	"gocop/internal/models"
)

// Odluke (25. 11. 2026.): akt se zadaje brojem NN i brojem akta ili
// poveznicom sa stranice NN, u svim oblicima koje NN koristi. Iz poveznice
// se uzimaju samo brojevi, a upit i sidro se odbacuju.
func TestRastaviUlaz(t *testing.T) {
	plan := models.IzdanjeNN{Godina: 2010, Broj: 84, Akt: 2389}
	for _, s := range []struct {
		brojNN, brojAkta, poveznica string
		zeli                        models.IzdanjeNN
	}{
		{"84/2010", "2389", "", plan},
		{" NN 84 / 2010. ", "akt 2389", "", plan},
		{"84/10", "2389", "", plan},
		{"5/95", "7", "", models.IzdanjeNN{Godina: 1995, Broj: 5, Akt: 7}},
		{"5/89", "7", "", models.IzdanjeNN{Godina: 2089, Broj: 5, Akt: 7}},
		{"", "", "https://narodne-novine.nn.hr/clanci/sluzbeni/2010_07_84_2389.html", plan},
		{"", "", "narodne-novine.nn.hr/clanci/sluzbeni/full/2010_07_84_2389.html", plan},
		{"", "", "http://Narodne-Novine.nn.hr/eli/sluzbeni/2010/84/2389/hrv/html", plan},
		{"", "", "https://narodne-novine.nn.hr/eli/sluzbeni/2010/84/2389/?x=1#dio", plan},
		{"", "", " https://narodne-novine.nn.hr/clanci/sluzbeni/2010_07_84_2389.html?search=voda#clanak-3 ", plan},
	} {
		got, err := RastaviUlaz(s.brojNN, s.brojAkta, s.poveznica)
		if err != nil || got != s.zeli {
			t.Errorf("RastaviUlaz(%q, %q, %q) = %+v, %v; želi %+v", s.brojNN, s.brojAkta, s.poveznica, got, err, s.zeli)
		}
	}
	for _, s := range [][2]string{{"84-2010", "2389"}, {"84/2010", ""}, {"", "2389"}, {"84/201", "2389"}, {"0/2010", "1"}, {"84/2010", "0"}, {"", ""}} {
		if _, err := RastaviUlaz(s[0], s[1], ""); !errors.Is(err, ErrKriviBroj) {
			t.Errorf("RastaviUlaz(%q, %q) greška %v, želi ErrKriviBroj", s[0], s[1], err)
		}
	}
}

// Odluke (25. 11. 2026.): program dohvaća samo s narodne-novine.nn.hr.
// Poveznica s drugog poslužitelja, s korisnikom ili portom, na lokalnu
// adresu ili datoteku odbija se, pa poveznica ne može navesti program na
// tuđu ili unutarnju adresu.
func TestTudjiPosluziteljSeOdbija(t *testing.T) {
	for _, p := range []string{
		"https://narodne-novine.nn.hr.evil.hr/clanci/sluzbeni/2010_07_84_2389.html",
		"https://x@narodne-novine.nn.hr/clanci/sluzbeni/2010_07_84_2389.html",
		"https://narodne-novine.nn.hr:8080/clanci/sluzbeni/2010_07_84_2389.html",
		"https://narodne-novine.nn.hr./clanci/sluzbeni/2010_07_84_2389.html",
		"http://127.0.0.1/clanci/sluzbeni/2010_07_84_2389.html",
		"file:///clanci/sluzbeni/2010_07_84_2389.html",
		"ftp://narodne-novine.nn.hr/clanci/sluzbeni/2010_07_84_2389.html",
		"https://narodne-novine.nn.hr/clanci/sluzbeni/../../eli/sluzbeni/2010/84/2389",
		"https://narodne-novine.nn.hr/search?q=voda",
		"https://narodne-novine.nn.hr/%zz",
		"   ://",
	} {
		if _, err := RastaviUlaz("", "", p); !errors.Is(err, ErrNijeNN) {
			t.Errorf("poveznica %q: greška %v, želi ErrNijeNN", p, err)
		}
	}
}

// Odluke (25. 11. 2026.): kad su zadani i brojevi i poveznica, moraju
// govoriti o istom aktu; inače administrator ne zna koji je akt dohvaćen.
func TestBrojeviIPoveznicaSeMorajuSlagati(t *testing.T) {
	poveznica := "https://narodne-novine.nn.hr/eli/sluzbeni/2010/84/2389"
	if got, err := RastaviUlaz("84/2010", "2389", poveznica); err != nil || got.Akt != 2389 {
		t.Errorf("isti akt: %+v, %v", got, err)
	}
	if _, err := RastaviUlaz("84/2010", "2390", poveznica); !errors.Is(err, ErrRazliciti) {
		t.Errorf("različiti akti: greška %v", err)
	}
	if _, err := RastaviUlaz("84-2010", "2389", poveznica); !errors.Is(err, ErrKriviBroj) {
		t.Errorf("krivi broj uz poveznicu: greška %v", err)
	}
	if _, err := RastaviUlaz("84/2010", "2389", "https://primjer.invalid/x"); !errors.Is(err, ErrNijeNN) {
		t.Errorf("kriva poveznica uz brojeve: greška %v", err)
	}
}

// Odluke (25. 11. 2026.): ELI put iz veza i tablice uz akt daje izdanje, i
// zapisan s poslužiteljem NN.
func TestIzdanjeIzELI(t *testing.T) {
	zeli := models.IzdanjeNN{Godina: 2021, Broj: 84, Akt: 1560}
	for _, e := range []string{"/eli/sluzbeni/2021/84/1560", "https://narodne-novine.nn.hr/eli/sluzbeni/2021/84/1560/", "/eli/sluzbeni/2021/84/1560/hrv/printhtml"} {
		if got, ok := IzdanjeIzELI(e); !ok || got != zeli {
			t.Errorf("IzdanjeIzELI(%q) = %+v, %v", e, got, ok)
		}
	}
	for _, e := range []string{"", "/eli/sluzbeni/2021/84", "https://primjer.invalid/eli/sluzbeni/2021/84/1560", "/eli/sluzbeni/2021/84/1560/hrv/pdf"} {
		if _, ok := IzdanjeIzELI(e); ok {
			t.Errorf("IzdanjeIzELI(%q) prihvaćen", e)
		}
	}
}
