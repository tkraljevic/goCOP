package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// rukovoditeljDionice je rukovoditelj dionice P.1.1 okoline akta
func rukovoditeljDionice(t *testing.T, o *okolinaAkta) (*models.User, *models.UserPermissions) {
	t.Helper()
	sektor, podrucje := "P", 1
	u := &models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", IsActive: true}
	repo := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test"))
	if err := repo.CreateUser(u, &models.Duty{Title: "Rukovoditelj dionice P.1.1", Role: models.RoleSectionLeader,
		ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: "P.1.1", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	u, err := repo.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u, models.NewUserPermissions(*u)
}

// Sken ovjere učitava samo onaj tko akt smije ovjeriti (Odluke
// (11. 10. 2026.)): vodočuvar, rukovoditelj dionice i dežurni koji je nacrt
// sastavio sken ne učitavaju, a rukovoditelj područja da.
func TestSkenOvjereUcitavaOnajTkoSmijeOvjeriti(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a := o.nacrtDezurnog(t, models.AktUspostava, time.Now().Add(-time.Hour).Truncate(time.Minute))
	sken := []byte("%PDF-1.4 potpisani akt")
	ruk := o.rukovod.ID.String()
	dionica, dionicaOvl := rukovoditeljDionice(t, o)
	for _, x := range []struct {
		opis  string
		u     *models.User
		perms *models.UserPermissions
	}{
		{"vodočuvar dionice akta", o.vodocuv, o.vodOvl},
		{"rukovoditelj dionice", dionica, dionicaOvl},
		{"dežurni koji je sastavio nacrt", o.dezurni, o.dezOvl},
		{"bez osobe", nil, o.ovlasti},
	} {
		if _, _, err := o.akti.UcitajSkenirani(ctx, x.perms, x.u, a.ID, sken, ruk, potvrdaPrimatelja); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: %v", x.opis, err)
		}
	}
	if x, err := o.akti.Get(ctx, a.ID); err != nil || x.Ovjeren() {
		t.Fatalf("akt je ovjeren skenom bez prava: %+v %v", x, err)
	}
	ovjeren, _, err := o.akti.UcitajSkenirani(ctx, o.ovlasti, o.rukovod, a.ID, sken, ruk, potvrdaPrimatelja)
	if err != nil || !ovjeren.Ovjeren() {
		t.Fatalf("rukovoditelj područja: %v", err)
	}
}

// Ovjereni akt šalje onaj tko ga smije ovjeriti i Centar obrane od poplava
// sektora (voditelj, zamjenik, dežurni operater) dok mu dužnost vrijedi
// (Odluke (11. 10. 2026.)); vodočuvar, rukovoditelj dionice i dežurni s
// isteklom dužnošću ne šalju.
func TestAktSaljeOnajTkoSmijeOvjeritiIDezurni(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, time.Now().Add(-time.Hour).Truncate(time.Minute))
	dionica, dionicaOvl := rukovoditeljDionice(t, o)
	sektor, drugi, jucer := "P", "Q", time.Now().Add(-24*time.Hour)
	istekao := models.User{ID: uuid.New(), FullName: "Iva Ivić", Duties: []models.Duty{{Role: models.RoleOperator,
		ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true, ExpiresAt: &jucer}}}
	tudji := models.User{ID: uuid.New(), FullName: "Ana Anić", Duties: []models.Duty{{Role: models.RoleOperator,
		ScopeType: models.ScopeSector, SectorID: &drugi, IsActive: true}}}
	voditeljCOPa := models.User{ID: uuid.New(), FullName: "Ivo Ivić", Duties: []models.Duty{{Role: models.RoleCopLeader,
		ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}}}
	zamjenikCOPa := models.User{ID: uuid.New(), FullName: "Petra Perić", Duties: []models.Duty{{Role: models.RoleCopDeputy,
		ScopeType: models.ScopeSector, SectorID: &sektor, IsActive: true}}}
	for _, x := range []struct {
		opis   string
		u      *models.User
		perms  *models.UserPermissions
		smije  bool
		poruka string
	}{
		{"rukovoditelj područja", o.rukovod, o.ovlasti, true, "nije uključeno"},
		{"dežurni operater u COP-u", o.dezurni, o.dezOvl, true, "nije uključeno"},
		{"voditelj COP-a", &voditeljCOPa, models.NewUserPermissions(voditeljCOPa), true, "nije uključeno"},
		{"zamjenik voditelja COP-a", &zamjenikCOPa, models.NewUserPermissions(zamjenikCOPa), true, "nije uključeno"},
		{"vodočuvar dionice akta", o.vodocuv, o.vodOvl, false, service.ErrUnauthorized.Error()},
		{"rukovoditelj dionice", dionica, dionicaOvl, false, service.ErrUnauthorized.Error()},
		{"dežurni s isteklom dužnošću", &istekao, models.NewUserPermissions(istekao), false, service.ErrUnauthorized.Error()},
		{"dežurni drugog sektora", &tudji, models.NewUserPermissions(tudji), false, service.ErrUnauthorized.Error()},
	} {
		if got := o.akti.SmijeSlati(x.perms, x.u, a); got != x.smije {
			t.Errorf("%s: smije slati = %v", x.opis, got)
		}
		// bez poslužitelja e-pošte slanje staje odmah iza provjere prava
		if _, err := o.akti.PosaljiNaZnanje(ctx, x.perms, x.u, a.ID, []string{"cz@primjer.hr"}, nil, false, service.PorukaAkta{}); err == nil ||
			!strings.Contains(err.Error(), x.poruka) {
			t.Errorf("%s: slanje %v", x.opis, err)
		}
	}
	if o.akti.SmijeSlati(o.ovlasti, nil, a) || o.akti.SmijeSlati(nil, o.rukovod, a) {
		t.Error("bez osobe ili ovlasti akt se ne šalje")
	}
}

// Primatelja mijenja samo uprava sektora ili područja u kojem je bio i u
// koje ide, a područje mora biti u sektoru. Uprava drugog sektora i uprava
// područja ne prepisuju tuđe ni sektorske primatelje.
func TestPrimateljaMijenjaSamoNjegovaUprava(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	upravaP := &models.UserPermissions{AdminSectors: map[string]bool{"P": true}}
	upravaQ := &models.UserPermissions{AdminSectors: map[string]bool{"Q": true}}
	podrucje1 := &models.UserPermissions{AdminAreas: map[int]bool{1: true}}
	upisi := func(perms *models.UserPermissions, p models.Primatelj) (*models.Primatelj, error) {
		p.Naziv, p.Aktivan = "Služba "+p.Sektor, true
		return &p, o.akti.SpremiPrimatelja(ctx, perms, &p)
	}
	q, err := upisi(admin, models.Primatelj{Sektor: "Q", Email: "q@primjer.hr"})
	if err != nil {
		t.Fatal(err)
	}
	sektorski, err := upisi(upravaP, models.Primatelj{Sektor: "P", Email: "p@primjer.hr"})
	if err != nil {
		t.Fatalf("uprava sektora upisuje svog primatelja: %v", err)
	}
	for _, x := range []struct {
		opis   string
		perms  *models.UserPermissions
		p      models.Primatelj
		poruka string
	}{
		{"uprava P preuzima primatelja sektora Q", upravaP, models.Primatelj{ID: q.ID, Sektor: "P", Email: "drugi@primjer.hr"}, service.ErrUnauthorized.Error()},
		{"uprava područja prepisuje sektorskog primatelja", podrucje1, models.Primatelj{ID: sektorski.ID, Sektor: "P", AreaID: 1, Email: "drugi@primjer.hr"}, service.ErrUnauthorized.Error()},
		{"uprava područja upisuje u drugi sektor", podrucje1, models.Primatelj{Sektor: "Q", AreaID: 1, Email: "drugi@primjer.hr"}, "nije u sektoru Q"},
		{"primatelj koji ne postoji", upravaP, models.Primatelj{ID: uuid.NewString(), Sektor: "P"}, "ne postoji"},
		{"bez ovlasti", nil, models.Primatelj{Sektor: "P"}, service.ErrUnauthorized.Error()},
	} {
		if _, err := upisi(x.perms, x.p); err == nil || !strings.Contains(err.Error(), x.poruka) {
			t.Errorf("%s: %v", x.opis, err)
		}
	}
	if sad, err := o.akti.Primatelji(ctx, "Q"); err != nil || len(sad) != 1 || sad[0].Email != "q@primjer.hr" {
		t.Errorf("primatelji sektora Q: %+v %v", sad, err)
	}
	if _, err := upisi(upravaQ, models.Primatelj{ID: q.ID, Sektor: "Q", Email: "nova@primjer.hr"}); err != nil {
		t.Errorf("uprava Q mijenja svog primatelja: %v", err)
	}
	podrucni, err := upisi(podrucje1, models.Primatelj{Sektor: "P", AreaID: 1, Email: "bp1@primjer.hr"})
	if err != nil {
		t.Fatalf("uprava područja upisuje primatelja svog područja: %v", err)
	}
	if _, err := upisi(podrucje1, models.Primatelj{ID: podrucni.ID, Sektor: "P", AreaID: 1, Email: "bp1-novi@primjer.hr"}); err != nil {
		t.Errorf("uprava područja mijenja svog primatelja: %v", err)
	}

	// bez registra djelatnika područje se ne da provjeriti, pa se ne upisuje
	bezRegistra := service.NewAktService(repoAkata(o), nil, nil, nil, nil, nil, nil, "test")
	if err := bezRegistra.SpremiPrimatelja(ctx, admin, &models.Primatelj{Sektor: "P", AreaID: 1, Naziv: "BP 1"}); err == nil || !strings.Contains(err.Error(), "nije u sektoru") {
		t.Errorf("bez registra djelatnika: %v", err)
	}
	// postojeći primatelj koji se ne da pročitati ne mijenja se
	o.izvrsi(t, `ALTER TABLE primatelji RENAME TO primatelji_stari`)
	if _, err := upisi(upravaP, models.Primatelj{ID: sektorski.ID, Sektor: "P"}); err == nil {
		t.Error("primatelj koji se ne da pročitati je promijenjen")
	}
}

// Nacrt akta po adresi otvara samo tko radi u sektoru, području ili na
// dionici akta, kao na popisu; ovjeren akt vidi cijela organizacija
// (Odluke (30. 10. 2026.), 12).
func TestVidiAktPoAdresi(t *testing.T) {
	nacrt := &models.Akt{Sektor: "P", AreaID: 1, Dionice: []models.AktDionica{{Code: "P.1.1"}}}
	ovjeren := &models.Akt{Sektor: "P", AreaID: 1, Status: models.AktOvjeren}
	stranac := &models.UserPermissions{SektoriDuznosti: map[string]bool{"Q": true}}
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
		a     *models.Akt
		vidi  bool
	}{
		{"sektor akta", &models.UserPermissions{SektoriDuznosti: map[string]bool{"P": true}}, nacrt, true},
		{"područje akta", &models.UserPermissions{PodrucjaDuznosti: map[int]bool{1: true}}, nacrt, true},
		{"dionica akta", &models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}}, nacrt, true},
		{"globalni administrator", &models.UserPermissions{IsGlobalAdmin: true}, nacrt, true},
		{"drugi sektor, nacrt", stranac, nacrt, false},
		{"drugi sektor, ovjeren akt", stranac, ovjeren, true},
		{"bez ovlasti", nil, ovjeren, false},
		{"bez akta", stranac, nil, false},
	} {
		if got := service.VidiAkt(x.perms, x.a); got != x.vidi {
			t.Errorf("%s: %v", x.opis, got)
		}
	}
}
