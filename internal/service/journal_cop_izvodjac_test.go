package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// osobaSDuznoscu slaže osobu s jednom aktivnom dužnošću u sektoru P i njezine
// ovlasti, kako ih program računa iz dužnosti
func osobaSDuznoscu(uloga models.Role, doseg models.ScopeType, podrucje int) (*models.User, *models.UserPermissions) {
	sektor := "P"
	d := models.Duty{Role: uloga, ScopeType: doseg, SectorID: &sektor, IsActive: true}
	if podrucje > 0 {
		d.AreaID = &podrucje
	}
	u := models.User{ID: uuid.New(), FullName: string(uloga), Duties: []models.Duty{d}}
	return &u, models.NewUserPermissions(u)
}

// Poslovođa izvođača s dužnošću na području sektora P ne piše u dnevnik
// COP-a tog sektora, ne preuzima dežurstvo i ne upisuje se u plan. U dnevnik
// usluge svog područja i dalje piše rad i napomene. Dežurni operater i
// rukovoditelj područja u istom sektoru pišu kao i dosad. Podaci su
// izmišljeni.
func TestIzvodjacNePiseUDnevnikCOPa(t *testing.T) {
	s := &JournalService{}
	ctx := context.Background()
	pocetak := time.Date(2026, 10, 1, 0, 0, 0, 0, models.Zagreb)
	cop := &models.Journal{ID: "cop", Kind: models.JournalKindDefense, CentarSektor: "P", StartedAt: &pocetak}
	usluga := &models.Journal{ID: "a02", Kind: models.JournalKindMaintenanceA02, AreaID: 1}
	o := models.OpsegDnevnika(*cop, nil, []models.Area{{ID: 1, SectorID: "P"}, {ID: 2, SectorID: "P"}})
	oUsluge := models.OpsegPodrucja(models.Area{ID: 1, SectorID: "P"})

	poslovodja, pp := osobaSDuznoscu(models.RoleServiceLeaderForeman, models.ScopeArea, 1)
	if s.CitaDnevnikeCOP(pp) || s.CitaDnevnikeCOP(nil) {
		t.Error("poslovođa ili osoba bez prijave čita dnevnik COP-a")
	}
	if s.PiseUDnevnik(pp, o, cop) || s.AllowedKinds(poslovodja, pp, o, cop) != nil || s.MozeSebeUPlan(pp, o, cop) {
		t.Errorf("poslovođa piše u dnevnik COP-a: piše=%v vrste=%v plan=%v",
			s.PiseUDnevnik(pp, o, cop), s.AllowedKinds(poslovodja, pp, o, cop), s.MozeSebeUPlan(pp, o, cop))
	}
	zapis := models.JournalEntry{Kind: models.EntryKindReport, Text: "dojava", Date: pocetak}
	if err := s.DodajZapisCOP(ctx, poslovodja, pp, o, cop, &zapis); err == nil || !strings.Contains(err.Error(), "pravo") {
		t.Errorf("poslovođa upisao zapis u dnevnik COP-a: %v", err)
	}
	if err := s.PreuzmiDezurstvo(ctx, poslovodja, pp, o, cop); err == nil || !strings.Contains(err.Error(), "preuzima") {
		t.Errorf("poslovođa preuzeo dežurstvo: %v", err)
	}
	d := models.Dezurstvo{UserID: poslovodja.ID.String(), UserName: poslovodja.FullName, Od: pocetak.Add(8 * time.Hour), Do: pocetak.Add(16 * time.Hour), Opis: models.OpisiRada[0].Opis}
	if err := s.SpremiDezurstvo(ctx, poslovodja, pp, o, cop, &d); err == nil || !strings.Contains(err.Error(), "voditelj ili zamjenik centra") {
		t.Errorf("poslovođa se upisao u plan: %v", err)
	}
	ispravak := models.JournalEntry{Kind: models.EntryKindReport, Text: "x"}
	prijepis := &models.Journal{ID: "cop", Kind: models.JournalKindDefense, CentarSektor: "P", Reconstruction: true}
	if err := s.IspraviPrijepis(ctx, poslovodja, pp, o, prijepis, "z1", ispravak); err == nil || !strings.Contains(err.Error(), "pravo") {
		t.Errorf("poslovođa ispravlja prijepis dnevnika COP-a: %v", err)
	}
	// dnevnik usluge: rad i napomena, kao i dosad
	if vrste := s.AllowedKinds(poslovodja, pp, oUsluge, usluga); len(vrste) != 2 || !s.PiseUDnevnik(pp, oUsluge, usluga) {
		t.Errorf("poslovođa u dnevniku usluge: %v", vrste)
	}

	// kontrola: dežurni operater, rukovoditelj područja, uprava sektora i
	// vodočuvar s dužnošću na dionici u sektoru pišu i upisuju se u plan
	for _, x := range []struct {
		uloga    models.Role
		doseg    models.ScopeType
		podrucje int
		dionice  string
	}{
		{models.RoleOperator, models.ScopeSector, 0, ""},
		{models.RoleAreaLeader, models.ScopeArea, 1, ""},
		{models.RoleSectorLeader, models.ScopeSector, 0, ""},
		{models.RoleWaterGuard, models.ScopeSection, 1, "P.1.1"},
	} {
		u, _ := osobaSDuznoscu(x.uloga, x.doseg, x.podrucje)
		u.Duties[0].SectionCodes = x.dionice
		perms := models.NewUserPermissions(*u)
		if !s.PiseUDnevnik(perms, o, cop) || len(s.AllowedKinds(u, perms, o, cop)) == 0 || !s.MozeSebeUPlan(perms, o, cop) {
			t.Errorf("%s ne piše u dnevnik COP-a svog sektora", x.uloga)
		}
		if !s.CitaDnevnikeCOP(perms) {
			t.Errorf("%s ne čita dnevnik COP-a", x.uloga)
		}
	}
	// globalni administrator piše svugdje
	if admin := (&models.UserPermissions{IsGlobalAdmin: true}); !s.PiseUDnevnik(admin, o, cop) || !s.CitaDnevnikeCOP(admin) {
		t.Error("globalni administrator ne piše u dnevnik COP-a")
	}
}

// Za dnevnik COP-a i plan dežurstava izvođač je samo tko danas ima važeću
// dužnost poslovođe (Odluke (30. 10. 2026.)). Bivši poslovođa kojem je
// ostala terenska dužnost, i poslovođa kojem je dužnost istekla, čitaju
// dnevnik COP-a, a prvi u njega i piše i vidi plan; u dnevnicima usluga bivši poslovođa i
// dalje vidi samo dnevnike svoje firme. Poslovođa s važećom dužnošću ostaje
// izvođač.
func TestBivsiPoslovodjaPiseUDnevnikCOPa(t *testing.T) {
	s := &JournalService{}
	s.SetImaoUlogu(func(uuid.UUID, models.Role) (bool, error) { return true, nil })
	cop := &models.Journal{ID: "cop", Kind: models.JournalKindDefense, CentarSektor: "P"}
	o := models.OpsegDnevnika(*cop, nil, []models.Area{{ID: 1, SectorID: "P"}})
	// sad mu je ostala samo terenska dužnost, bez uloge u ustroju
	skladistar, perms := osobaSDuznoscu(models.RoleWarehouseKeeper, models.ScopeArea, 1)
	proslo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	istekao, _ := osobaSDuznoscu(models.RoleServiceLeaderForeman, models.ScopeArea, 1)
	istekao.Duties[0].ExpiresAt = &proslo
	ovlIstekao := models.NewUserPermissions(*istekao)
	for tko, pp := range map[string]*models.UserPermissions{"bivši poslovođa": perms, "poslovođa s isteklom dužnošću": ovlIstekao} {
		if s.IzvodjacZaCOP(&pp.User) || !s.CitaDnevnikeCOP(pp) {
			t.Errorf("%s se u dnevniku COP-a gleda kao izvođač", tko)
		}
	}
	if !s.PiseUDnevnik(perms, o, cop) || !s.MozeSebeUPlan(perms, o, cop) || !s.VidiPlanDezurstava(perms, o, cop) {
		t.Error("bivši poslovođa ne piše u dnevnik COP-a")
	}
	if !s.IzvodjacZaDnevnike(skladistar) {
		t.Error("bivši poslovođa u dnevnicima usluga više nije izvođač")
	}
	// kontrola: poslovođa s važećom dužnošću, i osoba bez računa
	poslovodja, pp := osobaSDuznoscu(models.RoleServiceLeaderForeman, models.ScopeArea, 1)
	if !s.IzvodjacZaCOP(poslovodja) || s.CitaDnevnikeCOP(pp) || s.PiseUDnevnik(pp, o, cop) {
		t.Error("poslovođa s važećom dužnošću nije izvođač u dnevniku COP-a")
	}
	if s.IzvodjacZaCOP(nil) {
		t.Error("osoba bez računa je izvođač")
	}
}

// Uprava centra smije u plan upisati bivšeg poslovođu s dužnošću u sektoru,
// a poslovođu s važećom dužnošću ne (Odluke (30. 10. 2026.)).
func TestBivsiPoslovodjaUlaziUPlan(t *testing.T) {
	s := &JournalService{}
	s.SetImaoUlogu(func(uuid.UUID, models.Role) (bool, error) { return true, nil })
	sektor, prvo := "P", 1
	od := time.Date(2020, 1, 1, 0, 0, 0, 0, models.Zagreb)
	opozvana := models.Duty{ID: uuid.New(), Role: models.RoleServiceLeaderForeman, SectorID: &sektor, AreaID: &prvo}
	povijest := []models.VerzijaZaduzenja{
		{Duty: func() models.Duty { d := opozvana; d.IsActive = true; return d }(), Od: od},
		{Duty: opozvana, Od: od.AddDate(1, 0, 0)},
		{Duty: models.Duty{ID: uuid.New(), Role: models.RoleWaterGuard, SectorID: &sektor, AreaID: &prvo, IsActive: true}, Od: od.AddDate(1, 0, 0)},
	}
	if s.izvodjacPoPovijesti(povijest) {
		t.Error("bivši poslovođa ne ulazi u plan")
	}
	// kontrola: dužnost poslovođe koja danas vrijedi
	if !s.izvodjacPoPovijesti(povijest[:1]) {
		t.Error("poslovođa s važećom dužnošću ulazi u plan")
	}
}

// Obračun sati svih osoba vidi uprava centra; izvješće o radnim satima jedne
// osobe vidi uprava centra i ta osoba sama, a ne svatko tko piše u dnevnik
// (Odluke (30. 10. 2026.), 2).
func TestTkoVidiObracunIIORS(t *testing.T) {
	s := &JournalService{}
	cop := &models.Journal{ID: "cop", Kind: models.JournalKindDefense, CentarSektor: "P"}
	voditelj, uprava := osobaSDuznoscu(models.RoleCopLeader, models.ScopeSector, 0)
	operater, dezurni := osobaSDuznoscu(models.RoleOperator, models.ScopeSector, 0)
	poslovodja, pp := osobaSDuznoscu(models.RoleServiceLeaderForeman, models.ScopeArea, 1)
	drugi := uuid.New().String()

	if s.VidiObracun(dezurni, cop) || s.VidiObracun(pp, cop) {
		t.Error("obračun vidi i tko nije uprava centra")
	}
	if s.VidiIORS(operater, dezurni, cop, drugi) || s.VidiIORS(poslovodja, pp, cop, drugi) {
		t.Error("tuđi IORS vidi i tko nije uprava centra")
	}
	if s.VidiIORS(nil, nil, cop, operater.ID.String()) || s.VidiIORS(operater, dezurni, cop, "") {
		t.Error("IORS bez prijave ili bez osobe")
	}

	// kontrola: uprava centra vidi sve, a osoba svoj IORS
	if !s.VidiObracun(uprava, cop) || !s.VidiIORS(voditelj, uprava, cop, drugi) {
		t.Error("uprava centra ne vidi obračun ili tuđi IORS")
	}
	if !s.VidiIORS(operater, dezurni, cop, operater.ID.String()) || !s.VidiIORS(poslovodja, pp, cop, poslovodja.ID.String()) {
		t.Error("osoba ne vidi svoj IORS")
	}
}
