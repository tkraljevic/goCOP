package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// okolinaSektora: sektor A s područjem 5 i vodočuvarom ondje
func okolinaSektora(t *testing.T) (*okolinaOporavka, *service.UserService) {
	t.Helper()
	o := novaOkolinaOporavka(t)
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, level, vgo_phone) VALUES ('A', 'Sektor A', 'VGO A', 'COP A', 2, ''), ('B', 'Sektor B', 'VGO B', 'COP B', 2, '')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, direct_to_sector, latitude, longitude, vgi_phone) VALUES
			(5, 'A', 'Područje 5', 'VGI 5', '', 0, 45.5, 18.6, ''), (6, 'A', 'Područje 6', 'VGI 6', '', 0, 45.5, 18.6, ''),
			(16, 'B', 'Područje 16', 'VGI 16', '', 0, 45.6, 18.7, '')`,
	} {
		if _, err := o.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return o, service.NewUserService(o.repo, o.auth, service.NewSSEBroker())
}

func sektor(s string) *string { return &s }
func podrucjeBr(i int) *int   { return &i }

// Uprava sektora uređuje ljude iste razine, ali im ne poništava lozinku, ne
// izdaje kod i ne upisuje lozinku u obrascu (s njom bi preuzela potpis);
// nižim razinama i dalje smije
func TestIstaRazinaUredjujeNePonistava(t *testing.T) {
	o, users := okolinaSektora(t)
	duznost := func(u *models.User, r models.Role, area *int) {
		t.Helper()
		req := service.AddDutyRequest{UserID: u.ID, Role: r, SectorID: sektor("A")}
		if area != nil {
			req = service.AddDutyRequest{UserID: u.ID, Role: r, AreaID: area}
		}
		if err := users.AddDuty(o.admin, req); err != nil {
			t.Fatal(err)
		}
	}
	rukovoditelj := o.racun(t, "ruka", "lozinka-ruka", "ruka@voda.hr", false, true)
	zamjenik := o.racun(t, "zama", "lozinka-zama", "zama@voda.hr", false, true)
	vodocuvar := o.racun(t, "voda", "lozinka-voda", "voda@voda.hr", false, true)
	duznost(rukovoditelj, models.RoleSectorLeader, nil)
	duznost(zamjenik, models.RoleSectorDeputy, nil)
	duznost(vodocuvar, models.RoleWaterGuard, podrucjeBr(5))
	perm, err := o.auth.PermissionsFor(rukovoditelj.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := users.ResetPassword(perm, zamjenik.ID); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("poništenje lozinke zamjeniku (ista razina): %v", err)
	}
	if _, _, err := o.dk.IzdajPrivremeniKod(context.Background(), perm, zamjenik.ID, false); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("privremeni kod zamjeniku (ista razina): %v", err)
	}
	z, _ := o.repo.GetUserByID(zamjenik.ID)
	osnovno := service.UpdateUserRequest{ID: z.ID, Username: z.Username, FullName: z.FullName, OrgType: z.OrgType, Email: z.Email, IsActive: true}
	sLozinkom := osnovno
	sLozinkom.Password = "lozinka-od-rukovoditelja"
	if _, err := users.UpdateUser(perm, sLozinkom); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("lozinka zamjeniku upisana u obrascu (ista razina): %v", err)
	}
	osnovno.Phone = "031 000 000"
	if _, err := users.UpdateUser(perm, osnovno); err != nil {
		t.Errorf("uređivanje zamjenika (ista razina) bez lozinke: %v", err)
	}
	if _, _, err := users.ResetPassword(perm, vodocuvar.ID); err != nil {
		t.Errorf("poništenje lozinke vodočuvaru (niža razina): %v", err)
	}
}

// Privremena uprava na svojoj razini dodjeljuje najdulje do vlastitog
// isteka; nižim razinama rok ne ograničava
func TestPrivremenaUpravaNeDajeTrajnu(t *testing.T) {
	o, users := okolinaSektora(t)
	istek := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Second)
	privremeni := o.racun(t, "privremeni", "lozinka-priv", "priv@voda.hr", false, true)
	if err := users.AddDuty(o.admin, service.AddDutyRequest{UserID: privremeni.ID, Role: models.RoleSectorLeader, SectorID: sektor("A"),
		IsTemporary: true, Reason: "zamjena", ExpiresAt: &istek}); err != nil {
		t.Fatal(err)
	}
	perm, err := o.auth.PermissionsFor(privremeni.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !perm.AdminSectors["A"] {
		t.Fatalf("privremena uprava sektora A: %+v", perm.AdminSectors)
	}
	novi, err := users.CreateUser(perm, service.CreateUserRequest{Username: "pomocnik", Password: "lozinka-pomocnik", FullName: "Pomoćnik",
		OrgType: models.OrgHrvatskeVode, Role: models.RoleSectorDeputy, SectorID: sektor("A")})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := o.repo.GetUserByID(novi.ID)
	if len(u.Duties) != 1 || !u.Duties[0].IsTemporary || u.Duties[0].ExpiresAt == nil || u.Duties[0].ExpiresAt.After(istek) {
		t.Fatalf("dužnost razine 2 od privremene uprave: %+v", u.Duties)
	}
	// i dodana dužnost iste razine, i trajna niža razina ostaje trajna
	if err := users.AddDuty(perm, service.AddDutyRequest{UserID: novi.ID, Role: models.RoleCopDeputy, SectorID: sektor("A")}); err != nil {
		t.Fatal(err)
	}
	if err := users.AddDuty(perm, service.AddDutyRequest{UserID: novi.ID, Role: models.RoleWaterGuard, AreaID: podrucjeBr(5)}); err != nil {
		t.Fatal(err)
	}
	u, _ = o.repo.GetUserByID(novi.ID)
	for _, d := range u.Duties {
		trajna := d.ExpiresAt == nil
		if d.Role == models.RoleWaterGuard && !trajna {
			t.Errorf("vodočuvar je dobio rok: %+v", d)
		}
		if d.Role != models.RoleWaterGuard && (trajna || d.ExpiresAt.After(istek)) {
			t.Errorf("%s nadživljava privremenu upravu: %+v", d.Role, d.ExpiresAt)
		}
	}
}

// Rok privremene uprave gleda dužnosti koje stvarno daju upravu nad tim
// dosegom: zamjenik za područje upravlja područjem, zamjenik glavnog
// rukovoditelja za sektor sektorom; stalna uprava drugog dosega ne
// otključava trajne dodjele, a stalnu upravu ne skraćuje tuđa privremena.
func TestRokUpraveIdePoDosegu(t *testing.T) {
	o, users := okolinaSektora(t)
	istek := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Second)
	dodaj := func(u *models.User, r models.Role, sek *string, area *int, privremena bool) {
		t.Helper()
		req := service.AddDutyRequest{UserID: u.ID, Role: r, SectorID: sek, AreaID: area}
		if privremena {
			req.IsTemporary, req.Reason, req.ExpiresAt = true, "zamjena", &istek
		}
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
	otvori := func(p *models.UserPermissions, ime string, r models.Role, sek *string, area *int) models.Duty {
		t.Helper()
		u, err := users.CreateUser(p, service.CreateUserRequest{Username: ime, Password: "lozinka-" + ime, FullName: ime,
			OrgType: models.OrgHrvatskeVode, Role: r, SectorID: sek, AreaID: area})
		if err != nil {
			t.Fatal(err)
		}
		svjez, _ := o.repo.GetUserByID(u.ID)
		return svjez.Duties[0]
	}
	privremena := func(d models.Duty) bool { return d.IsTemporary && d.ExpiresAt != nil && !d.ExpiresAt.After(istek) }

	// (A) samo privremeni zamjenik rukovoditelja sektora za područje 5
	a := o.racun(t, "zamjenik-a", "lozinka-a", "a@voda.hr", false, true)
	dodaj(a, models.RoleSectorAreaDeputy, sektor("A"), podrucjeBr(5), true)
	if d := otvori(ovlasti(a), "ruk5", models.RoleAreaLeader, nil, podrucjeBr(5)); !privremena(d) {
		t.Errorf("(A) rukovoditelj područja od privremenog zamjenika za područje: %+v", d)
	}
	// (D) samo privremeni zamjenik glavnog rukovoditelja za sektor A
	dd := o.racun(t, "zamjenik-d", "lozinka-d", "d@voda.hr", false, true)
	dodaj(dd, models.RoleSectorMainDeputy, sektor("A"), nil, true)
	if d := otvori(ovlasti(dd), "rukA2", models.RoleSectorLeader, sektor("A"), nil); !privremena(d) {
		t.Errorf("(D) rukovoditelj sektora od privremenog zamjenika glavnog: %+v", d)
	}
	// (B) stalni rukovoditelj sektora A, privremeni rukovoditelj sektora B
	b := o.racun(t, "rukovoditelj-b", "lozinka-b", "b@voda.hr", false, true)
	dodaj(b, models.RoleSectorLeader, sektor("A"), nil, false)
	dodaj(b, models.RoleSectorLeader, sektor("B"), nil, true)
	if d := otvori(ovlasti(b), "zamB", models.RoleSectorDeputy, sektor("B"), nil); !privremena(d) {
		t.Errorf("(B) zamjenik u sektoru B od privremene uprave B: %+v", d)
	}
	if d := otvori(ovlasti(b), "zamA", models.RoleSectorDeputy, sektor("A"), nil); d.IsTemporary || d.ExpiresAt != nil {
		t.Errorf("(B) zamjenik u sektoru A od stalne uprave A dobio je rok: %+v", d)
	}
	// (C) stalni zamjenik za područje 5, privremeni rukovoditelj područja 6
	c := o.racun(t, "zamjenik-c", "lozinka-c", "c@voda.hr", false, true)
	dodaj(c, models.RoleSectorAreaDeputy, sektor("A"), podrucjeBr(5), false)
	dodaj(c, models.RoleAreaLeader, nil, podrucjeBr(6), true)
	if d := otvori(ovlasti(c), "zam5", models.RoleAreaDeputy, nil, podrucjeBr(5)); d.IsTemporary || d.ExpiresAt != nil {
		t.Errorf("(C) dužnost u području 5 od stalne uprave dobila je rok: %+v", d)
	}
	if d := otvori(ovlasti(c), "zam6", models.RoleAreaDeputy, nil, podrucjeBr(6)); !privremena(d) {
		t.Errorf("(C) dužnost u području 6 od privremene uprave: %+v", d)
	}

	// (E) privremena uprava sprema tuđu stalnu dužnost bez promjene: ostaje stalna
	stalni := o.racun(t, "stalni-zamjenik", "lozinka-s", "s@voda.hr", false, true)
	dodaj(stalni, models.RoleSectorDeputy, sektor("B"), nil, false)
	sv, _ := o.repo.GetUserByID(stalni.ID)
	d0 := sv.Duties[0]
	if err := users.UpdateDuty(ovlasti(b), d0.ID, service.AddDutyRequest{UserID: stalni.ID, Title: d0.Title, Role: d0.Role, SectorID: d0.SectorID, IsPrimary: d0.IsPrimary}); err != nil {
		t.Fatal(err)
	}
	sv, _ = o.repo.GetUserByID(stalni.ID)
	if sv.Duties[0].IsTemporary || sv.Duties[0].ExpiresAt != nil {
		t.Errorf("(E) spremanje tuđe stalne dužnosti dalo joj je rok: %+v", sv.Duties[0])
	}
}

// Tuđim očima tuđa spremljena lozinka e-pošte ne otključava se ni za
// adresar ni za slanje (svi putovi idu kroz isti račun korisnika)
func TestTudjimOcimaNemaTudjeLozinkePoste(t *testing.T) {
	_, _, err := (&service.AktService{}).RacunPosteOtkljucan(service.TudjimOcima(context.Background()), &models.User{})
	if !errors.Is(err, service.ErrTudjimOcima) {
		t.Fatalf("tuđim očima: %v", err)
	}
}

// Zastavicu globalnog administratora daje samo stalna uprava organizacije:
// privremena uprava razine 1 (dužnost s rokom) ne može njome postati trajna
func TestPrivremenaUpravaOrganizacijeNeDajeZastavicu(t *testing.T) {
	o, users := okolinaSektora(t)
	istek := time.Now().Add(10 * 24 * time.Hour)
	priv := o.racun(t, "privremeni-glavni", "lozinka-pg", "pg@voda.hr", false, true)
	if err := users.AddDuty(o.admin, service.AddDutyRequest{UserID: priv.ID, Role: models.RoleNationalDeputy,
		IsTemporary: true, Reason: "zamjena", ExpiresAt: &istek}); err != nil {
		t.Fatal(err)
	}
	perm, err := o.auth.PermissionsFor(priv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !perm.IsGlobalAdmin {
		t.Fatal("privremena dužnost razine 1 daje upravu organizacije")
	}
	if _, err := users.CreateUser(perm, service.CreateUserRequest{Username: "novi-glavni", Password: "lozinka-ng", FullName: "Novi Glavni",
		OrgType: models.OrgHrvatskeVode, IsGlobalAdmin: true}); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("privremena uprava otvorila je globalnog administratora: %v", err)
	}
	ja, _ := o.repo.GetUserByID(priv.ID)
	if _, err := users.UpdateUser(perm, service.UpdateUserRequest{ID: ja.ID, Username: ja.Username, FullName: ja.FullName,
		OrgType: ja.OrgType, Email: ja.Email, IsActive: true, IsGlobalAdmin: true}); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("privremena uprava dala si je zastavicu: %v", err)
	}
	// stalna uprava organizacije i dalje smije
	if _, err := users.CreateUser(o.admin, service.CreateUserRequest{Username: "drugi-glavni", Password: "lozinka-dg", FullName: "Drugi Glavni",
		OrgType: models.OrgHrvatskeVode, IsGlobalAdmin: true}); err != nil {
		t.Errorf("stalni globalni administrator: %v", err)
	}
}
