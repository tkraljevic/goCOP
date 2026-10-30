package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Tko ovjerava dnevni list (Odluke (30. 10. 2026.), 8): voditelj COP-a i
// voditelj glavnog centra po dužnosti ne ovjeravaju, ali ovjeravaju list
// na kojem stoji zadatak koji su zadali. Uprava sektora, glavni
// rukovoditelj i globalni administrator ovjeravaju i kad područje ima
// rukovoditelja, ali tada potvrđuju da ovjeravaju umjesto njega.
// Rukovoditelj područja potvrdu ne treba. Podaci su izmišljeni.
func TestOvjeraListaPoDuznostiIZadatku(t *testing.T) {
	k := novaKnjigaProba(t)
	ctx := context.Background()
	vs := k.o.vs
	// svaki list je za svoj dan: jedan list po vodočuvaru i danu
	dan := 0
	predaj := func(zadao *models.UserPermissions) *models.VodocuvarskiList {
		t.Helper()
		kad := vs.sada()
		dan++
		l := &models.VodocuvarskiList{UserID: k.pero.ID.String(), Ime: k.pero.FullName, Sektor: k.sektor, AreaID: k.p1,
			Datum: kad.AddDate(0, 0, -dan), Broj: dan, Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", PredanoAt: &kad}
		if zadao != nil {
			z := &models.Zadatak{ID: uuid.NewString(), UserID: k.pero.ID.String(), Sektor: k.sektor, AreaID: k.p1, Tekst: "pregledati propust",
				ZadaoID: zadao.User.ID.String(), Zadao: zadao.User.FullName, ZadanoAt: kad, Status: models.ZadatakObavljen}
			if err := k.o.repo.SaveZadatak(ctx, z); err != nil {
				t.Fatal(err)
			}
			l.Zadaci = []models.ZadatakNaListu{{ID: z.ID, Tekst: z.Tekst, Zadao: z.Zadao, ZadanoAt: kad, Status: z.Status}}
		}
		if err := k.o.repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	ovjeri := func(naziv string, perms *models.UserPermissions, l *models.VodocuvarskiList, umjesto bool, ocekivano error) {
		t.Helper()
		_, err := vs.Ovjeri(ctx, perms, &perms.User, l.ID, umjesto)
		if !errors.Is(err, ocekivano) {
			t.Errorf("%s: ovjera %v, očekivano %v", naziv, err, ocekivano)
		}
	}

	cop := k.osoba(models.Duty{Role: models.RoleCopLeader, ScopeType: models.ScopeSector})
	centar := k.osoba(models.Duty{Role: models.RoleMainCenterLeader, ScopeType: models.ScopeAll})
	bezZadatka := predaj(nil)
	for naziv, perms := range map[string]*models.UserPermissions{"voditelj COP-a": cop, "voditelj glavnog centra": centar} {
		if vs.SmijeOvjeriti(perms, bezZadatka) {
			t.Errorf("%s nudi ovjeru lista bez svog zadatka", naziv)
		}
		ovjeri(naziv+" bez svog zadatka", perms, bezZadatka, true, ErrUnauthorized)
		if !vs.SmijeVidjeti(perms, bezZadatka) || !vs.SmijeParafirati(perms, bezZadatka) {
			t.Errorf("%s više ne čita ili ne parafira list", naziv)
		}
	}
	sCOPZadatkom := predaj(cop)
	if !vs.SmijeOvjeriti(cop, sCOPZadatkom) || vs.OvjeravaUmjesto(ctx, cop, sCOPZadatkom) {
		t.Error("voditelj COP-a ne ovjerava list sa svojim zadatkom, ili traži potvrdu")
	}
	if vs.SmijeOvjeriti(centar, sCOPZadatkom) {
		t.Error("tuđi zadatak na listu otvara ovjeru")
	}
	ovjeri("voditelj COP-a uz svoj zadatak", cop, sCOPZadatkom, false, nil)

	// tko je zadao zadatak, a dužnost mu je istekla, ne ovjerava
	prosla := time.Now().Add(-time.Hour)
	bivsiCOP := models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Bivši voditelj", IsActive: true,
		Duties: []models.Duty{{Role: models.RoleCopLeader, ScopeType: models.ScopeSector, SectorID: &k.sektor, IsActive: true, ExpiresAt: &prosla}}})
	if vs.SmijeOvjeriti(bivsiCOP, predaj(bivsiCOP)) {
		t.Error("zadatak uz isteklu dužnost otvara ovjeru")
	}
	// dužnost globalnog administratora ovjerava kao i račun administratora
	dAdmin := models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Admin po dužnosti", IsActive: true,
		Duties: []models.Duty{{Role: models.RoleGlobalAdmin, ScopeType: models.ScopeAll, IsActive: true}}})
	if !vs.SmijeOvjeriti(dAdmin, bezZadatka) {
		t.Error("dužnost globalnog administratora ne ovjerava")
	}

	sektor := k.osoba(models.Duty{Role: models.RoleSectorLeader, ScopeType: models.ScopeSector})
	glavni := k.osoba(models.Duty{Role: models.RoleNationalLeader, ScopeType: models.ScopeAll})
	admin := models.NewUserPermissions(models.User{ID: uuid.New(), FullName: "Admin", IsActive: true, IsGlobalAdmin: true})
	// dok područje nema rukovoditelja, ovjeravaju bez potvrde
	for naziv, perms := range map[string]*models.UserPermissions{"uprava sektora": sektor, "glavni rukovoditelj": glavni, "administrator": admin} {
		l := predaj(nil)
		if !vs.SmijeOvjeriti(perms, l) || vs.OvjeravaUmjesto(ctx, perms, l) {
			t.Errorf("%s bez rukovoditelja područja: ovjera %v, potvrda %v", naziv, vs.SmijeOvjeriti(perms, l), vs.OvjeravaUmjesto(ctx, perms, l))
		}
		ovjeri(naziv+" bez rukovoditelja područja", perms, l, false, nil)
	}

	// istekli račun rukovoditelja područja ne traži potvrdu
	p1 := k.p1
	bivsi := &models.User{Username: "bbivsi", FullName: "Bruno Bivši"}
	if err := k.users.CreateUser(bivsi, &models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &k.sektor, AreaID: &p1, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if vs.OvjeravaUmjesto(ctx, sektor, predaj(nil)) {
		t.Error("neaktivan račun rukovoditelja područja traži potvrdu")
	}
	ante := &models.User{Username: "aantic", FullName: "Ante Antić", IsActive: true}
	duznostAnte := models.Duty{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &k.sektor, AreaID: &p1, IsActive: true}
	if err := k.users.CreateUser(ante, &duznostAnte); err != nil {
		t.Fatal(err)
	}
	ante.Duties = []models.Duty{duznostAnte}
	for naziv, perms := range map[string]*models.UserPermissions{"uprava sektora": sektor, "glavni rukovoditelj": glavni, "administrator": admin} {
		l := predaj(nil)
		if !vs.OvjeravaUmjesto(ctx, perms, l) {
			t.Errorf("%s ne traži potvrdu kad područje ima rukovoditelja", naziv)
		}
		ovjeri(naziv+" bez potvrde", perms, l, false, ErrOvjeraUmjesto)
		if spremljen, err := k.o.repo.Get(ctx, l.ID); err != nil || spremljen == nil || spremljen.Potvrden() {
			t.Errorf("%s: list ovjeren bez potvrde", naziv)
		}
		ovjeri(naziv+" uz potvrdu", perms, l, true, nil)
	}
	rukovoditelj := models.NewUserPermissions(*ante)
	l := predaj(nil)
	if vs.OvjeravaUmjesto(ctx, rukovoditelj, l) {
		t.Error("rukovoditelj područja traži potvrdu")
	}
	ovjeri("rukovoditelj područja", rukovoditelj, l, false, nil)

	// kad je rukovoditelj područja ujedno vodočuvar lista, list ne može
	// ovjeriti sam, pa ga uprava sektora ovjerava bez potvrde
	antinList := &models.VodocuvarskiList{UserID: ante.ID.String(), Sektor: k.sektor, AreaID: k.p1}
	if vs.OvjeravaUmjesto(ctx, sektor, antinList) {
		t.Error("vodočuvar lista se broji kao rukovoditelj koji može ovjeriti")
	}
}

// Kad se rukovoditelji područja ne daju pročitati, program pita potvrdu;
// bez izvora osoba ne pita
func TestOvjeraUmjestoBezPopisaOsoba(t *testing.T) {
	k := novaKnjigaProba(t)
	l := &models.VodocuvarskiList{UserID: k.pero.ID.String(), Sektor: k.sektor, AreaID: k.p1}
	sektor := k.osoba(models.Duty{Role: models.RoleSectorLeader, ScopeType: models.ScopeSector})
	if _, err := k.o.baza.Exec(`DROP TABLE duties`); err != nil {
		t.Fatal(err)
	}
	if !k.o.vs.OvjeravaUmjesto(context.Background(), sektor, l) {
		t.Error("greška čitanja ne traži potvrdu")
	}
	k.o.vs.users = nil
	if k.o.vs.OvjeravaUmjesto(context.Background(), sektor, l) {
		t.Error("bez izvora osoba traži potvrdu")
	}
}
