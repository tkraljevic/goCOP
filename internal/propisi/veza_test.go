package propisi

import (
	"reflect"
	"testing"

	"gocop/internal/models"
)

// osnovaZaVezu je izmišljeni osnovni propis s člancima 1.–3., člankom 97.a
// i točkom Plana XXIII.
func osnovaZaVezu() *models.Propis {
	return &models.Propis{
		Naziv: "Zakon o čuvanju nasipa", NN: models.IzdanjeNN{Godina: 2001, Broj: 12, Akt: 345},
		Dijelovi: []models.DioPropisa{
			{Oznaka: "clanak-1", Vrsta: models.DioClanak}, {Oznaka: "clanak-2", Vrsta: models.DioClanak},
			{Oznaka: "clanak-3", Vrsta: models.DioClanak}, {Oznaka: "clanak-97a", Vrsta: models.DioClanak},
			{Oznaka: "xxiii", Vrsta: models.DioTocka},
		},
	}
}

func uputaSCiljem(vrsta string, clanci ...string) models.UputaIzmjene {
	return models.UputaIzmjene{Vrsta: vrsta, IzClanka: "clanak-1", Tekst: "Rečenica.", Cilj: models.CiljUpute{Clanci: clanci}}
}

// Odluke (25. 11. 2026.): broj članka iz upute postaje oznaka članka osnove
// („97.a” je „clanak-97a”, rimska točka „xxiii”), a ulazne upute ostaju
// iste. Već povezana i ručno povezana uputa se ne mijenja.
func TestUputaSeVezeNaOznakuOsnove(t *testing.T) {
	ulaz := []models.UputaIzmjene{
		uputaSCiljem(models.UputaZamjenaRijeci, "2", "97.a"),
		uputaSCiljem(models.UputaNoviTekst, "XXIII"),
		uputaSCiljem(models.UputaBrisanjeDijela, "clanak-3"),
		{Vrsta: models.UputaNeprepoznata, Rucno: true, Cilj: models.CiljUpute{Clanci: []string{"300"}}},
		uputaSCiljem(models.UputaCijeliTekst),
	}
	out, upozorenja := PoveziSOsnovom(ulaz, osnovaZaVezu())
	if len(upozorenja) != 0 {
		t.Errorf("upozorenja %+v", upozorenja)
	}
	got := [][]string{out[0].Cilj.Clanci, out[1].Cilj.Clanci, out[2].Cilj.Clanci, out[3].Cilj.Clanci, out[4].Cilj.Clanci}
	zeli := [][]string{{"clanak-2", "clanak-97a"}, {"xxiii"}, {"clanak-3"}, {"300"}, nil}
	if !reflect.DeepEqual(got, zeli) {
		t.Errorf("ciljevi %v, želi %v", got, zeli)
	}
	if ulaz[0].Cilj.Clanci[0] != "2" {
		t.Error("povezivanje je promijenilo ulazne upute")
	}
}

// Odluke (25. 11. 2026.): uputa koja cilja članak kojeg u propisu nema
// postaje neprepoznata, bez cilja, uz upozorenje „Članka 300. nema u
// propisu.”, pa je administrator poveže sam.
func TestNepostojeciClanakJeNeprepoznat(t *testing.T) {
	out, upozorenja := PoveziSOsnovom([]models.UputaIzmjene{
		uputaSCiljem(models.UputaZamjenaRijeci, "1", "300"),
		uputaSCiljem(models.UputaNoviTekst, "XL"),
		uputaSCiljem(models.UputaBrisanjeDijela, "50.b"),
	}, osnovaZaVezu())
	if out[0].Vrsta != models.UputaNeprepoznata || out[0].Cilj.Clanci != nil || out[0].MijenjaPropis() || out[0].Upozorenje != "Članka 300. nema u propisu." {
		t.Errorf("uputa %+v", out[0])
	}
	var sto []string
	for _, u := range upozorenja {
		sto = append(sto, u.Sto)
	}
	if !reflect.DeepEqual(sto, []string{"Članka 300. nema u propisu.", "Točke XL. nema u propisu.", "Članka 50.b nema u propisu."}) ||
		upozorenja[0].Vrsta != UpozorenjeNemaClanka || upozorenja[0].Dio != "clanak-1" || upozorenja[0].Retci[0] != "Rečenica." {
		t.Errorf("upozorenja %+v", upozorenja)
	}
}

// Odluke (25. 11. 2026.): „Iza članka 3.b dodaje se članak 3.c” nije greška
// ni kad članka 3.b nema (dodala ga je ranija izmjena); cilj dobiva oznaku
// kakvu bi članak imao.
func TestDodavanjeIzaNepostojecegNijeGreska(t *testing.T) {
	out, upozorenja := PoveziSOsnovom([]models.UputaIzmjene{uputaSCiljem(models.UputaDodavanjeDijela, "3.b")}, osnovaZaVezu())
	if len(upozorenja) != 0 || out[0].Vrsta != models.UputaDodavanjeDijela || !reflect.DeepEqual(out[0].Cilj.Clanci, []string{"clanak-3b"}) {
		t.Errorf("uputa %+v, upozorenja %+v", out[0], upozorenja)
	}
}

// Odluke (25. 11. 2026.): izmjena se veže uz propis kad ih Narodne novine
// vežu ELI vezom (u bilo kojem smjeru) ili kad uvod prvog članka izmjene
// spominje naziv propisa, i u drugom padežu.
func TestVezeSePoELIIPoNazivu(t *testing.T) {
	osnova := osnovaZaVezu()
	izmjena := procitajPrimjer(t, "izmisljena_izmjena.html")
	d := Podijeli(izmjena.Odlomci).Dijelovi
	if !VezeSe(izmjena, d, osnova) {
		t.Error("izmjena s vezom amends se ne veže")
	}
	// Bez ELI veza veže se po nazivu u uvodu prvog članka.
	bezVeza := &Procitano{NN: models.IzdanjeNN{Godina: 2002, Broj: 5, Akt: 67}}
	if !VezeSe(bezVeza, d, osnova) || !VezeSe(nil, d, osnova) {
		t.Error("izmjena se ne veže po nazivu")
	}
	// Osnova navodi izmjenu (amended_by), a izmjena nema ni veze ni naziva.
	osnova.Veze = []models.VezaELI{{Vrsta: "amended_by", ELI: "/eli/sluzbeni/2002/5/67"}}
	prazni := []models.DioPropisa{{Oznaka: "clanak-1", Vrsta: models.DioClanak, Stavci: []models.StavakPropisa{{Tekst: "U Pravilniku o drugom …"}}}}
	if !VezeSe(bezVeza, prazni, osnova) {
		t.Error("izmjena koju osnova navodi se ne veže")
	}
	osnova.Veze, osnova.ELI = nil, "/eli/sluzbeni/2001/12/345"
	if VezeSe(bezVeza, prazni, osnova) || VezeSe(nil, nil, osnova) {
		t.Error("izmjena drugog propisa se veže")
	}
	if VezeSe(nil, d, &models.Propis{Naziv: "Ab"}) {
		t.Error("veže se po nazivu bez riječi")
	}
}
