package service_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Odluke (30. 10. 2026.), 9: lozinka ima najmanje 10 znakova, svima i na
// svakom mjestu. Broje se znakovi, ne bajtovi: devet slova č ima 18 bajtova.
func TestLozinkaNajmanjeDesetZnakova(t *testing.T) {
	for _, x := range []struct {
		lozinka string
		valja   bool
	}{
		{"", false},
		{"123456789", false},
		{strings.Repeat("č", 9), false},
		{"1234567890", true},
		{strings.Repeat("č", 10), true},
	} {
		err := service.ProvjeriDuljinuLozinke(x.lozinka)
		if (err == nil) != x.valja || (err != nil && !errors.Is(err, service.ErrKratkaLozinka)) {
			t.Errorf("%q: %v", x.lozinka, err)
		}
	}
	if service.ErrKratkaLozinka.Error() != "Lozinka je prekratka: upišite najmanje 10 znakova." {
		t.Errorf("poruka: %q", service.ErrKratkaLozinka)
	}
}

// Novi račun i polje „Nova lozinka” na uređivanju djelatnika odbijaju
// kratku lozinku, a račun ostaje kakav je bio.
func TestKratkaLozinkaNaRacunuSeOdbija(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	novi := service.CreateUserRequest{Username: "kratka", Password: "kratka-1", FullName: "Kratka Lozinka",
		OrgType: models.OrgHrvatskeVode, Role: models.RoleWaterGuard, AreaID: podrucjeP(5)}
	if _, err := o.users.CreateUser(o.admin, novi); !errors.Is(err, service.ErrKratkaLozinka) || !errors.Is(err, service.ErrInvalidUserData) {
		t.Errorf("novi račun s kratkom lozinkom: %v", err)
	}
	if u := racunPoImenu(t, o, "kratka"); u != nil {
		t.Error("račun je otvoren s kratkom lozinkom")
	}

	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	izmjena := service.UpdateUserRequest{ID: vod.ID, Username: vod.Username, FullName: vod.FullName, OrgType: vod.OrgType,
		Email: vod.Email, IsActive: true, Password: "kratka-2"}
	if _, err := o.users.UpdateUser(o.admin, izmjena); !errors.Is(err, service.ErrKratkaLozinka) {
		t.Errorf("nova lozinka na uređivanju djelatnika: %v", err)
	}
	if u := racunPoID(t, o, vod.ID); u == nil || !o.auth.CheckPassword(u.PasswordHash, "lozinka-vod") || u.MustChangePassword {
		t.Error("odbijena lozinka je ipak promijenila račun")
	}
	izmjena.Password = "nova-lozinka-vod"
	if _, err := o.users.UpdateUser(o.admin, izmjena); err != nil {
		t.Errorf("dovoljno duga lozinka: %v", err)
	}
}

// Odluke (30. 10. 2026.), 10: uprava branjenog područja otvara račun samo u
// svom području i nikad kao administratora.
func TestUpravaPodrucjaOtvaraRacunSamoUSvomPodrucju(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ruk := o.osoba(t, "ruk16", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	uprava := o.ovlasti(t, ruk.ID)
	novi := func(ime string, uloga models.Role, podrucje *int) service.CreateUserRequest {
		return service.CreateUserRequest{Username: ime, Password: "lozinka-" + ime, FullName: ime,
			OrgType: models.OrgHrvatskeVode, Role: uloga, AreaID: podrucje}
	}

	stvoren, err := o.users.CreateUser(uprava, novi("vod16", models.RoleWaterGuard, podrucjeP(16)))
	if err != nil {
		t.Fatalf("vodočuvar u svom području: %v", err)
	}
	if u := racunPoID(t, o, stvoren.ID); u == nil || len(u.Duties) != 1 || u.Duties[0].AreaID == nil || *u.Duties[0].AreaID != 16 || u.IsGlobalAdmin {
		t.Errorf("novi račun: %+v", u)
	}

	admin := novi("admin16", models.RoleWaterGuard, podrucjeP(16))
	admin.IsGlobalAdmin = true
	sektor := novi("sektor16", models.RoleSectorLeader, nil)
	sektor.SectorID = sektorP("B")
	for opis, req := range map[string]service.CreateUserRequest{
		"tuđe područje":             novi("vod17", models.RoleWaterGuard, podrucjeP(17)),
		"područje drugog sektora":   novi("vod5", models.RoleWaterGuard, podrucjeP(5)),
		"globalni administrator":    admin,
		"uloga administratora":      novi("glavni16", models.RoleGlobalAdmin, nil),
		"uloga sektora":             sektor,
		"bez uloge":                 novi("bezuloge16", "", nil),
		"bez uloge, s područjem 16": novi("bezuloge16b", "", podrucjeP(16)),
	} {
		if _, err := o.users.CreateUser(uprava, req); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: %v", opis, err)
		}
		if u := racunPoImenu(t, o, req.Username); u != nil {
			t.Errorf("%s: račun je ipak otvoren", opis)
		}
	}
}

// Odluke (30. 10. 2026.), 15: novi račun nema zadanu ulogu. Globalni
// administrator otvara račun bez uloge, a takav se račun smije prijaviti.
func TestNoviRacunBezUloge(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	stvoren, err := o.users.CreateUser(o.admin, service.CreateUserRequest{Username: "novak", Password: "lozinka-novak",
		FullName: "Ivo Novak", OrgType: models.OrgHrvatskeVode})
	if err != nil {
		t.Fatal(err)
	}
	u := racunPoID(t, o, stvoren.ID)
	if u == nil || len(u.Duties) != 0 || u.IsGlobalAdmin {
		t.Fatalf("račun bez uloge: %+v", u)
	}
	if _, _, err := o.auth.Login("novak", "lozinka-novak", "192.168.1.50", "test"); err != nil {
		t.Errorf("račun bez uloge se ne prijavljuje: %v", err)
	}
}

// racunPoImenu čita račun po korisničkom imenu; nil kad ga nema
func racunPoImenu(t *testing.T, o *okolinaOvlasti, ime string) *models.User {
	t.Helper()
	u, err := o.repo.GetUserByUsername(ime)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// racunPoID čita račun po identifikatoru; nil kad ga nema
func racunPoID(t *testing.T, o *okolinaOvlasti, id uuid.UUID) *models.User {
	t.Helper()
	u, err := o.repo.GetUserByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
