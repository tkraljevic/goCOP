package service_test

import (
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Odluke (30. 10. 2026.), 16: uprava branjenog područja novom računu i
// postojećem, pri dodjeli dužnosti, daje samo uloge niže od svoje. Upravu
// područja daje uprava sektora ili globalni administrator.
func TestUpravaPodrucjaDajeSamoNizeUloge(t *testing.T) {
	o, users := okolinaSektora(t)
	duznost := func(u *models.User, req service.AddDutyRequest) {
		t.Helper()
		req.UserID = u.ID
		if err := users.AddDuty(o.admin, req); err != nil {
			t.Fatal(err)
		}
	}
	ovlasti := func(u *models.User) *models.UserPermissions {
		t.Helper()
		p, err := o.auth.PermissionsFor(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	rukovoditelj := o.racun(t, "rukbp", "lozinka-rukbp", "rukbp@voda.hr", false, true)
	zamjenik := o.racun(t, "zambp", "lozinka-zambp", "zambp@voda.hr", false, true)
	vodocuvar := o.racun(t, "vodbp", "lozinka-vodbp", "vodbp@voda.hr", false, true)
	sektorA := o.racun(t, "ruksa", "lozinka-ruksa", "ruksa@voda.hr", false, true)
	duznost(rukovoditelj, service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeBr(5)})
	duznost(zamjenik, service.AddDutyRequest{Role: models.RoleAreaDeputy, AreaID: podrucjeBr(5)})
	duznost(vodocuvar, service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeBr(5)})
	duznost(sektorA, service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektor("A")})
	bp := ovlasti(rukovoditelj)

	novi := func(ime string, r models.Role) service.CreateUserRequest {
		return service.CreateUserRequest{Username: ime, Password: "lozinka-" + ime, FullName: ime, OrgType: models.OrgHrvatskeVode,
			Role: r, AreaID: podrucjeBr(5)}
	}
	for _, r := range []models.Role{models.RoleAreaLeader, models.RoleAreaDeputy, models.RoleContractOfficerA2} {
		if _, err := users.CreateUser(bp, novi("upr"+string(r), r)); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("uprava područja otvorila račun s ulogom %s u svom području: %v", r, err)
		}
		if err := users.AddDuty(bp, service.AddDutyRequest{UserID: vodocuvar.ID, Role: r, AreaID: podrucjeBr(5)}); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("uprava područja dodala dužnost %s u svom području: %v", r, err)
		}
	}
	if _, err := users.CreateUser(bp, novi("novivod", models.RoleWaterGuard)); err != nil {
		t.Errorf("uprava područja ne može otvoriti račun vodočuvara: %v", err)
	}
	if err := users.AddDuty(bp, service.AddDutyRequest{UserID: vodocuvar.ID, Role: models.RoleSectionLeader, AreaID: podrucjeBr(5)}); err != nil {
		t.Errorf("uprava područja ne može dodati rukovoditelja dionice: %v", err)
	}

	// izmjena vodočuvara u zamjenika je dodjela, a izmjena zamjenika bez
	// promjene uloge nije
	v := ovlasti(vodocuvar).User.Duties
	if err := users.UpdateDuty(bp, v[0].ID, service.AddDutyRequest{Role: models.RoleAreaDeputy, AreaID: podrucjeBr(5)}); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja promijenila vodočuvara u zamjenika: %v", err)
	}
	z := ovlasti(zamjenik).User.Duties[0]
	if err := users.UpdateDuty(bp, z.ID, service.AddDutyRequest{Role: z.Role, AreaID: z.AreaID, Reason: "isto"}); err != nil {
		t.Errorf("uprava područja ne može spremiti zamjeniku istu dužnost: %v", err)
	}

	// uprava sektora i dalje daje upravu područja
	sa := ovlasti(sektorA)
	if err := users.AddDuty(sa, service.AddDutyRequest{UserID: vodocuvar.ID, Role: models.RoleAreaDeputy, AreaID: podrucjeBr(6)}); err != nil {
		t.Errorf("uprava sektora ne može dati upravu područja: %v", err)
	}
	if _, err := users.CreateUser(sa, novi("novirukbp", models.RoleAreaLeader)); err != nil {
		t.Errorf("uprava sektora ne može otvoriti račun rukovoditelja područja: %v", err)
	}

	// zamjenik rukovoditelja sektora za područje imenovan je s razine 2, pa
	// u svom području daje i upravu područja
	zsp := o.racun(t, "zsp6", "lozinka-zsp6", "zsp6@voda.hr", false, true)
	duznost(zsp, service.AddDutyRequest{Role: models.RoleSectorAreaDeputy, AreaID: podrucjeBr(6)})
	zs := ovlasti(zsp)
	if _, err := users.CreateUser(zs, service.CreateUserRequest{Username: "rukbp6", Password: "lozinka-rukbp6", FullName: "rukbp6",
		OrgType: models.OrgHrvatskeVode, Role: models.RoleAreaLeader, AreaID: podrucjeBr(6)}); err != nil {
		t.Errorf("zamjenik za područje ne može dati rukovoditelja svog područja: %v", err)
	}
	if _, err := users.CreateUser(zs, service.CreateUserRequest{Username: "rukbp5", Password: "lozinka-rukbp5", FullName: "rukbp5",
		OrgType: models.OrgHrvatskeVode, Role: models.RoleAreaLeader, AreaID: podrucjeBr(5)}); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("zamjenik za područje 6 dao je rukovoditelja područja 5: %v", err)
	}

	// obrazac nudi samo uloge koje osoba smije dati
	for _, x := range []struct {
		opis  string
		p     *models.UserPermissions
		uloga models.Role
		smije bool
	}{
		{"uprava područja: rukovoditelj područja", bp, models.RoleAreaLeader, false},
		{"uprava područja: ovlaštenik A3", bp, models.RoleContractOfficerA3, false},
		{"uprava područja: dežurni operater", bp, models.RoleOperator, false},
		{"uprava područja: vodočuvar", bp, models.RoleWaterGuard, true},
		{"uprava područja: rukovoditelj dionice", bp, models.RoleSectionLeader, true},
		{"uprava sektora: rukovoditelj područja", sa, models.RoleAreaLeader, true},
		{"uprava sektora: dežurni operater", sa, models.RoleOperator, true},
		{"uprava sektora: glavni rukovoditelj", sa, models.RoleNationalLeader, false},
		{"uprava sektora: globalni administrator", sa, models.RoleGlobalAdmin, false},
		{"globalni administrator: glavni rukovoditelj", o.admin, models.RoleNationalLeader, true},
		{"zamjenik za područje: rukovoditelj područja", zs, models.RoleAreaLeader, true},
		{"bez uprave: vodočuvar", &models.UserPermissions{}, models.RoleWaterGuard, false},
	} {
		if got := service.SmijeDatiUlogu(x.p, x.uloga); got != x.smije {
			t.Errorf("%s: %v, želi %v", x.opis, got, x.smije)
		}
	}
}

// Odluke (30. 10. 2026.), 16: uprava područja ne smije kroz izmjenu
// produljiti upravu svog područja koju je s rokom dala uprava sektora. Smije
// je ostaviti kakva je ili skratiti; produljuje je uprava sektora.
func TestUpravaPodrucjaNeProduljujeUpravuPodrucja(t *testing.T) {
	o, users := okolinaSektora(t)
	ovlasti := func(u *models.User) *models.UserPermissions {
		t.Helper()
		p, err := o.auth.PermissionsFor(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	rukovoditelj := o.racun(t, "rukbp", "lozinka-rukbp", "rukbp@voda.hr", false, true)
	zamjenik := o.racun(t, "zambp", "lozinka-zambp", "zambp@voda.hr", false, true)
	sektorA := o.racun(t, "ruksa", "lozinka-ruksa", "ruksa@voda.hr", false, true)
	for _, x := range []service.AddDutyRequest{
		{UserID: rukovoditelj.ID, Role: models.RoleAreaLeader, AreaID: podrucjeBr(5)},
		{UserID: sektorA.ID, Role: models.RoleSectorLeader, SectorID: sektor("A")},
		{UserID: zamjenik.ID, Role: models.RoleWaterGuard, AreaID: podrucjeBr(5)},
	} {
		if err := users.AddDuty(o.admin, x); err != nil {
			t.Fatal(err)
		}
	}
	bp, sa := ovlasti(rukovoditelj), ovlasti(sektorA)
	rok := time.Now().AddDate(0, 1, 0).Truncate(time.Second)
	if err := users.AddDuty(sa, service.AddDutyRequest{UserID: zamjenik.ID, Role: models.RoleAreaDeputy, AreaID: podrucjeBr(5),
		IsTemporary: true, ExpiresAt: &rok, Reason: "obrana"}); err != nil {
		t.Fatal(err)
	}
	var z models.Duty
	for _, d := range ovlasti(zamjenik).User.Duties {
		if d.Role == models.RoleAreaDeputy {
			z = d
		}
	}
	izmjena := func(p *models.UserPermissions, privremena bool, do *time.Time) error {
		return users.UpdateDuty(p, z.ID, service.AddDutyRequest{UserID: zamjenik.ID, Role: z.Role, AreaID: z.AreaID,
			IsTemporary: privremena, ExpiresAt: do, Reason: "obrana"})
	}
	kasnije, ranije := rok.AddDate(0, 1, 0), rok.AddDate(0, 0, -10)
	if err := izmjena(bp, false, nil); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja učinila je privremenog zamjenika stalnim: %v", err)
	}
	if err := izmjena(bp, true, &kasnije); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja produljila je rok zamjeniku: %v", err)
	}
	if err := izmjena(bp, true, &rok); err != nil {
		t.Errorf("uprava područja ne može spremiti isti rok: %v", err)
	}
	if err := izmjena(bp, true, &ranije); err != nil {
		t.Errorf("uprava područja ne može skratiti rok: %v", err)
	}

	// istekla, a neopozvana dužnost ne oživljava
	proslo := time.Now().Add(-time.Hour).UTC()
	if _, err := o.db.Exec(`UPDATE duties SET expires_at = ?, rok = ? WHERE id = ?`, proslo, proslo, z.ID.String()); err != nil {
		t.Fatal(err)
	}
	if err := izmjena(bp, true, &kasnije); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja oživjela je istekloga zamjenika: %v", err)
	}
	if err := izmjena(bp, false, nil); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja istekloga je zamjenika učinila stalnim: %v", err)
	}
	if err := izmjena(sa, false, nil); err != nil {
		t.Errorf("uprava sektora ne može zamjenika učiniti stalnim: %v", err)
	}
}
