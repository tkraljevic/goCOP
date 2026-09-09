package web

import (
	"reflect"
	"strings"
	"testing"

	"gocop/internal/models"
)

func nazivljeHV() models.OrgTerms {
	return models.DefaultTerms()
}

func sektorB() models.Sector {
	return models.Sector{
		ID: "B", Name: "Sektor B — Dunav i donja Drava",
		VgoName: "VGO za Dunav i donju Dravu, Osijek", CenterCop: "COP Osijek",
		Address: "Splavarska 2a, 31000 Osijek", Phone: "N/A", Level: 2,
	}
}

// Memorandum mora ispasti kao na papirnatom obrascu centra: ustanova, vrsta
// jedinice, za što je nadležna, centar, adresa i brojevi.
func TestZaglavljePreslikavaObrazacCentra(t *testing.T) {
	s := sektorB()
	z := zaglavljeIzvjesca(nazivljeHV(), &s)

	if z.Ustanova != "HRVATSKE VODE" {
		t.Errorf("ustanova %q", z.Ustanova)
	}
	ocekivano := []string{
		"VODNOGOSPODARSKI ODJEL",
		"ZA DUNAV I DONJU DRAVU",
		"Centar obrane od poplava Sektora B",
	}
	if !reflect.DeepEqual(z.Jedinica, ocekivano) {
		t.Errorf("jedinica:\n  %q\nočekivano:\n  %q", z.Jedinica, ocekivano)
	}
	if z.Adresa != "Splavarska 2a, 31000 Osijek" {
		t.Errorf("adresa %q", z.Adresa)
	}
	if !reflect.DeepEqual(z.Kontakt, []string{"Telefon:", "N/A"}) {
		t.Errorf("kontakt %q", z.Kontakt)
	}
}

// Isti program u drugom centru daje memorandum tog centra. Da ovo ne vrijedi,
// svaki bi sektor trebao svoju inačicu programa.
func TestSvakiCentarDobivaSvojMemorandum(t *testing.T) {
	a := models.Sector{ID: "A", VgoName: "VGO za Muru i gornju Dravu, Varaždin",
		Address: "Međimurska 26b, 42000 Varaždin", Phone: "042/407-000", Level: 2}
	z := zaglavljeIzvjesca(nazivljeHV(), &a)

	if !strings.Contains(strings.Join(z.Jedinica, "|"), "ZA MURU I GORNJU DRAVU") {
		t.Errorf("jedinica %q", z.Jedinica)
	}
	if !strings.Contains(strings.Join(z.Jedinica, "|"), "Sektora A") {
		t.Errorf("centar %q", z.Jedinica)
	}
	if z.Adresa != "Međimurska 26b, 42000 Varaždin" {
		t.Errorf("adresa %q", z.Adresa)
	}
}

// Više brojeva u jednom polju ide u zasebne retke, kao na obrascu.
func TestViseBrojevaIdeUZasebneRetke(t *testing.T) {
	s := sektorB()
	s.Phone = "031/252-800, N/A"
	s.Email = "copos@voda.hr"
	z := zaglavljeIzvjesca(nazivljeHV(), &s)

	ocekivano := []string{"Telefon:", "031/252-800", "N/A", "copos@voda.hr"}
	if !reflect.DeepEqual(z.Kontakt, ocekivano) {
		t.Errorf("kontakt %q, očekivano %q", z.Kontakt, ocekivano)
	}
}

// Bez telefona nema ni natpisa "Telefon:" — prazan naslov stupca izgleda kao
// izgubljen podatak.
func TestBezTelefonaNemaNatpisa(t *testing.T) {
	s := sektorB()
	s.Phone, s.Email = "", ""
	if z := zaglavljeIzvjesca(nazivljeHV(), &s); len(z.Kontakt) != 0 {
		t.Errorf("kontakt %q, očekivano prazno", z.Kontakt)
	}
}

// Znak ustanove dolazi iz Postavki i putuje knjigom verzija; memorandum ga
// preuzima kakav jest.
func TestZnakDolaziIzNazivlja(t *testing.T) {
	t2 := nazivljeHV()
	t2.Logo, t2.LogoMime = []byte("nije-bitno"), "image/png"
	s := sektorB()
	z := zaglavljeIzvjesca(t2, &s)

	if z.ZnakVrsta != "png" || string(z.Znak) != "nije-bitno" {
		t.Errorf("znak %q vrste %q", z.Znak, z.ZnakVrsta)
	}
	// Word ne prikazuje SVG bez rasterske zamjene, pa se takav znak preskače
	t2.LogoMime = "image/svg+xml"
	if z := zaglavljeIzvjesca(t2, &s); z.ZnakVrsta != "" || z.Znak != nil {
		t.Error("SVG je prošao kao znak")
	}
}

// Bez sektora se ne izmišlja: ostaje naziv ustanove, bez lažne adrese.
func TestBezSektoraNemaAdrese(t *testing.T) {
	z := zaglavljeIzvjesca(nazivljeHV(), nil)
	if z.Ustanova != "HRVATSKE VODE" {
		t.Errorf("ustanova %q", z.Ustanova)
	}
	if z.Adresa != "" || len(z.Jedinica) != 0 || len(z.Kontakt) != 0 {
		t.Errorf("izmišljeni podaci: %+v", z)
	}
}

// Preimenuje li ustanova pojmove, memorandum ide za njima — uključujući
// slučaj kad se hrvatska sklonidba ne može pretpostaviti.
func TestMemorandumPratiPreimenovanoNazivlje(t *testing.T) {
	t2 := nazivljeHV()
	t2.OrgName = "Vode Kantona"
	t2.SectorOffice, t2.SectorOfficeShort = "Regionalni ured", "RU"
	t2.Sector = "Regija"
	t2.Center = "Centar zaštite"
	s := models.Sector{ID: "1", VgoName: "RU za Sjever, Bihać", Level: 2}
	z := zaglavljeIzvjesca(t2, &s)

	if z.Ustanova != "VODE KANTONA" {
		t.Errorf("ustanova %q", z.Ustanova)
	}
	ocekivano := []string{"REGIONALNI URED", "ZA SJEVER", "Centar zaštite Regija 1"}
	if !reflect.DeepEqual(z.Jedinica, ocekivano) {
		t.Errorf("jedinica %q, očekivano %q", z.Jedinica, ocekivano)
	}
}

// Naziv jedinice koji nije u očekivanom obliku ispisuje se kakav jest; bolje
// neuredan redak nego izgubljen ili iskrivljen naziv.
func TestNeocekivanNazivJediniceOstajeCijel(t *testing.T) {
	s := models.Sector{ID: "B", VgoName: "Podružnica Osijek", Level: 2}
	z := zaglavljeIzvjesca(nazivljeHV(), &s)

	if len(z.Jedinica) == 0 || z.Jedinica[0] != "PODRUŽNICA OSIJEK" {
		t.Errorf("jedinica %q", z.Jedinica)
	}
}

// Krovna jedinica nema oznaku sektora; njezin centar ima vlastito ime.
func TestKrovnaJedinicaImaSvojCentar(t *testing.T) {
	s := models.Sector{ID: "DIREKCIJA", Name: "Direkcija, Zagreb", VgoName: "Direkcija",
		Address: "Ulica grada Vukovara 220, 10000 Zagreb", Level: 1}
	z := zaglavljeIzvjesca(nazivljeHV(), &s)

	spojeno := strings.Join(z.Jedinica, "|")
	if strings.Contains(spojeno, "Sektora DIREKCIJA") {
		t.Errorf("krovna jedinica je dobila oznaku sektora: %q", z.Jedinica)
	}
	if !strings.Contains(spojeno, "Glavni centar obrane od poplava") {
		t.Errorf("krovna jedinica nema svoj centar: %q", z.Jedinica)
	}
}
