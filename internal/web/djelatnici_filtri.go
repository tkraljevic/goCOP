package web

import (
	"net/url"
	"strconv"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Filtri popisa djelatnika kao čipovi s izbornikom (crtež 20, 1): čip piše
// odabrano („Sektor: B”), a svaka stavka izbornika je adresa popisa s tim
// izborom i ostalim filtrima kakvi jesu.

// filtriDjelatnika su filtri popisa iz adrese
type filtriDjelatnika struct {
	Sektor   string
	Podrucje int
	Uloga    string
	Stanje   string
	Trazi    string
}

// CipFiltra je čip s izbornikom: natpis s odabranim i stavke (IzborFiltra
// je ista stavka kao na Očitanjima)
type CipFiltra struct {
	Natpis  string
	Odabran bool
	Izbor   []IzborFiltra
}

// url je adresa popisa s ovim filtrima; prazni se ne pišu
func (f filtriDjelatnika) url() string {
	v := url.Values{}
	if f.Trazi != "" {
		v.Set("q", f.Trazi)
	}
	if f.Sektor != "" {
		v.Set("sector", f.Sektor)
	}
	if f.Podrucje > 0 {
		v.Set("area", strconv.Itoa(f.Podrucje))
	}
	if f.Uloga != "" {
		v.Set("role", f.Uloga)
	}
	if f.Stanje != "" {
		v.Set("status", f.Stanje)
	}
	if len(v) == 0 {
		return "/users"
	}
	return "/users?" + v.Encode()
}

// imaFiltar javlja je li išta odabrano, pa ima smisla „Poništi filtre”
func (f filtriDjelatnika) imaFiltar() bool {
	return f != filtriDjelatnika{}
}

// urlPonisti je adresa popisa bez filtara, a prazna kad filtra nema
func (f filtriDjelatnika) urlPonisti() string {
	if f.imaFiltar() {
		return "/users"
	}
	return ""
}

// stanjaRacuna su izbori stanja računa, redom kao u izborniku
var stanjaRacuna = []struct{ Vrijednost, Naziv string }{
	{string(models.AccountActive), "Aktivan"},
	{string(models.AccountPending), "Nije se prijavio"},
	{string(models.AccountDisabled), "Isključen"},
	{"BEZ_EPOSTE", "Bez adrese za PIN"},
	{string(repository.StanjeCekaLozinku), "Čeka promjenu lozinke"},
}

// cipoviDjelatnika su čipovi Sektor, Branjeno područje, Uloga i Stanje računa
func cipoviDjelatnika(f filtriDjelatnika, sektori []models.Sector, podrucja []models.Area) []CipFiltra {
	return []CipFiltra{cipSektora(f, sektori), cipPodrucja(f, podrucja), cipUloge(f), cipStanja(f)}
}

// cipSektora: izbor sektora briše područje, jer područje pripada sektoru
func cipSektora(f filtriDjelatnika, sektori []models.Sector) CipFiltra {
	pojam := models.Terms().Get("sektor")
	c := CipFiltra{Natpis: pojam + ": svi", Odabran: f.Sektor != ""}
	g := f
	g.Sektor, g.Podrucje = "", 0
	c.Izbor = append(c.Izbor, IzborFiltra{"Svi", g.url(), f.Sektor == ""})
	for _, s := range sektori {
		g.Sektor = s.ID
		c.Izbor = append(c.Izbor, IzborFiltra{s.Name, g.url(), f.Sektor == s.ID})
	}
	if f.Sektor != "" {
		c.Natpis = pojam + ": " + f.Sektor
	}
	return c
}

func cipPodrucja(f filtriDjelatnika, podrucja []models.Area) CipFiltra {
	pojam := models.Terms().Get("podrucje")
	c := CipFiltra{Natpis: pojam + ": sva", Odabran: f.Podrucje > 0}
	g := f
	g.Podrucje = 0
	c.Izbor = append(c.Izbor, IzborFiltra{"Sva", g.url(), f.Podrucje == 0})
	for _, a := range podrucja {
		g.Podrucje = a.ID
		c.Izbor = append(c.Izbor, IzborFiltra{strconv.Itoa(a.ID) + " · " + a.Name, g.url(), f.Podrucje == a.ID})
	}
	if f.Podrucje > 0 {
		c.Natpis = pojam + ": " + strconv.Itoa(f.Podrucje)
	}
	return c
}

// cipUloge nudi globalnog administratora i uloge iz kataloga, bez
// Preglednika, kao i dosadašnji izbornik
func cipUloge(f filtriDjelatnika) CipFiltra {
	c := CipFiltra{Natpis: "Uloga: sve", Odabran: f.Uloga != ""}
	g := f
	g.Uloga = ""
	c.Izbor = append(c.Izbor, IzborFiltra{"Sve", g.url(), f.Uloga == ""})
	uloge := append([]option{{string(models.RoleGlobalAdmin), models.RoleGlobalAdmin.Label()}}, roleOptions()...)
	for _, o := range uloge {
		if o.Value == string(models.RoleViewer) {
			continue
		}
		g.Uloga = o.Value
		c.Izbor = append(c.Izbor, IzborFiltra{o.Label, g.url(), f.Uloga == o.Value})
	}
	if f.Uloga != "" {
		c.Natpis = "Uloga: " + models.Role(f.Uloga).Label()
	}
	return c
}

func cipStanja(f filtriDjelatnika) CipFiltra {
	c := CipFiltra{Natpis: "Stanje računa: sva", Odabran: f.Stanje != ""}
	g := f
	g.Stanje = ""
	c.Izbor = append(c.Izbor, IzborFiltra{"Sva", g.url(), f.Stanje == ""})
	for _, s := range stanjaRacuna {
		g.Stanje = s.Vrijednost
		c.Izbor = append(c.Izbor, IzborFiltra{s.Naziv, g.url(), f.Stanje == s.Vrijednost})
		if f.Stanje == s.Vrijednost {
			c.Natpis = "Stanje računa: " + s.Naziv
		}
	}
	return c
}
