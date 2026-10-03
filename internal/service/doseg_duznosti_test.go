package service_test

import (
	"errors"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Uprava sektora A ne može sebi ni suradniku upisati upravu sektora B uz
// područje iz sektora A: sektor se uzima iz područja, a dionice moraju biti
// iz njega. Inače bi dobila AdminSectors[B] i poništila lozinku (i s njom
// potpisni ključ) rukovoditelju sektora B. Ni vlastito zaduženje ne dodaje.
func TestDuznostNeDajeTudjiSektor(t *testing.T) {
	o := novaOkolinaOporavka(t)
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, level, vgo_phone) VALUES ('A', 'Sektor A', 'VGO A', 'COP A', 2, ''), ('B', 'Sektor B', 'VGO B', 'COP B', 2, '')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, direct_to_sector, latitude, longitude, vgi_phone) VALUES
			(5, 'A', 'Područje 5', 'VGI 5', '', 0, 45.5, 18.6, ''), (16, 'B', 'Područje 16', 'VGI 16', '', 0, 45.6, 18.7, '')`,
	} {
		if _, err := o.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	users := service.NewUserService(o.repo, o.auth, service.NewSSEBroker())
	sektor := func(s string) *string { return &s }
	podrucje := func(i int) *int { return &i }
	rukovoditelj := func(ime, s string) *models.User {
		t.Helper()
		u := o.racun(t, ime, "lozinka-"+ime, ime+"@voda.hr", false, true)
		if err := users.AddDuty(o.admin, service.AddDutyRequest{UserID: u.ID, Role: models.RoleSectorLeader, SectorID: sektor(s)}); err != nil {
			t.Fatal(err)
		}
		return u
	}
	rukA, rukB := rukovoditelj("ruka", "A"), rukovoditelj("rukb", "B")
	permA, err := o.auth.PermissionsFor(rukA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !permA.AdminSectors["A"] || permA.AdminSectors["B"] {
		t.Fatalf("polazne ovlasti: %+v", permA.AdminSectors)
	}

	napad := service.AddDutyRequest{UserID: rukA.ID, Role: models.RoleSectorLeader, SectorID: sektor("B"), AreaID: podrucje(5)}
	if err := users.AddDuty(permA, napad); err == nil {
		t.Error("uprava sektora A dodala je sebi zaduženje")
	}
	suradnik := o.racun(t, "suradnik", "lozinka-suradnik", "suradnik@voda.hr", false, true)
	napad.UserID = suradnik.ID
	if err := users.AddDuty(permA, napad); !errors.Is(err, service.ErrInvalidUserData) {
		t.Errorf("sektor B uz područje iz sektora A: %v", err)
	}
	if _, err := users.CreateUser(permA, service.CreateUserRequest{Username: "novi", Password: "lozinka-novi", FullName: "Novi",
		OrgType: models.OrgHrvatskeVode, Role: models.RoleSectorLeader, SectorID: sektor("B"), AreaID: podrucje(5)}); !errors.Is(err, service.ErrInvalidUserData) {
		t.Errorf("novi račun sa sektorom B uz područje iz A: %v", err)
	}
	// dionice iz tuđeg područja ni uz vlastito područje
	if _, err := o.db.Exec(`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.16.1', 16, 'B', 'Dionica', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if err := users.AddDuty(permA, service.AddDutyRequest{UserID: suradnik.ID, Role: models.RoleWaterGuard, AreaID: podrucje(5), SectionCodes: "B.16.1"}); !errors.Is(err, service.ErrInvalidUserData) {
		t.Errorf("dionica iz područja 16 uz područje 5: %v", err)
	}
	// račun bez ijedne dužnosti zadužuje globalni administrator; uprava
	// sektora dodaje dužnost tek računu koji već ima aktivnu
	vodocuvar := service.AddDutyRequest{UserID: suradnik.ID, Role: models.RoleWaterGuard, AreaID: podrucje(5)}
	if err := users.AddDuty(permA, vodocuvar); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava sektora A zadužila je račun bez dužnosti: %v", err)
	}
	if err := users.AddDuty(o.admin, service.AddDutyRequest{UserID: suradnik.ID, Role: models.RoleMachinist, AreaID: podrucje(5)}); err != nil {
		t.Fatal(err)
	}
	// ispravna dodjela u svom sektoru i dalje prolazi
	if err := users.AddDuty(permA, vodocuvar); err != nil {
		t.Errorf("vodočuvar u području 5: %v", err)
	}

	permA, err = o.auth.PermissionsFor(rukA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if permA.AdminSectors["B"] {
		t.Fatal("uprava sektora A dobila je upravu sektora B")
	}
	if _, _, err := users.ResetPassword(permA, rukB.ID); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava sektora A poništila je lozinku rukovoditelju sektora B: %v", err)
	}
}
