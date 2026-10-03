package service_test

import (
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Račun bez aktivne dužnosti (opozvane, istekle ili otvoren bez nje)
// mayManage štiti od svih osim globalnog administratora. Kad bi mu uprava
// područja smjela dodati dužnost u svom dosegu, odmah bi ga smjela i
// uređivati: uključiti, poništiti lozinku (s njom i potpisni ključ) i
// prijaviti se kao on. Takav račun zadužuje samo globalni administrator.
func TestRacunBezDuznostiZaduzujeGlobalniAdministrator(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ruk5 := o.osoba(t, "ruk5", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(5)})
	perm5 := o.ovlasti(t, ruk5.ID)
	vodocuvar5 := service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)}

	// bivši rukovoditelj sektora B: dužnost opozvana, račun isključen
	bivsi := o.osoba(t, "bivsi", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	if err := o.users.RevokeDuty(o.admin, bivsi.Duties[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, bivsi.ID.String()); err != nil {
		t.Fatal(err)
	}
	d := vodocuvar5
	d.UserID = bivsi.ID
	if err := o.users.AddDuty(perm5, d); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja 5 zadužila je isključen račun bez dužnosti: %v", err)
	}
	if _, err := o.db.Exec(`UPDATE users SET is_active = 1 WHERE id = ?`, bivsi.ID.String()); err != nil {
		t.Fatal(err)
	}
	if err := o.users.AddDuty(perm5, d); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja 5 zadužila je račun bez dužnosti: %v", err)
	}
	if _, _, err := o.users.ResetPassword(perm5, bivsi.ID); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja 5 poništila je lozinku računu bez dužnosti: %v", err)
	}

	// dežurni operater sektora B kojemu je istekla privremena dužnost
	jucer := time.Now().Add(-24 * time.Hour)
	operater := o.osoba(t, "operater", service.AddDutyRequest{Role: models.RoleOperator, SectorID: sektorP("B"), IsTemporary: true, ExpiresAt: &jucer})
	d.UserID = operater.ID
	if err := o.users.AddDuty(perm5, d); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja 5 zadužila je račun kojemu je istekla dužnost: %v", err)
	}
	// ni produljenje istekle dužnosti odozdo, kad drugih aktivnih nema
	rukB := o.osoba(t, "rukb", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	var istekla models.Duty
	if err := o.db.QueryRow(`SELECT id FROM duties WHERE user_id = ?`, operater.ID.String()).Scan(&istekla.ID); err != nil {
		t.Fatal(err)
	}
	sutra := time.Now().Add(24 * time.Hour)
	produlji := service.AddDutyRequest{UserID: operater.ID, Role: models.RoleOperator, SectorID: sektorP("B"), IsTemporary: true, ExpiresAt: &sutra}
	if err := o.users.UpdateDuty(o.ovlasti(t, rukB.ID), istekla.ID, produlji); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava sektora B produljila je isteklu dužnost računa bez drugih dužnosti: %v", err)
	}
	if err := o.users.UpdateDuty(o.admin, istekla.ID, produlji); err != nil {
		t.Errorf("globalni administrator ne produljuje isteklu dužnost: %v", err)
	}

	// račun koji je globalni administrator otvorio bez dužnosti
	novi, err := o.users.CreateUser(o.admin, service.CreateUserRequest{Username: "novi", Password: "lozinka-novi", FullName: "Novi", OrgType: models.OrgHrvatskeVode})
	if err != nil {
		t.Fatal(err)
	}
	d.UserID = novi.ID
	if err := o.users.AddDuty(perm5, d); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja 5 zadužila je račun otvoren bez dužnosti: %v", err)
	}
	if err := o.users.DeleteUser(perm5, novi.ID); err == nil {
		t.Error("uprava područja 5 obrisala je račun otvoren bez dužnosti")
	}
	// ni globalnog administratora
	d.UserID = o.sef.ID
	if err := o.users.AddDuty(perm5, d); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava područja 5 zadužila je globalnog administratora: %v", err)
	}

	// globalni administrator zadužuje račun bez dužnosti; poslije ga uprava
	// područja smije i dalje zaduživati
	d.UserID = novi.ID
	if err := o.users.AddDuty(o.admin, d); err != nil {
		t.Fatalf("globalni administrator: %v", err)
	}
	if err := o.users.AddDuty(perm5, service.AddDutyRequest{UserID: novi.ID, Role: models.RoleMachinist, AreaID: podrucjeP(5)}); err != nil {
		t.Errorf("uprava područja 5 ne zadužuje svog vodočuvara: %v", err)
	}
	// novi račun s početnom dužnošću otvara uprava područja kao i dosad
	if _, err := o.users.CreateUser(perm5, service.CreateUserRequest{Username: "vod5", Password: "lozinka-vod5", FullName: "Vod 5",
		OrgType: models.OrgHrvatskeVode, Role: models.RoleWaterGuard, AreaID: podrucjeP(5)}); err != nil {
		t.Errorf("uprava područja 5 ne otvara račun s dužnošću: %v", err)
	}
}

// Primarnu dužnost i vlastiti naziv daje samo onaj tko smije uređivati cijeli
// račun: naziv dužnosti ide u certifikat potpisa i uz ime. Uprava koja
// dodaje dužnost osobi s dužnostima izvan svog dosega daje ispomoć pod
// nazivom uloge.
func TestDuznostIzvanDosegaJeIspomoc(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ruk16 := o.osoba(t, "ruk16", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	perm16 := o.ovlasti(t, ruk16.ID)

	// vodočuvar iz sektora A dolazi u ispomoć u područje 16
	gost := o.osoba(t, "gost", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5), IsPrimary: true})
	if err := o.users.AddDuty(perm16, service.AddDutyRequest{UserID: gost.ID, Role: models.RoleSectionLeader, SectionCodes: "B.16.1",
		Title: "Rukovoditelj sektora B", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	svjez, _ := o.repo.GetUserByID(gost.ID)
	for _, d := range svjez.Duties {
		if d.Role != models.RoleSectionLeader {
			continue
		}
		if d.IsPrimary || d.Title != models.RoleSectionLeader.Label() {
			t.Errorf("dužnost izvan dosega uprave: primarna=%v naziv=%q", d.IsPrimary, d.Title)
		}
		// ni izmjenom ne postaje primarna ni ne dobije vlastiti naziv: takva
		// se izmjena odbija, a ne sprema tiho s drugim nazivom
		if err := o.users.UpdateDuty(perm16, d.ID, service.AddDutyRequest{UserID: gost.ID, Role: models.RoleSectionLeader, SectionCodes: "B.16.1",
			Title: "Glavni rukovoditelj"}); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("izmjena vlastitog naziva izvan dosega uprave: %v", err)
		}
		if err := o.users.UpdateDuty(perm16, d.ID, service.AddDutyRequest{UserID: gost.ID, Role: models.RoleSectionLeader, SectionCodes: "B.16.1",
			Title: d.Title, IsPrimary: true}); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("izmjena u primarnu izvan dosega uprave: %v", err)
		}
		iz, _ := o.repo.GetDuty(d.ID)
		if iz.IsPrimary || iz.Title != models.RoleSectionLeader.Label() {
			t.Errorf("izmjena izvan dosega uprave: primarna=%v naziv=%q", iz.IsPrimary, iz.Title)
		}
	}
	if o.users.SmijeUredjivatiRacun(perm16, gost.ID) {
		t.Error("uprava područja 16 smije uređivati račun s dužnošću u sektoru A")
	}

	// vlastiti vodočuvar: naziv i primarnost ostaju kako ih uprava upiše
	vod := o.osoba(t, "vod16", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(16)})
	if err := o.users.AddDuty(perm16, service.AddDutyRequest{UserID: vod.ID, Role: models.RoleSectionLeader, SectionCodes: "B.16.1",
		Title: "Rukovoditelj dionice Batina", IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	svjez, _ = o.repo.GetUserByID(vod.ID)
	nasao := false
	for _, d := range svjez.Duties {
		if d.Role == models.RoleSectionLeader && d.IsPrimary && d.Title == "Rukovoditelj dionice Batina" {
			nasao = true
		}
	}
	if !nasao {
		t.Errorf("dužnost u dosegu uprave izgubila je naziv ili primarnost: %+v", svjez.Duties)
	}
}

// Vlastiti naziv dužnosti, koji je dao onaj tko uređuje cijeli račun, uprava
// izvan tog dosega pri izmjeni ne smije ni promijeniti ni tiho zamijeniti
// nazivom uloge: naziv ide uz ime i u certifikat potpisa. Izmjena s
// nepromijenjenim nazivom prolazi i naziv ostaje; promjena naziva se odbija.
func TestIzmjenaDuznostiCuvaVlastitiNaziv(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	rukB := o.osoba(t, "rukb", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	permB := o.ovlasti(t, rukB.ID)
	// osoba s primarnom dužnošću u sektoru A; globalni administrator joj da
	// dužnost u sektoru B s vlastitim nazivom
	gost := o.osoba(t, "gost", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5), IsPrimary: true})
	naziv := "Zamjenik rukovoditelja BP 16 (ispomoc)"
	if err := o.users.AddDuty(o.admin, service.AddDutyRequest{UserID: gost.ID, Role: models.RoleAreaDeputy, AreaID: podrucjeP(16), Title: naziv}); err != nil {
		t.Fatal(err)
	}
	if o.users.SmijeUredjivatiRacun(permB, gost.ID) {
		t.Fatal("uprava sektora B smije uređivati račun s primarnom dužnošću u sektoru A")
	}
	var d models.Duty
	svjez, _ := o.repo.GetUserByID(gost.ID)
	for _, x := range svjez.Duties {
		if x.Role == models.RoleAreaDeputy {
			d = x
		}
	}
	if d.Title != naziv {
		t.Fatalf("polazište: naziv %q", d.Title)
	}
	// ispravak slova u nazivu: odbijen, naziv ostaje
	if err := o.users.UpdateDuty(permB, d.ID, service.AddDutyRequest{UserID: gost.ID, Role: models.RoleAreaDeputy, AreaID: podrucjeP(16),
		Title: "Zamjenik rukovoditelja BP 16 (ispomoć)"}); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("uprava sektora B promijenila je vlastiti naziv dužnosti: %v", err)
	}
	if iz, _ := o.repo.GetDuty(d.ID); iz.Title != naziv {
		t.Errorf("odbijena izmjena promijenila je naziv u %q", iz.Title)
	}
	// izmjena roka uz nepromijenjen naziv: prolazi, naziv ostaje
	if err := o.users.UpdateDuty(permB, d.ID, service.AddDutyRequest{UserID: gost.ID, Role: models.RoleAreaDeputy, AreaID: podrucjeP(16),
		Title: naziv, IsTemporary: true, Reason: "ispomoć u obrani"}); err != nil {
		t.Fatal(err)
	}
	if iz, _ := o.repo.GetDuty(d.ID); iz.Title != naziv || !iz.IsTemporary {
		t.Errorf("izmjena uz nepromijenjen naziv: naziv %q, privremena %v", iz.Title, iz.IsTemporary)
	}
	// globalni administrator naziv mijenja
	if err := o.users.UpdateDuty(o.admin, d.ID, service.AddDutyRequest{UserID: gost.ID, Role: models.RoleAreaDeputy, AreaID: podrucjeP(16),
		Title: "Zamjenik rukovoditelja BP 16"}); err != nil {
		t.Fatal(err)
	}
	if iz, _ := o.repo.GetDuty(d.ID); iz.Title != "Zamjenik rukovoditelja BP 16" {
		t.Errorf("globalni administrator nije promijenio naziv: %q", iz.Title)
	}
}
