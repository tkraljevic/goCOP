package models

import (
	"slices"
	"testing"
)

// postaviNazive postavlja nazive organizacije za jedan test i poslije vraća
// točno ono što je vrijedilo prije, da drugi testovi ne vide tuđe nazive.
func postaviNazive(t *testing.T, n OrgTerms) {
	t.Helper()
	stari := currentTerms.Load()
	t.Cleanup(func() { currentTerms.Store(stari) })
	SetTerms(n)
}

// Zaključava zadane nazive uloga: posebne uloge izvan kataloga imaju svoj
// naziv, stari terenski radnik zove se kao vodočuvar, uloge iz kataloga nose
// naziv iz kataloga, a nepoznata uloga (npr. stigla s novijeg čvora) ispisuje
// se svojom šifrom umjesto praznog mjesta.
func TestZadaniNazivUloge(t *testing.T) {
	for _, s := range []struct {
		uloga Role
		naziv string
	}{
		{RoleGlobalAdmin, "Globalni administrator"},
		{RoleAreaAdmin, "Voditelj / Zamjenik COP-a"},
		{RoleFieldWorker, "Vodočuvar"},
		{RoleWaterGuard, "Vodočuvar"},
		{RoleSectorLeader, "Rukovoditelj sektora"},
		{RoleOperator, "Dežurni operater u COP-u"},
		{RoleViewer, "Preglednik (samo čitanje)"},
		{Role("NOVA_ULOGA"), "NOVA_ULOGA"},
	} {
		if got := s.uloga.DefaultLabel(); got != s.naziv {
			t.Errorf("%s: zadani naziv %q, očekivano %q", s.uloga, got, s.naziv)
		}
	}
}

// Zaključava naziv uloge prema postavkama organizacije: bez vlastitog naziva
// vrijedi zadani, vlastiti naziv vodočuvara vrijedi i za starog terenskog
// radnika, a uloga kojoj organizacija nije dala naziv ostaje sa zadanim.
func TestNazivUlogePremaPostavkama(t *testing.T) {
	postaviNazive(t, OrgTerms{})
	if got := RoleWaterGuard.Label(); got != "Vodočuvar" {
		t.Errorf("bez vlastitih naziva: %q, očekivan zadani naziv", got)
	}

	postaviNazive(t, OrgTerms{RoleLabels: map[string]string{
		string(RoleWaterGuard):   "Čuvar nasipa",
		string(RoleSectorLeader): "  Voditelj sektora  ",
	}})
	for _, s := range []struct {
		uloga Role
		naziv string
	}{
		{RoleWaterGuard, "Čuvar nasipa"},
		{RoleFieldWorker, "Čuvar nasipa"},
		{RoleSectorLeader, "Voditelj sektora"},
		{RoleMachinist, "Strojar"},
		{RoleGlobalAdmin, "Globalni administrator"},
	} {
		if got := s.uloga.Label(); got != s.naziv {
			t.Errorf("%s: naziv %q, očekivano %q", s.uloga, got, s.naziv)
		}
	}
}

// Zaključava razinu ustroja uz ulogu za ispis: uloga iz kataloga nosi svoju
// skupinu, a posebne i nepoznate uloge nemaju skupinu.
func TestRazinaUlogeZaIspis(t *testing.T) {
	for _, s := range []struct {
		uloga   Role
		skupina string
	}{
		{RoleNationalLeader, RoleGroupLevel1},
		{RoleSectorLeader, RoleGroupLevel2},
		{RoleAreaLeader, RoleGroupLevel3},
		{RoleSectionLeader, RoleGroupLevel4},
		{RoleWaterGuard, RoleGroupField},
		{RoleServiceLeaderForeman, RoleGroupExternal},
		{RoleGuest, RoleGroupGuest},
		{RoleViewer, RoleGroupProgram},
		{RoleGlobalAdmin, ""},
		{RoleFieldWorker, ""},
		{Role("NOVA_ULOGA"), ""},
	} {
		if got := s.uloga.GroupLabel(); got != s.skupina {
			t.Errorf("%s: skupina %q, očekivano %q", s.uloga, got, s.skupina)
		}
	}
}

// Zaključava popis skupina za obrasce: svaka skupina jednom, redom od vrha
// ustroja prema terenu, kako ih katalog navodi.
func TestSkupineUlogaRedomKataloga(t *testing.T) {
	want := []string{
		RoleGroupLevel1, RoleGroupLevel2, RoleGroupLevel3, RoleGroupLevel4,
		RoleGroupField, RoleGroupExternal, RoleGroupGuest, RoleGroupProgram,
	}
	if got := RoleGroups(); !slices.Equal(got, want) {
		t.Errorf("skupine %q, očekivano %q", got, want)
	}
}
