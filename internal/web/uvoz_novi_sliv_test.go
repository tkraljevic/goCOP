package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/uvoz/izvori"
)

// Odluke (30. 10. 2026.): novi sliv uvozom otvara samo globalni
// administrator; uprava područja uvozi i novu letvu, ali samo u sliv koji u
// stablu već postoji. Imena su izmišljena.

const porukaNovogSliva = "novi sliv otvara samo globalni administrator"

// okolinaNovogSliva: Ana uvozi za svoje područje, kojem pripada i nova letva
// novovo; stablo ima samo sliv primjerica
func okolinaNovogSliva(t *testing.T) *okolinaVrata {
	t.Helper()
	o := novaOkolinaVrata(t)
	o.h.SetOvlastiLetve(func(p *models.UserPermissions, letva string) bool {
		return p != nil && p.AllowedSections["P"] && (letva == "primjerovo" || letva == "novovo")
	})
	return o
}

func TestSmijeSliv(t *testing.T) {
	uprava := UvozPageData{Permissions: &models.UserPermissions{AllowedSections: map[string]bool{"P": true}},
		Slivovi: []string{"primjerica"}, SlivZa: map[string]string{"primjerovo": "primjerica"}}
	globalni := uprava
	globalni.Permissions = &models.UserPermissions{IsGlobalAdmin: true}
	for _, s := range []struct {
		opis   string
		d      UvozPageData
		sliv   string
		letve  []string
		odbija bool
	}{
		{"uprava, nova letva u novi sliv", uprava, "novica", []string{"novovo"}, true},
		{"uprava, nova letva bez upisane letve u novi sliv", uprava, "novica", []string{""}, true},
		{"uprava, među letvama jedna nova", uprava, "novica", []string{"primjerovo", "novovo"}, true},
		{"uprava, nova letva u postojeći sliv", uprava, "primjerica", []string{"novovo"}, false},
		{"uprava, samo postojeće letve", uprava, "novica", []string{"primjerovo"}, false},
		{"uprava, bez sliva", uprava, "", []string{"novovo"}, false},
		{"globalni administrator, novi sliv", globalni, "novica", []string{"novovo"}, false},
	} {
		err := s.d.smijeSliv(s.sliv, s.letve)
		if s.odbija != (err != nil) || (err != nil && !strings.Contains(err.Error(), porukaNovogSliva)) {
			t.Errorf("%s: %v", s.opis, err)
		}
	}
}

// Posebni izvor bez sliva iz obrasca novu letvu stavlja u zadani sliv
// formata; i to je novi sliv kad ga stablo nema.
func TestPosebniIzvorZadaniSlivJeNovi(t *testing.T) {
	o := okolinaNovogSliva(t)
	gkd, ima := izvori.Nadji("gkd")
	if !ima {
		t.Fatal("nema formata gkd")
	}
	d := o.h.pageData(zahtjevSKorisnikom(httptestGet("/administracija/uvoz-niza"), o.ana, o.anaOvl))
	if err := o.h.smijeUvesti(&d, gkd, izvori.Zadatak{Letva: "novovo"}); err == nil ||
		!strings.Contains(err.Error(), "sliv nije upisan, pa bi nova letva išla u sliv dunav, zadan za ovaj izvor") ||
		!strings.Contains(err.Error(), porukaNovogSliva) {
		t.Errorf("nova letva u zadani sliv koji stablo nema: %v", err)
	}
	if err := o.h.smijeUvesti(&d, gkd, izvori.Zadatak{Letva: "primjerovo"}); err != nil {
		t.Errorf("postojeća letva bez upisanog sliva: %v", err)
	}
	if err := o.h.smijeUvesti(&d, gkd, izvori.Zadatak{Letva: "novovo", Sliv: "primjerica"}); err != nil {
		t.Errorf("nova letva u postojeći sliv: %v", err)
	}
	if err := o.h.smijeUvesti(&d, gkd, izvori.Zadatak{Letva: "primjerovo", Sliv: "primjerica"}); err != nil {
		t.Errorf("postojeća letva: %v", err)
	}
}

// Godišnjak piše na više letava (šifra=letva): kad je među njima nova, novi
// sliv otvara samo globalni administrator, a postojeći sliv smije i Ana.
func TestGodisnjakNoviSlivSamoGlobalni(t *testing.T) {
	o := okolinaNovogSliva(t)
	god, ima := izvori.Nadji("godisnjak")
	if !ima {
		t.Fatal("nema formata godisnjak")
	}
	ana := o.h.pageData(zahtjevSKorisnikom(httptestGet("/administracija/uvoz-niza"), o.ana, o.anaOvl))
	pero := o.h.pageData(zahtjevSKorisnikom(httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl))
	postaje := "42010=primjerovo,42020=novovo"
	for _, s := range []struct {
		opis   string
		d      *UvozPageData
		z      izvori.Zadatak
		odbija bool
	}{
		{"uprava, nova letva u novi sliv", &ana, izvori.Zadatak{Postaje: postaje, Sliv: "novica"}, true},
		{"uprava, nova letva u zadani sliv koji stablo nema", &ana, izvori.Zadatak{Postaje: postaje}, true},
		{"uprava, nova letva u postojeći sliv", &ana, izvori.Zadatak{Postaje: postaje, Sliv: "primjerica"}, false},
		{"uprava, samo postojeća letva", &ana, izvori.Zadatak{Postaje: "42010=primjerovo", Sliv: "novica"}, false},
		{"globalni administrator, novi sliv", &pero, izvori.Zadatak{Postaje: postaje, Sliv: "novica"}, false},
	} {
		err := o.h.smijeUvesti(s.d, god, s.z)
		if s.odbija != (err != nil) || (err != nil && !strings.Contains(err.Error(), porukaNovogSliva)) {
			t.Errorf("%s: %v", s.opis, err)
		}
	}
}

// Pregled posebnog izvora: Ana novu letvu ne stavlja u novi sliv, a u
// postojeći smije; globalni administrator otvara novi sliv.
func TestPosebniIzvorNoviSlivSamoGlobalni(t *testing.T) {
	o := okolinaNovogSliva(t)
	his := datotekaObrasca{"datoteke", "vodostaj.csv", dnevniPrimjerovaHIS}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		sliv   string
		odbija bool
	}{
		{"uprava, novi sliv", o.ana, o.anaOvl, "novica", true},
		{"uprava, postojeći sliv", o.ana, o.anaOvl, "primjerica", false},
		{"globalni administrator, novi sliv", o.pero, o.peroOvl, "novica", false},
	} {
		r := viseDijelni(t, "/administracija/uvoz-izvora/pregled",
			map[string]string{"format": "his2000", "letva": "novovo", "sliv": s.sliv}, his)
		w := odgovor(o.h.PregledIzvora, r, s.u, s.p)
		html := htmlUTekst(w.Body.String())
		pregled := strings.Contains(html, "što bi se upisalo</h2>")
		if w.Code != http.StatusOK || pregled == s.odbija || s.odbija != strings.Contains(html, porukaNovogSliva) {
			t.Errorf("%s: %d %s", s.opis, w.Code, porukeStranice(html))
		}
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("pregled je zapisao %v", d)
	}
}

// reCekaPotvrdu nalazi broj pod kojim pregled čeka potvrdu
var reCekaPotvrdu = regexp.MustCompile(`name="id" value="[^"]+"`)

// Vrata arhive: novi niz nove letve u novi sliv uvozi samo globalni
// administrator.
func TestNizUNoviSlivSamoGlobalni(t *testing.T) {
	o := okolinaNovogSliva(t)
	niz := datotekaObrasca{"datoteka", "novovo_probni_vodostaj_dnevni.csv", dnevniPrimjerova}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		sliv   string
		odbija bool
	}{
		{"uprava, novi sliv", o.ana, o.anaOvl, "novica", true},
		{"uprava, postojeći sliv", o.ana, o.anaOvl, "primjerica", false},
		{"globalni administrator, novi sliv", o.pero, o.peroOvl, "novica", false},
	} {
		r := viseDijelni(t, "/administracija/uvoz-niza/pregled", map[string]string{"sliv": s.sliv}, niz)
		html := htmlUTekst(odgovor(o.h.PregledUvoza, r, s.u, s.p).Body.String())
		ceka := reCekaPotvrdu.MatchString(html)
		if ceka == s.odbija || s.odbija != strings.Contains(html, porukaNovogSliva) {
			t.Errorf("%s: %s", s.opis, porukeStranice(html))
		}
	}
}

// Povijest s HydroViewa: nova letva u novi sliv odbija se prije prijave na
// HydroView; u postojećem slivu stiže do provjere računa.
func TestPovijestSHydroViewaNoviSlivSamoGlobalni(t *testing.T) {
	o := okolinaNovogSliva(t)
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		sliv   string
		odbija bool
	}{
		{"uprava, novi sliv", o.ana, o.anaOvl, "novica", true},
		{"uprava, postojeći sliv", o.ana, o.anaOvl, "primjerica", false},
		{"globalni administrator, novi sliv", o.pero, o.peroOvl, "novica", false},
	} {
		forma := url.Values{"letva": {"novovo"}, "sliv": {s.sliv}, "od": {"2001-03-01"}, "do": {"2001-03-01"}}
		w := odgovor(o.h.PreuzmiHidroView, obicniObrazac("/administracija/uvoz-izvora/hidroview", forma.Encode()), s.u, s.p)
		greska := porukaGreske(w.Body.String())
		if s.odbija != strings.Contains(greska, "Sliva novica u arhivi još nema, a "+porukaNovogSliva) ||
			s.odbija == strings.Contains(greska, "račun za HydroView nije upisan") {
			t.Errorf("%s: %q", s.opis, greska)
		}
	}
}
