package models

import "testing"

// Ime dijela stoji nad tekstom i u putu pogotka pretrage, pa mora biti kako
// piše u Narodnim novinama: s brojem, a glava i dio Plana bez broja s
// naslovom (Odluke (25. 11. 2026.)). Primjeri su izmišljeni.
func TestNazivDijela(t *testing.T) {
	for _, c := range []struct {
		d    DioPropisa
		want string
	}{
		{DioPropisa{Vrsta: DioClanak, Broj: "97.a"}, "Članak 97.a"},
		{DioPropisa{Vrsta: DioTocka, Broj: "XI."}, "Točka XI."},
		{DioPropisa{Vrsta: DioGlava, Broj: "VII.", Naslov: "ČUVANJE NASIPA"}, "Glava VII."},
		{DioPropisa{Vrsta: DioGlava, Naslov: "ZAVRŠNE ODREDBE"}, "ZAVRŠNE ODREDBE"},
		{DioPropisa{Vrsta: DioPlana, Naslov: "UVODNE ODREDBE"}, "UVODNE ODREDBE"},
		{DioPropisa{Vrsta: DioPrilog, Broj: "2"}, "Prilog 2."},
		{DioPropisa{Vrsta: DioPrilog, Broj: "3."}, "Prilog 3."},
		{DioPropisa{Vrsta: DioUvod}, "Uvod"},
		{DioPropisa{Vrsta: DioClanak, Naslov: "Predmet"}, "Članak"},
		{DioPropisa{Vrsta: "nepoznato"}, "Odlomak"},
	} {
		if got := c.d.Naziv(); got != c.want {
			t.Errorf("%+v: %q, a treba %q", c.d, got, c.want)
		}
	}
}

// Glava, odjeljak, dio Plana i prilog imaju svoju stranicu s dijelovima pod
// sobom; članak, točka, uvod i završetak čitaju se sami
func TestCjelinaPropisa(t *testing.T) {
	for vrsta, want := range map[string]bool{
		DioGlava: true, DioOdjeljak: true, DioPlana: true, DioPrilog: true,
		DioClanak: false, DioTocka: false, DioUvod: false, DioZavrsetak: false, DioOdlomak: false,
	} {
		if got := (DioPropisa{Vrsta: vrsta}).Cjelina(); got != want {
			t.Errorf("%s: %v", vrsta, got)
		}
	}
}

// Stupac „Što se mijenja” piše vrstu upute riječima korisnika, a dodani
// članak s brojem i mjestom (Odluke (25. 11. 2026.), plan Propisa 6.)
func TestOpisVrsteUpute(t *testing.T) {
	for _, c := range []struct {
		u    UputaIzmjene
		want string
	}{
		{UputaIzmjene{Vrsta: UputaZamjenaRijeci}, "Riječi se zamjenjuju drugima."},
		{UputaIzmjene{Vrsta: UputaNoviTekst}, "Mijenja se i glasi."},
		{UputaIzmjene{Vrsta: UputaNeprepoznata}, "Program nije prepoznao odredbu."},
		{UputaIzmjene{Vrsta: "nešto treće"}, "Program nije prepoznao odredbu."},
		{UputaIzmjene{Vrsta: UputaDodavanjeDijela, Iza: "iza članka 5.", NoveOznake: []string{"5.a"}}, "Dodaje se članak 5.a iza članka 5."},
		{UputaIzmjene{Vrsta: UputaDodavanjeDijela, Iza: "iza točke 15.", NoveOznake: []string{"15.a", "15.b", "15.c"}}, "Dodaju se točke 15.a–15.c iza točke 15."},
		{UputaIzmjene{Vrsta: UputaDodavanjeDijela, Iza: "iza stavka 2", NoveOznake: []string{"3", "4"}}, "Dodaju se stavci 3 i 4 iza stavka 2."},
		{UputaIzmjene{Vrsta: UputaDodavanjeDijela, Iza: "iza riječi"}, "Dodaje se novi dio."},
		{UputaIzmjene{Vrsta: UputaDodavanjeDijela}, "Dodaje se novi dio."},
	} {
		if got := c.u.OpisVrste(); got != c.want {
			t.Errorf("%+v: %q, a treba %q", c.u, got, c.want)
		}
	}
	if RasponOznaka(nil) != "" || RasponOznaka([]string{"7.a"}) != "7.a" {
		t.Error("prazan popis ili jedna oznaka")
	}
}
