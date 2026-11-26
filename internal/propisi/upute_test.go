package propisi

import (
	"reflect"
	"strings"
	"testing"

	"gocop/internal/models"
)

// clanakIzmjeneZa slaže članak zakona o izmjenama iz rečenica; redak koji
// počinje s » redak je bloka u navodnicima.
func clanakIzmjeneZa(oznaka string, retci ...string) models.DioPropisa {
	d := models.DioPropisa{Oznaka: oznaka, Vrsta: models.DioClanak}
	for _, r := range retci {
		d.Stavci = append(d.Stavci, models.StavakPropisa{Tekst: r, Navod: strings.HasPrefix(r, "»")})
	}
	return d
}

// jednaUputa prepoznaje jednu rečenicu kao jedini članak izmjene.
func jednaUputa(t *testing.T, retci ...string) models.UputaIzmjene {
	t.Helper()
	u := PrepoznajUpute([]models.DioPropisa{clanakIzmjeneZa("clanak-1", retci...)})
	if len(u) != 1 {
		t.Fatalf("%q: %d uputa, želi 1: %+v", retci[0], len(u), u)
	}
	return u[0]
}

// Odluke (25. 11. 2026.): svaki oblik upute iz zakona o izmjenama ima svoj
// uzorak; uz šest oblika kojih u ispitnom skupu nema (zadnjih šest), koji
// su provjereni samo ovdje. Primjeri su izmišljeni.
func TestOblikUputa(t *testing.T) {
	for _, s := range []struct {
		retci []string
		zeli  models.UputaIzmjene
	}{
		{[]string{"U članku 7. stavku 2. riječi: »tri dana« zamjenjuju se riječima: »pet dana«."},
			models.UputaIzmjene{Vrsta: models.UputaZamjenaRijeci, Cilj: models.CiljUpute{Clanci: []string{"7"}, Stavci: []string{"2"}}, Stari: "tri dana", Novi: "pet dana"}},
		{[]string{"U članku 8. stavcima 2. i 3. broj: »9.« zamjenjuje se brojem: »13.«."},
			models.UputaIzmjene{Vrsta: models.UputaZamjenaBroja, Cilj: models.CiljUpute{Clanci: []string{"8"}, Stavci: []string{"2", "3"}}, Stari: "9.", Novi: "13."}},
		{[]string{"U članku 1. stavku 1. iza riječi: »nasipa« stavlja se zarez i dodaju riječi: »ustava«."},
			models.UputaIzmjene{Vrsta: models.UputaDodavanjeRijeci, Cilj: models.CiljUpute{Clanci: []string{"1"}, Stavci: []string{"1"}}, Iza: "iza riječi", Stari: "nasipa", Novi: "ustava"}},
		{[]string{"U članku 2. ispred riječi: »rijeke« dodaje se riječ: »velike«."},
			models.UputaIzmjene{Vrsta: models.UputaDodavanjeRijeci, Cilj: models.CiljUpute{Clanci: []string{"2"}}, Iza: "ispred riječi", Stari: "rijeke", Novi: "velike"}},
		{[]string{"U članku 3. stavku 1. riječi: »i noću« brišu se."},
			models.UputaIzmjene{Vrsta: models.UputaBrisanjeRijeci, Cilj: models.CiljUpute{Clanci: []string{"3"}, Stavci: []string{"1"}}, Stari: "i noću"}},
		{[]string{"U članku 4. stavak 2. mijenja se i glasi:", "»(2) Novi tekst stavka.«."},
			models.UputaIzmjene{Vrsta: models.UputaNoviTekst, Cilj: models.CiljUpute{Clanci: []string{"4"}, Stavci: []string{"2"}}, Blok: []string{"»(2) Novi tekst stavka.«."}}},
		{[]string{"Članak 6. mijenja se i glasi:"},
			models.UputaIzmjene{Vrsta: models.UputaNoviTekst, Cilj: models.CiljUpute{Clanci: []string{"6"}}}},
		{[]string{"U članku 2. točka 12. mijenja se i glasi:"},
			models.UputaIzmjene{Vrsta: models.UputaNoviTekst, Cilj: models.CiljUpute{Clanci: []string{"2"}, Tocka: "12"}}},
		{[]string{"Iza članka 5. dodaju se naslov iznad članka i članak 5.a koji glase:"},
			models.UputaIzmjene{Vrsta: models.UputaDodavanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"5"}, NaslovIznad: true}, Iza: "iza članka 5.", NoveOznake: []string{"5.a"}}},
		{[]string{"U članku 9. stavku 1. iza točke 15. dodaju se točke 15.a do 15.c koje glase:"},
			models.UputaIzmjene{Vrsta: models.UputaDodavanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"9"}, Stavci: []string{"1"}, Tocka: "15"}, Iza: "iza točke 15.", NoveOznake: []string{"15.a", "15.b", "15.c"}}},
		{[]string{"Naslovi iznad članaka i članci 10. i 11. brišu se."},
			models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"10", "11"}, NaslovIznad: true}}},
		{[]string{"U članku 12. dosadašnji stavak 3. postaje stavak 4."},
			models.UputaIzmjene{Vrsta: models.UputaPreoznacavanje, Cilj: models.CiljUpute{Clanci: []string{"12"}, Stavci: []string{"3"}}, NoveOznake: []string{"4"}}},
		{[]string{"U cijelom tekstu Zakona o čuvanju nasipa (»Narodne novine«, br. 12/01.) riječi: »čuvar« u određenom broju i padežu zamjenjuju se riječima: »čuvar nasipa« u odgovarajućem broju i padežu."},
			models.UputaIzmjene{Vrsta: models.UputaCijeliTekst, Stari: "čuvar", Novi: "čuvar nasipa"}},
		{[]string{"U cijelom tekstu Pravilnika riječi: »ured« zamjenjuju se riječima: »odjel«."},
			models.UputaIzmjene{Vrsta: models.UputaCijeliTekst, Stari: "ured", Novi: "odjel"}},
		{[]string{"Ovaj Zakon stupa na snagu osmoga dana od dana objave u »Narodnim novinama«."},
			models.UputaIzmjene{Vrsta: models.UputaStupanje}},
		{[]string{"U članku 13. nešto se čudno zamjenjuje se drugim."},
			models.UputaIzmjene{Vrsta: models.UputaNeprepoznata, Cilj: models.CiljUpute{Clanci: []string{"13"}}}},
		// Oblici kojih u ispitnom skupu nema.
		{[]string{"Naslov iznad članka 14. mijenja se i glasi:"},
			models.UputaIzmjene{Vrsta: models.UputaNoviTekst, Cilj: models.CiljUpute{Clanci: []string{"14"}, NaslovIznad: true}}},
		{[]string{"Članak 15. briše se."},
			models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"15"}}}},
		{[]string{"U članku 20. dosadašnji stavci 3. do 5. postaju stavci 4. do 6."},
			models.UputaIzmjene{Vrsta: models.UputaPreoznacavanje, Cilj: models.CiljUpute{Clanci: []string{"20"}, Stavci: []string{"3", "4", "5"}}, NoveOznake: []string{"4", "5", "6"}}},
		{[]string{"U članku 16. stavku 1. na kraju rečenice točka se zamjenjuje zarezom i dodaju se riječi: »ako je potrebno«."},
			models.UputaIzmjene{Vrsta: models.UputaDodavanjeRijeci, Cilj: models.CiljUpute{Clanci: []string{"16"}, Stavci: []string{"1"}}, Iza: "na kraju rečenice", Novi: "ako je potrebno"}},
		{[]string{"U članku 17. stavku 2. druga rečenica briše se."},
			models.UputaIzmjene{Vrsta: models.UputaBrisanjeRijeci, Cilj: models.CiljUpute{Clanci: []string{"17"}, Stavci: []string{"2"}}, Stari: "druga rečenica"}},
		{[]string{"U točki XXIII. riječi: »sat« zamjenjuju se riječima: »dva sata«."},
			models.UputaIzmjene{Vrsta: models.UputaZamjenaRijeci, Cilj: models.CiljUpute{Clanci: []string{"XXIII"}}, Stari: "sat", Novi: "dva sata"}},
	} {
		got := jednaUputa(t, s.retci...)
		zeli := s.zeli
		zeli.Redni, zeli.IzClanka, zeli.Tekst = 1, "clanak-1", s.retci[0]
		if !reflect.DeepEqual(got, zeli) {
			t.Errorf("%q:\n dobio %+v\n želi  %+v", s.retci[0], got, zeli)
		}
	}
}

// Odluke (25. 11. 2026.): jedna rečenica može nositi dvije radnje
// („…, a iza riječi: …”); obje dobivaju isto mjesto i isti tekst rečenice.
func TestDvijeRadnjeUJednojRecenici(t *testing.T) {
	r := "U članku 9. stavku 7. iza riječi: »čuvara« stavlja se zarez i dodaju riječi: »zamjenika«, a iza riječi: »obrane« dodaju se riječi: »od poplava«."
	u := PrepoznajUpute([]models.DioPropisa{clanakIzmjeneZa("clanak-2", r)})
	if len(u) != 2 || u[0].Novi != "zamjenika" || u[1].Stari != "obrane" || u[1].Novi != "od poplava" ||
		!reflect.DeepEqual(u[1].Cilj.Clanci, []string{"9"}) || u[1].Tekst != r || u[1].Redni != 2 {
		t.Errorf("upute %+v", u)
	}
}

// Rečenica s tri radnje dijeli se na tri upute. Prije se od druge radnje
// rezalo po krivim mjestima, a duža rečenica srušila je čitanje.
func TestTriRadnjeUJednojRecenici(t *testing.T) {
	r := "U članku 5. stavku 1. riječi: »a« zamjenjuju se riječima: »b«, a riječi: »c« zamjenjuju se riječima: »d«, a iza riječi: »e« dodaju se riječi: »f«."
	u := PrepoznajUpute([]models.DioPropisa{clanakIzmjeneZa("clanak-1", r)})
	if len(u) != 3 {
		t.Fatalf("%d uputa, želi 3: %+v", len(u), u)
	}
	zeli := [][2]string{{"a", "b"}, {"c", "d"}, {"e", "f"}}
	for i, z := range zeli {
		if u[i].Stari != z[0] || u[i].Novi != z[1] || !reflect.DeepEqual(u[i].Cilj.Clanci, []string{"5"}) {
			t.Errorf("uputa %d: %+v", i+1, u[i])
		}
	}
	if u[2].Vrsta != models.UputaDodavanjeRijeci {
		t.Errorf("treća uputa %v", u[2].Vrsta)
	}
}

// Odluke (25. 11. 2026.): mjesto se nasljeđuje unutar članka izmjene: „U
// stavku 5.” ostaje u istom članku, a „Točke 2. i 3.” i u istom stavku.
// Novi članak izmjene počinje bez mjesta.
func TestCiljSeNasljeduje(t *testing.T) {
	u := PrepoznajUpute([]models.DioPropisa{
		clanakIzmjeneZa("clanak-1", "U članku 20. stavku 1. u uvodnoj rečenici riječi: »a« zamjenjuju se riječima: »b«.",
			"U stavku 5. riječi: »c« brišu se.", "Točke 2. i 3. brišu se."),
		clanakIzmjeneZa("clanak-2", "Stavak 4. briše se."),
	})
	if len(u) != 4 {
		t.Fatalf("upute %+v", u)
	}
	zeli := []models.CiljUpute{
		{Clanci: []string{"20"}, Stavci: []string{"1"}, UvodnaRecenica: true},
		{Clanci: []string{"20"}, Stavci: []string{"5"}, Naslijedeno: true},
		{Clanci: []string{"20"}, Stavci: []string{"5"}, Tocka: "2, 3", Naslijedeno: true},
		{Stavci: []string{"4"}},
	}
	for i, z := range zeli {
		if !reflect.DeepEqual(u[i].Cilj, z) {
			t.Errorf("cilj %d: %+v, želi %+v", i+1, u[i].Cilj, z)
		}
	}
	// Mjesto „U dosadašnjem stavku 3., koji postaje stavak 4.,” je stari stavak.
	d := jednaUputa(t, "U dosadašnjem stavku 3., koji postaje stavak 4., riječi: »x« brišu se.")
	if !reflect.DeepEqual(d.Cilj.Stavci, []string{"3"}) || d.Vrsta != models.UputaBrisanjeRijeci {
		t.Errorf("dosadašnji stavak %+v", d)
	}
	// Podtočka i podstavak su najniža razina cilja.
	p := jednaUputa(t, "U članku 25. stavku 1. točki 2. podtočki 2.1. riječ: »a« zamjenjuje se riječima: »b«.")
	if p.Cilj.Tocka != "2" || p.Cilj.Podtocka != "2.1" {
		t.Errorf("podtočka %+v", p.Cilj)
	}
}

// Odluke (25. 11. 2026.): raspon „do” se razvija u sve oznake između:
// „100.a do 100.d” su četiri članka, „1. do 4.” četiri broja.
func TestRasponDoSeRazvija(t *testing.T) {
	u := jednaUputa(t, "Iza članka 30. dodaju se naslovi iznad članaka i članci 30.a do 30.d koji glase:")
	if !reflect.DeepEqual(u.NoveOznake, []string{"30.a", "30.b", "30.c", "30.d"}) {
		t.Errorf("nove oznake %v", u.NoveOznake)
	}
	b := jednaUputa(t, "Točke XX. do XXIII. brišu se.")
	if !reflect.DeepEqual(b.Cilj.Clanci, []string{"XX", "XXI", "XXII", "XXIII"}) {
		t.Errorf("brisanje točaka %+v", b.Cilj)
	}
	for ulaz, zeli := range map[string][]string{
		"1. do 4.":      {"1", "2", "3", "4"},
		"8. i 9.":       {"8", "9"},
		"do 3.":         {"3"},
		"5.a do 6.c":    {"5.a", "6.c"},
		"1. do 900.":    {"1", "900"},
		"XI. do XIII.":  {"XI", "XII", "XIII"},
		"XX. do XXIII.": {"XX", "XXI", "XXII", "XXIII"},
		"5. do 5.c":     {"5", "5.a", "5.b", "5.c"},
		"5.a do 5.c":    {"5.a", "5.b", "5.c"},
		"5. do 6.c":     {"5", "6.c"},
		"I. do CCC.":    {"I", "CCC"},
	} {
		if got := razvijPopis(ulaz); !reflect.DeepEqual(got, zeli) {
			t.Errorf("razvijPopis(%q) = %v, želi %v", ulaz, got, zeli)
		}
	}
	if rimskiPopis(nil) || !rimskiPopis([]string{"XI"}) || rimskiPopis([]string{"11"}) {
		t.Error("rimski popis")
	}
}

// Odluke (25. 11. 2026.): u jednoj stvarnoj izmjeni nedostaje zatvoreni
// navodnik. Takva uputa prolazi samo blagi uzorak i nosi upozorenje.
func TestBlagiUzorakDajeUpozorenje(t *testing.T) {
	u := jednaUputa(t, "U članku 40. stavku 1. riječi: »od 10 do 50 kuna zamjenjuju se riječima: »od 1 do 6 eura«.")
	if u.Vrsta != models.UputaZamjenaRijeci || !u.Blago || u.Upozorenje != porukaBlago || u.Novi != "od 1 do 6 eura" || !u.MijenjaPropis() {
		t.Errorf("uputa %+v", u)
	}
}

// Odluke (25. 11. 2026.): rečenica bez glagola izmjene odredba je same
// izmjene (prijelazne odredbe), a ne greška. Stavci jednog članka su jedna
// odredba, i ne mijenjaju propis.
func TestVlastitaOdredbaNijeGreska(t *testing.T) {
	u := PrepoznajUpute([]models.DioPropisa{
		{Oznaka: "glava-1", Vrsta: models.DioGlava},
		clanakIzmjeneZa("clanak-4", "»Navod bez upute.«", "Prva rečenica.", "Druga rečenica.", " "),
	})
	if len(u) != 1 || u[0].Vrsta != models.UputaVlastita || u[0].Tekst != "Prva rečenica.\nDruga rečenica." || u[0].MijenjaPropis() || u[0].Blok != nil {
		t.Errorf("upute %+v", u)
	}
}

// Odluke (25. 11. 2026.): prva rečenica izmjene počinje nazivom zakona s
// popisom izdanja u zagradi; taj uvod se skida prije čitanja mjesta.
func TestUvodSPopisomIzdanjaSeSkida(t *testing.T) {
	u := jednaUputa(t, "U Zakonu o čuvanju nasipa (»Narodne novine«, br. 12/01., 5/02. – Odluka Ustavnog suda Republike Hrvatske i 7/03.) "+
		"u članku 3. riječi: »danju« brišu se.")
	if u.Vrsta != models.UputaBrisanjeRijeci || !reflect.DeepEqual(u.Cilj.Clanci, []string{"3"}) || u.Stari != "danju" {
		t.Errorf("uputa %+v", u)
	}
}
