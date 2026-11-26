package models

import "strings"

// naziviVrstaDijela su imena vrsta dijelova kako stoje nad tekstom propisa.
var naziviVrstaDijela = map[string]string{
	DioUvod: "Uvod", DioGlava: "Glava", DioOdjeljak: "Odjeljak", DioPlana: "Dio",
	DioClanak: "Članak", DioTocka: "Točka", DioPrilog: "Prilog", DioZavrsetak: "Završetak",
	DioOdlomak: "Odlomak",
}

// Naziv je ime dijela kako stoji nad tekstom: „Članak 97.a”, „Točka XI.”,
// „Glava VII.”, „Prilog 1.” ili „Uvod”. Glava i dio Plana bez broja nose
// svoj naslov („UVODNE ODREDBE”).
func (d DioPropisa) Naziv() string {
	naziv, ok := naziviVrstaDijela[d.Vrsta]
	if !ok {
		naziv = naziviVrstaDijela[DioOdlomak]
	}
	broj := d.Broj
	if d.Vrsta == DioPrilog && broj != "" && !strings.HasSuffix(broj, ".") {
		broj += "."
	}
	switch {
	case broj != "":
		return naziv + " " + broj
	case d.Naslov != "" && (d.Vrsta == DioGlava || d.Vrsta == DioPlana):
		return d.Naslov
	}
	return naziv
}

// Cjelina kaže ima li dio svoju stranicu s dijelovima pod sobom: glava,
// odjeljak, dio Plana i prilog. Članak, točka Plana, uvod, završetak i
// odlomak čitaju se sami.
func (d DioPropisa) Cjelina() bool {
	switch d.Vrsta {
	case DioGlava, DioOdjeljak, DioPlana, DioPrilog:
		return true
	}
	return false
}

// opisiUputa su vrste uputa izmjene riječima korisnika („Što se mijenja”).
var opisiUputa = map[string]string{
	UputaZamjenaRijeci:   "Riječi se zamjenjuju drugima.",
	UputaZamjenaBroja:    "Broj se zamjenjuje drugim.",
	UputaDodavanjeRijeci: "Dodaju se riječi.",
	UputaBrisanjeRijeci:  "Brišu se riječi.",
	UputaNoviTekst:       "Mijenja se i glasi.",
	UputaBrisanjeDijela:  "Briše se.",
	UputaPreoznacavanje:  "Dosadašnji dijelovi dobivaju nove brojeve.",
	UputaCijeliTekst:     "Riječi se zamjenjuju u cijelom tekstu.",
	UputaStupanje:        "Stupanje na snagu.",
	UputaVlastita:        "Odredba same izmjene.",
}

// dodaniDijelovi: uz „iza članka” dodaje se članak, uz „iza stavka” stavak
// (jednina, množina)
var dodaniDijelovi = map[string][2]string{
	"članka": {"članak", "članci"}, "stavka": {"stavak", "stavci"},
	"točke": {"točka", "točke"}, "podtočke": {"podtočka", "podtočke"},
}

// OpisVrste kaže što uputa radi, kako stoji u stupcu „Što se mijenja”:
// „Riječi se zamjenjuju drugima.” ili „Dodaje se članak 97.a iza članka
// 97.”. Neprepoznata i nepoznata uputa imaju istu rečenicu.
func (u UputaIzmjene) OpisVrste() string {
	if u.Vrsta == UputaDodavanjeDijela {
		return u.opisDodavanja()
	}
	if opis, ok := opisiUputa[u.Vrsta]; ok {
		return opis
	}
	return "Program nije prepoznao odredbu."
}

// opisDodavanja: „Dodaje se članak 97.a iza članka 97.”, „Dodaju se
// članci 100.a–100.d iza članka 100.”
func (u UputaIzmjene) opisDodavanja() string {
	razina := strings.Fields(u.Iza)
	vrsta, ok := [2]string{}, false
	if len(razina) > 1 {
		vrsta, ok = dodaniDijelovi[razina[1]]
	}
	if !ok || len(u.NoveOznake) == 0 {
		return "Dodaje se novi dio."
	}
	iza := strings.TrimSuffix(u.Iza, ".") + "."
	if len(u.NoveOznake) == 1 {
		return "Dodaje se " + vrsta[0] + " " + u.NoveOznake[0] + " " + iza
	}
	return "Dodaju se " + vrsta[1] + " " + RasponOznaka(u.NoveOznake) + " " + iza
}

// RasponOznaka piše popis oznaka kratko: „100.a”, „100.a i 100.b” ili
// „100.a–100.d”.
func RasponOznaka(oznake []string) string {
	switch len(oznake) {
	case 0:
		return ""
	case 1:
		return oznake[0]
	case 2:
		return oznake[0] + " i " + oznake[1]
	}
	return oznake[0] + "–" + oznake[len(oznake)-1]
}
