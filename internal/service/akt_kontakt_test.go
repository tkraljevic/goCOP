package service

import (
	"testing"

	"gocop/internal/models"
	"gocop/internal/posta"
)

// Osoba se u adresaru e-pošte traži po adresi, pa po imenu. Ime se
// uspoređuje bez obzira na red riječi, velika slova, dijakritiku i
// interpunkciju („PERIĆ, PERO” je Pero Perić); kad je istoimenih više,
// program ne pogađa, a jedini kontakt u adresaru uzima se i bez imena.
func TestNajboljiKontaktIzAdresara(t *testing.T) {
	pero := models.User{FullName: "Pero Perić", Email: "pero.peric@voda.hr"}
	bezAdrese := models.User{FullName: "Ana-Marija Anić"}
	for _, x := range []struct {
		opis      string
		osoba     models.User
		adresar   []posta.Kontakt
		zeliAdr   string // prazno = nijedan
		zeliNista bool
	}{
		{"ista adresa, drugo ime", pero, []posta.Kontakt{
			{Ime: "Ivo Ivić", Email: "ivo.ivic@voda.hr"},
			{Ime: "P. Perić", Email: "PERO.PERIC@voda.hr"},
		}, "PERO.PERIC@voda.hr", false},
		{"isto ime, obrnut red i velika slova", bezAdrese, []posta.Kontakt{
			{Ime: "Ivo Ivić", Email: "ivo.ivic@voda.hr"},
			{Ime: "ANIĆ, ANA MARIJA", Email: "ana.anic@voda.hr"},
		}, "ana.anic@voda.hr", false},
		{"ime bez dijakritike i s točkom", models.User{FullName: "Đuro Šoić Žužić"}, []posta.Kontakt{
			{Ime: "zuzic. djuro soic", Email: "krivi@voda.hr"},
			{Ime: "Zuzic Duro Soic", Email: "duro.zuzic@voda.hr"},
		}, "duro.zuzic@voda.hr", false},
		{"dvije istoimene osobe", bezAdrese, []posta.Kontakt{
			{Ime: "Ana-Marija Anić", Email: "ana.anic@voda.hr"},
			{Ime: "Anić Ana Marija", Email: "ana.anic2@voda.hr"},
		}, "", true},
		{"jedini kontakt drugog imena", bezAdrese, []posta.Kontakt{
			{Ime: "Ured sektora P", Email: "sektor.p@voda.hr"},
		}, "sektor.p@voda.hr", false},
		{"nitko istog imena među više kontakata", bezAdrese, []posta.Kontakt{
			{Ime: "Ivo Ivić", Email: "ivo.ivic@voda.hr"},
			{Ime: "Marko Markić", Email: "marko.markic@voda.hr"},
		}, "", true},
		{"prazan adresar", pero, nil, "", true},
	} {
		k := najboljiKontakt(x.adresar, x.osoba)
		switch {
		case x.zeliNista && k != nil:
			t.Errorf("%s: izabran %+v, želi nijedan", x.opis, *k)
		case !x.zeliNista && (k == nil || k.Email != x.zeliAdr):
			t.Errorf("%s: izabran %+v, želi %s", x.opis, k, x.zeliAdr)
		}
	}
}
