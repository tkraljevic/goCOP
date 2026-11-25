package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// Odluke (25. 11. 2026.): ID propisa slaže se iz broja NN i broja akta, pa
// isti akt učitan na dva računala daje jedan zapis; adresa u Narodnim
// novinama i oznake na stranici slažu se iz istih brojeva.
func TestIzdanjeNNOznake(t *testing.T) {
	for _, s := range []struct {
		i                     IzdanjeNN
		id, eli, kratko, puno string
	}{
		{IzdanjeNN{Godina: 2010, Broj: 84, Akt: 2389}, "nn-2010-84-2389", "/eli/sluzbeni/2010/84/2389", "NN 84/10", "NN 84/2010"},
		{IzdanjeNN{Godina: 2005, Broj: 7, Akt: 12}, "nn-2005-7-12", "/eli/sluzbeni/2005/7/12", "NN 7/05", "NN 7/2005"},
		{IzdanjeNN{Godina: 1998, Broj: 107, Akt: 1}, "nn-1998-107-1", "/eli/sluzbeni/1998/107/1", "NN 107/98", "NN 107/1998"},
	} {
		if got := s.i.ID(); got != s.id {
			t.Errorf("ID %v: %q, želi %q", s.i, got, s.id)
		}
		if got := s.i.PutELI(); got != s.eli {
			t.Errorf("PutELI %v: %q, želi %q", s.i, got, s.eli)
		}
		if got := s.i.Kratko(); got != s.kratko {
			t.Errorf("Kratko %v: %q, želi %q", s.i, got, s.kratko)
		}
		if got := s.i.Puno(); got != s.puno {
			t.Errorf("Puno %v: %q, želi %q", s.i, got, s.puno)
		}
	}
}

// Odluke (25. 11. 2026.): oznaka „mijenjan NN …” stoji samo uz članke koje
// izmjena stvarno mijenja. Odredba same izmjene, stupanje na snagu i
// neprepoznata odredba bez članka ne daju oznaku; kad administrator
// neprepoznatu poveže s člankom, daje.
func TestUputaKojaNeMijenjaPropis(t *testing.T) {
	for _, s := range []struct {
		u    UputaIzmjene
		zeli bool
	}{
		{UputaIzmjene{Vrsta: UputaVlastita}, false},
		{UputaIzmjene{Vrsta: UputaStupanje}, false},
		{UputaIzmjene{Vrsta: UputaNeprepoznata}, false},
		{UputaIzmjene{Vrsta: UputaNeprepoznata, Cilj: CiljUpute{Clanci: []string{"clanak-5"}}, Rucno: true}, true},
		{UputaIzmjene{Vrsta: UputaZamjenaRijeci, Cilj: CiljUpute{Clanci: []string{"clanak-5"}}}, true},
		{UputaIzmjene{Vrsta: UputaCijeliTekst}, true},
		{UputaIzmjene{Vrsta: UputaBrisanjeDijela, Cilj: CiljUpute{Clanci: []string{"clanak-7"}}}, true},
	} {
		if got := s.u.MijenjaPropis(); got != s.zeli {
			t.Errorf("%s (cilj %v): %v, želi %v", s.u.Vrsta, s.u.Cilj.Clanci, got, s.zeli)
		}
	}
}

// Odluke (25. 11. 2026.): oblik zapisa u knjizi su imena polja u JSON-u.
// Prazna neobavezna polja se ne pišu, pa zapis propisa bez izmjena ostaje
// kratak, a stariji čvor ne dobiva polja koja ne zna.
func TestPropisUJSONuImaImenaIzPlana(t *testing.T) {
	p := Propis{
		ID: "nn-2001-1-1", Adresa: "zakon-o-cuvanju-nasipa", Naziv: "Zakon o čuvanju nasipa",
		NN: IzdanjeNN{Godina: 2001, Broj: 1, Akt: 1}, DatumObjave: "2001-01-05", Izvor: IzvorPropisaTekst, Citac: 1,
		Dijelovi: []DioPropisa{{Oznaka: "clanak-1", Vrsta: DioClanak, Broj: "1.", Stavci: []StavakPropisa{{
			Tekst: "Nasip čuvaju:",
			Tocke: []TockaPropisa{{Vrsta: TockaBroj, Oznaka: "1.", Tekst: "vodočuvar"}},
		}}}},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, ima := range []string{`"id":"nn-2001-1-1"`, `"adresa":`, `"nn":{"godina":2001,"broj":1,"akt":1}`, `"datum_objave":"2001-01-05"`, `"izvor":"tekst"`, `"citac":1`, `"dijelovi":[{"oznaka":"clanak-1","vrsta":"clanak","broj":"1.","stavci":[{"tekst":"Nasip čuvaju:","tocke":[{"vrsta":"broj","oznaka":"1.","tekst":"vodočuvar"}]}]}]`} {
		if !strings.Contains(s, ima) {
			t.Errorf("JSON nema %s: %s", ima, s)
		}
	}
	for _, nema := range []string{`"eli"`, `"stupa_na_snagu"`, `"veze"`, `"uklonjeno"`, `"naslov"`, `"roditelj"`, `"neprepoznato"`, `"tablica"`, `"navod"`} {
		if strings.Contains(s, nema) {
			t.Errorf("JSON ima prazno polje %s: %s", nema, s)
		}
	}
	var natrag Propis
	if err := json.Unmarshal(b, &natrag); err != nil {
		t.Fatal(err)
	}
	if natrag.Dijelovi[0].Stavci[0].Tocke[0].Tekst != "vodočuvar" || natrag.NN != p.NN {
		t.Errorf("zapis se ne čita natrag: %+v", natrag)
	}
}
