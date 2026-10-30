package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Vrsta dnevnika usluge ne mijenja se nakon otvaranja: po njoj se zna
// program ugovora, a time i tko je nadzor. Ovlaštenik A3, koji smije
// mijenjati naslovnicu na svom području, dnevnik A.02 ne pretvara u A.03.
// Naslovnica s istom vrstom i dalje se sprema.
func TestVrstaDnevnikaUslugeSeNeMijenja(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	podrucje := models.Area{ID: 15, SectorID: "B"}
	a3, ovlA3 := ovlastenik(15, models.RoleContractOfficerA3)
	rukovoditelj, ovlRuk := osobaUSektoruB(models.RoleAreaLeader, models.ScopeArea, 15)

	for tko, o := range map[string]struct {
		u *models.User
		p *models.UserPermissions
	}{"ovlaštenik A3": {a3, ovlA3}, "rukovoditelj područja": {rukovoditelj, ovlRuk}} {
		j := *p.uslugaB
		j.Kind = models.JournalKindMaintenanceA03
		if err := p.s.SaveJournal(ctx, o.u, o.p, podrucje, &j); err == nil || !strings.Contains(err.Error(), "ne mijenja se nakon otvaranja") {
			t.Errorf("%s mijenja vrstu dnevnika: %v", tko, err)
		}
	}
	if cur, err := p.s.GetJournal(ctx, p.uslugaB.ID); err != nil || cur == nil || cur.Kind != models.JournalKindMaintenanceA02 {
		t.Fatalf("vrsta dnevnika je promijenjena: %+v", cur)
	}
	if p.s.CanSupervise(a3, ovlA3, p.oUB, p.uslugaB) {
		t.Error("ovlaštenik A3 je nadzor u dnevniku A.02")
	}
	// kontrola: ista vrsta, nova naslovnica
	for tko, o := range map[string]struct {
		u *models.User
		p *models.UserPermissions
	}{"ovlaštenik A3": {a3, ovlA3}, "rukovoditelj područja": {rukovoditelj, ovlRuk}} {
		j := *p.uslugaB
		j.Contractor = "Primjer d.o.o."
		if err := p.s.SaveJournal(ctx, o.u, o.p, podrucje, &j); err != nil {
			t.Errorf("%s ne sprema naslovnicu: %v", tko, err)
		}
	}
	// dnevnik koji nije na području ne mijenja se
	j := *p.uslugaB
	if err := p.s.SaveJournal(ctx, rukovoditelj, ovlRuk, models.Area{ID: 30, SectorID: "D"}, &j); err == nil {
		t.Error("naslovnica dnevnika s drugog područja je spremljena")
	}
}

// Svaka strana list potvrđuje jednom: drugi ovlaštenik ili drugi poslovođa
// ne prepisuje ime i vrijeme potvrde koja već stoji.
func TestListSePotvrdujeJednom(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	list, _ := p.nalog(t, p.uslugaB, p.oUB)
	prvi, ovlPrvi := ovlastenik(15, models.RoleContractOfficerA2)
	prvi.FullName = "Prvi ovlaštenik"
	drugi, ovlDrugi := ovlastenik(15, models.RoleContractDeputyA2)
	drugi.FullName = "Drugi ovlaštenik"
	izvodjac, ovlIzv := osobaUSektoruB(models.RoleServiceLeaderForeman, models.ScopeArea, 15)
	izvodjac2, ovlIzv2 := osobaUSektoruB(models.RoleServiceLeaderForeman, models.ScopeArea, 15)
	izvodjac2.FullName = "Drugi poslovođa"

	if err := p.s.ConfirmSheet(ctx, prvi, ovlPrvi, p.oUB, p.uslugaB, list.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.s.ConfirmSheet(ctx, drugi, ovlDrugi, p.oUB, p.uslugaB, list.ID); err == nil || !strings.Contains(err.Error(), "već potvrdio Prvi ovlaštenik") {
		t.Errorf("druga potvrda za nadzor: %v", err)
	}
	if err := p.s.ConfirmSheet(ctx, izvodjac, ovlIzv, p.oUB, p.uslugaB, list.ID); err != nil {
		t.Errorf("izvođač ne potvrđuje list: %v", err)
	}
	if err := p.s.ConfirmSheet(ctx, izvodjac2, ovlIzv2, p.oUB, p.uslugaB, list.ID); err == nil || !strings.Contains(err.Error(), "za izvođača već potvrdio") {
		t.Errorf("druga potvrda za izvođača: %v", err)
	}
	sh, err := p.s.GetSheet(ctx, list.ID)
	if err != nil || sh == nil || sh.SupervisorConfirmedBy != "Prvi ovlaštenik" || sh.ContractorConfirmedBy != izvodjac.FullName {
		t.Errorf("potvrde lista: %+v", sh)
	}
}

// Nalog izvedenim označava izvođač ili nadzor; dežurni i rukovoditelj
// područja, koji u dnevniku usluge nisu nadzor, ne.
func TestNalogIzvedenimIzvodjacIliNadzor(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	_, nalog := p.nalog(t, p.uslugaB, p.oUB)
	dezurni, ovlDez := osobaUSektoruB(models.RoleOperator, models.ScopeSector, 0)
	rukovoditelj, ovlRuk := osobaUSektoruB(models.RoleAreaLeader, models.ScopeArea, 15)
	for tko, o := range map[string]struct {
		u *models.User
		p *models.UserPermissions
	}{"dežurni": {dezurni, ovlDez}, "rukovoditelj područja": {rukovoditelj, ovlRuk}} {
		if err := p.s.SetTaskStatus(ctx, o.u, o.p, p.oUB, p.uslugaB, nalog.ID, models.TaskDone); err == nil || !strings.Contains(err.Error(), "izvođač ili nadzor") {
			t.Errorf("%s označava nalog izvedenim: %v", tko, err)
		}
	}
	if p.upis(t, nalog.ID).Status != models.TaskOpen {
		t.Fatal("nalog nije otvoren")
	}
	// kontrola: izvođač, pa nadzor na drugom nalogu
	izvodjac, ovlIzv := osobaUSektoruB(models.RoleServiceLeaderForeman, models.ScopeArea, 15)
	if err := p.s.SetTaskStatus(ctx, izvodjac, ovlIzv, p.oUB, p.uslugaB, nalog.ID, models.TaskDone); err != nil || p.upis(t, nalog.ID).Status != models.TaskDone {
		t.Errorf("izvođač ne označava nalog izvedenim: %v", err)
	}
	_, drugi := p.nalog(t, p.uslugaB, p.oUB)
	ou, op := ovlastenik(15, models.RoleContractOfficerA2)
	if err := p.s.SetTaskStatus(ctx, ou, op, p.oUB, p.uslugaB, drugi.ID, models.TaskDone); err != nil {
		t.Errorf("nadzor ne označava nalog izvedenim: %v", err)
	}
}

// Naslovnica se mijenja samo uz pravo nad spremljenim dnevnikom: poslani
// dnevnik koji se predstavlja kao dnevnik COP-a ne daje dežurnom pravo na
// naslovnicu dnevnika usluge. Dnevnik kojeg na području nema ne mijenja se,
// a greška čitanja ne sprema ništa.
func TestNaslovnicaGledaSpremljeniDnevnik(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	podrucje := models.Area{ID: 15, SectorID: "B"}
	dezurni, ovlDez := osobaUSektoruB(models.RoleOperator, models.ScopeSector, 0)
	j := *p.uslugaB
	j.CentarSektor = "B"
	if err := p.s.SaveJournal(ctx, dezurni, ovlDez, podrucje, &j); err == nil || !strings.Contains(err.Error(), "ovlaštenik za praćenje ugovora") {
		t.Errorf("dežurni mijenja naslovnicu dnevnika usluge: %v", err)
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	tudji := *p.uslugaD
	if err := p.s.SaveJournal(ctx, &models.User{ID: uuid.New()}, admin, podrucje, &tudji); err == nil || !strings.Contains(err.Error(), "nije pronađen") {
		t.Errorf("dnevnik s drugog područja: %v", err)
	}
	otkazan, prekini := context.WithCancel(ctx)
	prekini()
	k := *p.uslugaB
	if err := p.s.SaveJournal(otkazan, &models.User{ID: uuid.New()}, admin, podrucje, &k); err == nil {
		t.Error("naslovnica spremljena uz grešku čitanja")
	}
	// kontrola: globalni administrator mijenja naslovnicu
	k = *p.uslugaB
	k.Contractor = "Primjer d.o.o."
	if err := p.s.SaveJournal(ctx, &models.User{ID: uuid.New()}, admin, podrucje, &k); err != nil {
		t.Errorf("globalni administrator ne mijenja naslovnicu: %v", err)
	}
}

// Stariji dnevnik obrane po području briše tko mu smije mijenjati
// naslovnicu, a bez ovlasti nitko ne briše dnevnik usluge.
func TestBriseDnevnikStarijiIBezOvlasti(t *testing.T) {
	p := novaUpisiProba(t)
	stari := &models.Journal{Kind: models.JournalKindDefense, AreaID: 15, Title: "Obrana 15", Year: 2025}
	rukovoditelj, ovlRuk := osobaUSektoruB(models.RoleAreaLeader, models.ScopeArea, 15)
	if !p.s.BriseDnevnik(rukovoditelj, ovlRuk, p.oUB, stari) {
		t.Error("rukovoditelj područja ne briše stariji dnevnik obrane po području")
	}
	if p.s.BriseDnevnik(rukovoditelj, ovlRuk, p.oUB, p.uslugaB) {
		t.Error("rukovoditelj područja briše dnevnik usluge")
	}
	if p.s.BriseDnevnik(&models.User{ID: uuid.New()}, nil, p.oUB, p.uslugaB) {
		t.Error("bez ovlasti dnevnik usluge se briše")
	}
}
