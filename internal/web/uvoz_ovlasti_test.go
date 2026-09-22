package web

import (
	"testing"

	"gocop/internal/models"
)

// Uvoz u arhivu je pravo nad letvom, ne nad programom: administrator svog
// područja uvozi svoje, a tuđe ne dira. Arhiva se sinkronizira na sve čvorove,
// pa bi uvoz tuđe letve bio tuđi podatak prepisan tuđom rukom.
func TestUvozTraziPravoNaTuLetvu(t *testing.T) {
	h := &UvozHandler{}
	h.SetOvlastiLetve(func(p *models.UserPermissions, letva string) bool {
		return p != nil && p.AllowedSections["bp16-baranja"] && letva == "batina"
	})

	globalni := &models.UserPermissions{IsGlobalAdmin: true}
	podrucje := &models.UserPermissions{AllowedSections: map[string]bool{"bp16-baranja": true}}
	nitko := &models.UserPermissions{AllowedSections: map[string]bool{}}

	for _, p := range []struct {
		opis   string
		perms  *models.UserPermissions
		letva  string
		ocekuj bool
	}{
		{"globalni administrator smije svaku letvu", globalni, "botovo", true},
		{"administrator područja smije svoju", podrucje, "batina", true},
		{"administrator područja ne smije tuđu", podrucje, "botovo", false},
		{"bez ijedne dionice ne smije ništa", nitko, "batina", false},
		{"neprijavljen ne smije ništa", nil, "batina", false},
		{"prazna letva nije nečija, pa je ne smije ni onaj s dionicom", podrucje, "", false},
	} {
		if got := h.smijeZa(p.perms, p.letva); got != p.ocekuj {
			t.Errorf("%s: dobiveno %v, očekivano %v", p.opis, got, p.ocekuj)
		}
	}
}

// Čvor koji provjeru prava nije predao ne otvara ništa nenamjerno: bez nje
// vrijedi staro pravilo, samo globalni administrator.
func TestBezPredaneProvjereUvoziSamoGlobalniAdministrator(t *testing.T) {
	h := &UvozHandler{}
	podrucje := &models.UserPermissions{AllowedSections: map[string]bool{"bp16-baranja": true}}
	if h.smijeZa(podrucje, "batina") {
		t.Error("bez predane provjere administrator područja ne smije uvoziti")
	}
	if !h.smijeZa(&models.UserPermissions{IsGlobalAdmin: true}, "batina") {
		t.Error("globalni administrator smije i bez predane provjere")
	}
}

// Stranicu uvoza otvara i onaj tko nije globalni administrator, ali samo ako
// mu je barem jedna letva dostupna — inače nema što ondje raditi.
func TestStranicuUvozaOtvaraOnajTkoImaBaremJednuLetvu(t *testing.T) {
	h := &UvozHandler{}
	if h.smije(UvozPageData{Permissions: &models.UserPermissions{}, Letve: nil}) {
		t.Error("bez ijedne dostupne letve stranica se ne otvara")
	}
	if !h.smije(UvozPageData{Permissions: &models.UserPermissions{}, Letve: []string{"batina"}}) {
		t.Error("s dostupnom letvom stranica se otvara")
	}
	if !h.smije(UvozPageData{Permissions: &models.UserPermissions{IsGlobalAdmin: true}}) {
		t.Error("globalni administrator otvara stranicu i kad je stablo prazno")
	}
	if h.smije(UvozPageData{}) {
		t.Error("bez prijave se ne otvara ništa")
	}
}

// Zahvati nad cijelom arhivom ostaju globalnom administratoru: izdavanje
// paketa i ulaganje očitanja nisu vezani uz jednu letvu, pa ih doseg po
// dionici ne može ograničiti.
func TestZahvatiNadCijelomArhivomOstajuGlobalnom(t *testing.T) {
	h := &UvozHandler{}
	if h.samoGlobalni(UvozPageData{Permissions: &models.UserPermissions{}, Letve: []string{"batina"}}) {
		t.Error("administrator područja ne izdaje arhivu")
	}
	if !h.samoGlobalni(UvozPageData{Permissions: &models.UserPermissions{IsGlobalAdmin: true}}) {
		t.Error("globalni administrator izdaje arhivu")
	}
}
