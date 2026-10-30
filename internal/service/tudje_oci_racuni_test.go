package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Tuđim očima vrijede ovlasti osobe čijim se očima gleda, pa se lozinka,
// uključenost računa, zastavica globalnog administratora i dužnosti s
// razinom uprave tada ne mijenjaju. Ostali podaci i dužnosti bez razine
// uprave mijenjaju se i tuđim očima, a svojim očima ista uprava smije sve.
func TestTudjimOcimaRacunIUpravneDuznostiSeNeMijenjaju(t *testing.T) {
	o, users := okolinaSektora(t)
	stalni := o.admin
	osoba := o.racun(t, "sektorski", "lozinka-sek1", "sek@voda.hr", false, true)
	vodocuvar := o.racun(t, "voda", "lozinka-voda", "voda@voda.hr", false, true)
	for _, req := range []service.AddDutyRequest{
		{UserID: osoba.ID, Role: models.RoleSectorLeader, SectorID: sektor("A")},
		{UserID: vodocuvar.ID, Role: models.RoleWaterGuard, AreaID: podrucjeBr(5)},
	} {
		if err := users.AddDuty(stalni, req); err != nil {
			t.Fatal(err)
		}
	}
	duznosti, err := o.repo.GetDutiesForUser(osoba.ID)
	if err != nil || len(duznosti) != 1 {
		t.Fatalf("dužnost uprave sektora: %v %v", duznosti, err)
	}
	duznost := duznosti[0]
	racun := func(id uuid.UUID) service.UpdateUserRequest {
		t.Helper()
		u := procitaniRacun(t, o, id)
		return service.UpdateUserRequest{ID: u.ID, Username: u.Username, FullName: u.FullName, OrgType: u.OrgType,
			Email: u.Email, IsActive: u.IsActive, IsGlobalAdmin: u.IsGlobalAdmin, TudjimOcima: true}
	}
	iskljuci := racun(osoba.ID)
	iskljuci.IsActive = false
	lozinka := racun(osoba.ID)
	lozinka.Password = "lozinka-u-obrascu"
	zastavica := racun(osoba.ID)
	zastavica.IsGlobalAdmin = true
	skini := racun(o.sef.ID)
	skini.IsGlobalAdmin = false
	izmjena := service.AddDutyRequest{UserID: osoba.ID, Role: models.RoleSectorLeader, SectorID: sektor("A"), Title: "Rukovoditelj za led", TudjimOcima: true}

	odbijeno := map[string]error{
		"poništenje lozinke":   func() error { _, _, err := users.ResetPassword(stalni, osoba.ID, true); return err }(),
		"isključenje računa":   func() error { _, err := users.UpdateUser(stalni, iskljuci); return err }(),
		"lozinka u obrascu":    func() error { _, err := users.UpdateUser(stalni, lozinka); return err }(),
		"davanje zastavice":    func() error { _, err := users.UpdateUser(stalni, zastavica); return err }(),
		"skidanje zastavice":   func() error { _, err := users.UpdateUser(stalni, skini); return err }(),
		"dužnost razine 1":     users.AddDuty(stalni, service.AddDutyRequest{UserID: osoba.ID, Role: models.RoleNationalDeputy, TudjimOcima: true}),
		"uprava sektora":       users.AddDuty(stalni, service.AddDutyRequest{UserID: vodocuvar.ID, Role: models.RoleSectorDeputy, SectorID: sektor("A"), TudjimOcima: true}),
		"izmjena uprave":       users.UpdateDuty(stalni, duznost.ID, izmjena),
		"opoziv uprave":        users.RevokeDuty(stalni, duznost.ID, true),
		"novi administrator":   noviRacun(users, stalni, "novi-glavni", true, "", nil),
		"novi račun s upravom": noviRacun(users, stalni, "novi-sektorski", false, models.RoleSectorDeputy, sektor("A")),
	}
	for radnja, err := range odbijeno {
		if !errors.Is(err, service.ErrTudjimOcima) {
			t.Errorf("tuđim očima, %s: %v", radnja, err)
		}
	}
	if sad := procitaniRacun(t, o, osoba.ID); !sad.IsActive || sad.IsGlobalAdmin || len(sad.Duties) != 1 || sad.Duties[0].Title != duznost.Title {
		t.Errorf("račun se tuđim očima ipak promijenio: %+v", sad)
	}

	// tuđim očima i dalje: telefon, dužnost bez razine uprave, račun vodočuvara
	telefon := racun(osoba.ID)
	telefon.Phone = "031 111 111"
	if _, err := users.UpdateUser(stalni, telefon); err != nil {
		t.Errorf("tuđim očima, telefon: %v", err)
	}
	if err := users.AddDuty(stalni, service.AddDutyRequest{UserID: vodocuvar.ID, Role: models.RoleWaterGuard, AreaID: podrucjeBr(6), TudjimOcima: true}); err != nil {
		t.Errorf("tuđim očima, vodočuvar u drugom području: %v", err)
	}
	if err := noviRacun(users, stalni, "novi-vodocuvar", false, models.RoleWaterGuard, nil); err != nil {
		t.Errorf("tuđim očima, novi vodočuvar: %v", err)
	}

	// svojim očima ista uprava smije sve to
	if _, _, err := users.ResetPassword(stalni, osoba.ID, false); err != nil {
		t.Errorf("svojim očima, poništenje lozinke: %v", err)
	}
	zastavica.TudjimOcima = false
	if _, err := users.UpdateUser(stalni, zastavica); err != nil {
		t.Errorf("svojim očima, zastavica: %v", err)
	}
	izmjena.TudjimOcima = false
	if err := users.UpdateDuty(stalni, duznost.ID, izmjena); err != nil {
		t.Errorf("svojim očima, izmjena uprave: %v", err)
	}
	if err := users.RevokeDuty(stalni, duznost.ID, false); err != nil {
		t.Errorf("svojim očima, opoziv uprave: %v", err)
	}
}

// procitaniRacun čita račun iz baze; greška ili nepostojeći račun prekidaju test
func procitaniRacun(t *testing.T, o *okolinaOporavka, id uuid.UUID) *models.User {
	t.Helper()
	u, err := o.repo.GetUserByID(id)
	if err != nil || u == nil {
		t.Fatalf("račun %s: %v", id, err)
	}
	return u
}

// noviRacun otvara račun tuđim očima; uloga vodočuvara ide u područje 5
func noviRacun(users *service.UserService, actor *models.UserPermissions, ime string, globalni bool, uloga models.Role, sektorID *string) error {
	req := service.CreateUserRequest{Username: ime, Password: "lozinka-" + ime, FullName: ime, OrgType: models.OrgHrvatskeVode,
		Email: ime + "@voda.hr", IsGlobalAdmin: globalni, Role: uloga, SectorID: sektorID, TudjimOcima: true}
	if uloga == models.RoleWaterGuard {
		req.AreaID = podrucjeBr(5)
	}
	_, err := users.CreateUser(actor, req)
	return err
}

// Poništenje, izmjena i opoziv odbijaju onoga tko nikim ne upravlja,
// nepostojeći račun ili dužnost, vlastitu dužnost i grešku čitanja baze
func TestRadnjeNadRacunomIDuznostiOdbijanja(t *testing.T) {
	o, users := okolinaSektora(t)
	rukovoditelj := o.racun(t, "ruka", "lozinka-ruka", "ruka@voda.hr", false, true)
	vodocuvar := o.racun(t, "voda", "lozinka-voda", "voda@voda.hr", false, true)
	for _, req := range []service.AddDutyRequest{
		{UserID: rukovoditelj.ID, Role: models.RoleSectorLeader, SectorID: sektor("A")},
		{UserID: vodocuvar.ID, Role: models.RoleWaterGuard, AreaID: podrucjeBr(5)},
	} {
		if err := users.AddDuty(o.admin, req); err != nil {
			t.Fatal(err)
		}
	}
	permR, err := o.auth.PermissionsFor(rukovoditelj.ID)
	if err != nil {
		t.Fatal(err)
	}
	permV, err := o.auth.PermissionsFor(vodocuvar.ID)
	if err != nil {
		t.Fatal(err)
	}
	vlastita := procitaniRacun(t, o, rukovoditelj.ID).Duties[0]
	nema := uuid.New()
	izmjena := service.AddDutyRequest{UserID: rukovoditelj.ID, Role: models.RoleSectorLeader, SectorID: sektor("A")}

	ovlast := map[string]error{
		"vodočuvar opoziva":         users.RevokeDuty(permV, vlastita.ID, false),
		"vodočuvar mijenja":         users.UpdateDuty(permV, vlastita.ID, izmjena),
		"opoziv vlastite dužnosti":  users.RevokeDuty(permR, vlastita.ID, false),
		"izmjena vlastite dužnosti": users.UpdateDuty(permR, vlastita.ID, izmjena),
		"poništenje bez prava":      func() error { _, _, err := users.ResetPassword(permV, rukovoditelj.ID, false); return err }(),
	}
	for radnja, err := range ovlast {
		if !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: %v", radnja, err)
		}
	}
	if err := users.RevokeDuty(o.admin, nema, false); err == nil {
		t.Error("opoziv nepostojeće dužnosti")
	}
	if err := users.UpdateDuty(o.admin, nema, izmjena); err == nil {
		t.Error("izmjena nepostojeće dužnosti")
	}
	if _, _, err := users.ResetPassword(o.admin, nema, false); !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("poništenje nepostojećeg računa: %v", err)
	}
	if _, _, err := users.ResetPassword(o.admin, o.admin.User.ID, false); err == nil {
		t.Error("poništenje vlastite lozinke")
	}

	// baza se ne da pročitati: greška, ne radnja
	if _, err := o.db.Exec(`ALTER TABLE duties RENAME TO duties_stara`); err != nil {
		t.Fatal(err)
	}
	if err := users.RevokeDuty(o.admin, vlastita.ID, false); err == nil {
		t.Error("opoziv bez tablice dužnosti")
	}
	if err := users.UpdateDuty(o.admin, vlastita.ID, izmjena); err == nil {
		t.Error("izmjena bez tablice dužnosti")
	}
	if _, _, err := users.ResetPassword(o.admin, vodocuvar.ID, false); err == nil {
		t.Error("poništenje bez tablice dužnosti")
	}
}
