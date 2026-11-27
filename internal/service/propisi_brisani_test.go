package service

import (
	"testing"

	"gocop/internal/models"
)

// „Naslovi iznad članaka i članci 98. i 99. brišu se.” (NN 47/23, članak 8.)
// briše i naslove i članke, pa članci dobivaju oznaku „brisan”, a ne
// „mijenjan”. Promjena samog naslova iznad članka ostaje „mijenjan”.
func TestBrisanjeSNaslovomIznadJeBrisan(t *testing.T) {
	slucajevi := []struct {
		ime  string
		u    models.UputaIzmjene
		zeli string
	}{
		{"članci s naslovima", models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-98", "clanak-99"}, NaslovIznad: true}}, OznakaBrisan},
		{"članak bez naslova", models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-3"}}}, OznakaBrisan},
		{"stavak u članku", models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-100"}, Stavci: []string{"4"}}}, OznakaMijenjan},
		{"novi naslov iznad članka", models.UputaIzmjene{Vrsta: models.UputaNoviTekst,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-5"}, NaslovIznad: true}}, OznakaMijenjan},
	}
	for _, s := range slucajevi {
		if got := vrstaOznake(s.u); got != s.zeli {
			t.Errorf("%s: %q, a treba %q", s.ime, got, s.zeli)
		}
	}
}

// Dio s oznakom „brisan” stranica ispisuje prekriženo; dio samo s oznakom
// „mijenjan” ne.
func TestDioJeBrisanSamoUzOznakuBrisan(t *testing.T) {
	brisan := DioNaStranici{Oznake: []OznakaIzmjene{{Vrsta: OznakaMijenjan}, {Vrsta: OznakaBrisan}}}
	mijenjan := DioNaStranici{Oznake: []OznakaIzmjene{{Vrsta: OznakaMijenjan}}}
	if !brisan.Brisan() || mijenjan.Brisan() || (DioNaStranici{}).Brisan() {
		t.Errorf("brisan %v, mijenjan %v, bez oznaka %v", brisan.Brisan(), mijenjan.Brisan(), DioNaStranici{}.Brisan())
	}
}

// Naslov iznad brisanog članka prekriži se samo kad ga je izmjena brisala:
// zajednički naslov nad člancima 98.–100. vrijedi dalje kad se briše samo
// članak 98.
func TestNaslovIznadBrisanSamoUzNaslov(t *testing.T) {
	iz := models.IzdanjeNN{Broj: 47, Godina: 2023}
	sNaslovom := models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela,
		Cilj: models.CiljUpute{Clanci: []string{"clanak-98"}, NaslovIznad: true}}
	bezNaslova := models.UputaIzmjene{Vrsta: models.UputaBrisanjeDijela,
		Cilj: models.CiljUpute{Clanci: []string{"clanak-98"}}}
	o := OznakaIzmjene{IzmjenaNN: iz, Odredbe: []OdredbaIzmjene{{Tekst: "a"}}}
	s := DioNaStranici{Oznake: []OznakaIzmjene{oznakaUzDio(o, sNaslovom, "clanak-98", "članak 98.")}}
	b := DioNaStranici{Oznake: []OznakaIzmjene{oznakaUzDio(o, bezNaslova, "clanak-98", "članak 98.")}}
	if !s.Brisan() || !s.BrisanNaslov() {
		t.Errorf("s naslovom: brisan %v, naslov %v", s.Brisan(), s.BrisanNaslov())
	}
	if !b.Brisan() || b.BrisanNaslov() {
		t.Errorf("bez naslova: brisan %v, naslov %v", b.Brisan(), b.BrisanNaslov())
	}
	// dvije odredbe iste izmjene uz isti članak su jedna oznaka, a naslov
	// ostaje brisan ako ga je brisala ijedna
	o2 := oznakaUzDio(OznakaIzmjene{IzmjenaNN: iz, Odredbe: []OdredbaIzmjene{{Tekst: "b"}}}, bezNaslova, "clanak-98", "članak 98.")
	spojeno := dodajOznaku(s.Oznake, o2)
	if len(spojeno) != 1 || !(DioNaStranici{Oznake: spojeno}).BrisanNaslov() {
		t.Errorf("spajanje: %d oznaka, naslov %v", len(spojeno), DioNaStranici{Oznake: spojeno}.BrisanNaslov())
	}
}
