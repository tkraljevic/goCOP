package service

import (
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Popis vodočuvara za zadavanje zadataka: isključen račun vodočuvara na
// njega ne ulazi, a vodočuvar koji se još nije prijavio ulazi (zadatak ga
// čeka do prve prijave). Podaci su izmišljeni: sektor P, područje 1.
func TestPopisVodocuvaraBezIskljucenihRacuna(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	users := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test"))
	o.vs.users = NewUserService(users, nil, nil)

	upisi := func(korisnik, ime string, aktivan bool) *models.User {
		t.Helper()
		u := &models.User{Username: korisnik, FullName: ime, IsActive: aktivan}
		d := vdDuznost(models.RoleWaterGuard, 1, true)
		if err := users.CreateUser(u, &d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	prijavljen := upisi("pperic", "Pero Perić", true)
	if err := users.MarkLogin(prijavljen.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	neprijavljen := upisi("mmarkic", "Marko Markić", true)
	iskljucen := upisi("iivic", "Ivo Ivić", false)

	rukovoditelj := models.NewUserPermissions(*vdPperic(vdDuznost(models.RoleAreaLeader, 1, true)))
	popis := map[string]bool{}
	for _, v := range o.vs.Vodocuvari(t.Context(), rukovoditelj) {
		popis[v.ID.String()] = true
	}
	if !popis[prijavljen.ID.String()] {
		t.Error("vodočuvar koji se prijavljivao nije na popisu")
	}
	if !popis[neprijavljen.ID.String()] {
		t.Error("vodočuvar koji se još nije prijavio nije na popisu")
	}
	if popis[iskljucen.ID.String()] {
		t.Error("isključen račun vodočuvara je na popisu za zadavanje zadataka")
	}
	if len(popis) != 2 {
		t.Errorf("na popisu %d vodočuvara, očekivana 2", len(popis))
	}
}

// Zadatak se ne zadaje isključenom računu ni kad ga zahtjev navede ručno,
// mimo popisa: zadavanje gleda isto što i popis. Vodočuvaru s aktivnim
// računom zadatak se zadaje, a osobi bez terenskog zaduženja ne.
func TestZadatakNeIdeIskljucenomRacunu(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := t.Context()
	users := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test"))
	o.vs.users = NewUserService(users, nil, nil)
	upisi := func(korisnik, ime string, aktivan bool, uloga models.Role) *models.User {
		t.Helper()
		u := &models.User{Username: korisnik, FullName: ime, IsActive: aktivan}
		d := vdDuznost(uloga, 1, true)
		if err := users.CreateUser(u, &d); err != nil {
			t.Fatal(err)
		}
		return u
	}
	aktivan := upisi("pperic", "Pero Perić", true, models.RoleWaterGuard)
	iskljucen := upisi("iivic", "Ivo Ivić", false, models.RoleWaterGuard)
	strojar := upisi("mmarkic", "Marko Markić", true, models.RoleMachinist)

	ruk := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true))
	rukovoditelj := models.NewUserPermissions(*ruk)
	if _, err := o.vs.ZadajZadatak(ctx, rukovoditelj, ruk, aktivan.ID.String(), "pregledati ustavu", time.Time{}); err != nil {
		t.Fatalf("zadatak aktivnom vodočuvaru: %v", err)
	}
	z, err := o.vs.ZadajZadatak(ctx, rukovoditelj, ruk, iskljucen.ID.String(), "pregledati ustavu", time.Time{})
	vdGreska(t, err, "Ivo Ivić ima isključen račun pa ne prima zadatke")
	if z != nil {
		t.Errorf("isključenom računu zadan je zadatak: %+v", z)
	}
	if zadaci := o.vs.Zadaci(ctx, iskljucen.ID.String()); len(zadaci) != 0 {
		t.Errorf("isključen račun ima zadatke: %+v", zadaci)
	}
	_, err = o.vs.ZadajZadatak(ctx, rukovoditelj, ruk, strojar.ID.String(), "pregledati ustavu", time.Time{})
	vdGreska(t, err, "Marko Markić nema terensko zaduženje pa nema ni dnevnik")
}
