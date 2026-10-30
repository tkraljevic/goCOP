package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// osobaUSektoruB je osoba s jednom aktivnom dužnošću u sektoru B (područje
// 15 kad je zadano) i ovlasti koje iz nje slijede
func osobaUSektoruB(uloga models.Role, doseg models.ScopeType, podrucje int) (*models.User, *models.UserPermissions) {
	sektor := "B"
	d := models.Duty{Role: uloga, ScopeType: doseg, SectorID: &sektor, IsActive: true}
	if podrucje > 0 {
		d.AreaID = &podrucje
	}
	p := models.NewUserPermissions(models.User{ID: uuid.New(), FullName: string(uloga), Duties: []models.Duty{d}})
	return &p.User, p
}

// Nadzor u dnevniku usluge je ovlaštenik za praćenje ugovora programa
// dnevnika na njegovu području, i njegov zamjenik (Odluke (30. 10. 2026.),
// 4). Dežurni operater, rukovoditelj područja, ovlaštenik drugog programa i
// ovlaštenik drugog područja ne pišu nalog, ne otkazuju ga i ne potvrđuju
// list za nadzor, a napomenu i dalje pišu. U dnevniku COP-a dežurni i
// dalje stornira tuđi zapis.
func TestNadzorUslugeJeOvlastenik(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	list, nalog := p.nalog(t, p.uslugaB, p.oUB)

	nisu := map[string]func() (*models.User, *models.UserPermissions){
		"dežurni operater": func() (*models.User, *models.UserPermissions) {
			return osobaUSektoruB(models.RoleOperator, models.ScopeSector, 0)
		},
		"rukovoditelj područja": func() (*models.User, *models.UserPermissions) {
			return osobaUSektoruB(models.RoleAreaLeader, models.ScopeArea, 15)
		},
		"ovlaštenik A3":         func() (*models.User, *models.UserPermissions) { return ovlastenik(15, models.RoleContractOfficerA3) },
		"ovlaštenik A2 drugdje": func() (*models.User, *models.UserPermissions) { return ovlastenik(30, models.RoleContractOfficerA2) },
	}
	for tko, osoba := range nisu {
		u, perms := osoba()
		if p.s.CanSupervise(u, perms, p.oUB, p.uslugaB) {
			t.Errorf("%s je nadzor u dnevniku usluge A.02", tko)
		}
		novi := &models.JournalEntry{Kind: models.EntryKindTask, Text: "očistiti propust"}
		if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, novi); err == nil {
			t.Errorf("%s piše nalog", tko)
		}
		if err := p.s.ConfirmSheet(ctx, u, perms, p.oUB, p.uslugaB, list.ID); err == nil {
			t.Errorf("%s potvrđuje list za nadzor", tko)
		}
		if err := p.s.SetTaskStatus(ctx, u, perms, p.oUB, p.uslugaB, nalog.ID, models.TaskCancelled); err == nil {
			t.Errorf("%s otkazuje nalog", tko)
		}
	}
	if sh, err := p.s.GetSheet(ctx, list.ID); err != nil || sh == nil || sh.SupervisorConfirmedAt != nil {
		t.Errorf("list je potvrđen za nadzor: %+v", sh)
	}
	if p.upis(t, nalog.ID).Status != models.TaskOpen {
		t.Error("nalog je otkazan")
	}
	poruka := ""
	u, perms := osobaUSektoruB(models.RoleOperator, models.ScopeSector, 0)
	if err := p.s.ConfirmSheet(ctx, u, perms, p.oUB, p.uslugaB, list.ID); err != nil {
		poruka = err.Error()
	}
	if !strings.Contains(poruka, "ovlaštenik za praćenje ugovora") {
		t.Errorf("poruka dežurnom: %q", poruka)
	}
	// dežurni i rukovoditelj i dalje pišu napomenu
	if _, err := p.s.AddEntry(ctx, u, perms, p.oUB, p.uslugaB, list, &models.JournalEntry{Kind: models.EntryKindNote, Text: "nasip pregledan"}); err != nil {
		t.Errorf("dežurni ne piše napomenu: %v", err)
	}

	// kontrola: ovlaštenik A2 na području 15, njegov zamjenik i globalni administrator
	zamjenik, ovlZ := ovlastenik(15, models.RoleContractDeputyA2)
	if !p.s.CanSupervise(zamjenik, ovlZ, p.oUB, p.uslugaB) {
		t.Error("zamjenik ovlaštenika A2 nije nadzor")
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	if !p.s.CanSupervise(&models.User{ID: uuid.New()}, admin, p.oUB, p.uslugaB) {
		t.Error("globalni administrator nije nadzor")
	}
	ou, op := ovlastenik(15, models.RoleContractOfficerA2)
	if err := p.s.SetTaskStatus(ctx, ou, op, p.oUB, p.uslugaB, nalog.ID, models.TaskCancelled); err != nil {
		t.Errorf("ovlaštenik ne otkazuje nalog: %v", err)
	}
	if err := p.s.ConfirmSheet(ctx, ou, op, p.oUB, p.uslugaB, list.ID); err != nil {
		t.Errorf("ovlaštenik ne potvrđuje list: %v", err)
	}
	if sh, err := p.s.GetSheet(ctx, list.ID); err != nil || sh == nil || sh.SupervisorConfirmedBy != ou.FullName {
		t.Errorf("list nije potvrđen za nadzor: %+v", sh)
	}

	// dnevnik COP-a: dežurni stornira tuđi zapis kao i dosad
	tudji := p.zapisCOP(t, p.copB, p.oB, "dojava")
	if err := p.s.VoidEntry(ctx, u, perms, p.oB, p.copB, tudji.ID, "krivo upisano"); err != nil {
		t.Errorf("dežurni ne stornira zapis u dnevniku COP-a: %v", err)
	}
}

// Naslovnicu dnevnika usluge mijenjaju ovlaštenik za praćenje ugovora i
// uprava područja ili sektora; izvođač, dežurni i rukovoditelj dionice ne.
// Dnevnik usluge briše samo uprava sektora ili globalni administrator
// (Odluke (30. 10. 2026.), 4).
func TestNaslovnicaIBrisanjeDnevnikaUsluge(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	podrucje := models.Area{ID: 15, SectorID: "B"}
	naslovnica := func(u *models.User, perms *models.UserPermissions) error {
		j := *p.uslugaB
		j.Contractor = "Primjer d.o.o."
		return p.s.SaveJournal(ctx, u, perms, podrucje, &j)
	}
	izvodjac, ovlIzv := osobaUSektoruB(models.RoleServiceLeaderForeman, models.ScopeArea, 15)
	dezurni, ovlDez := osobaUSektoruB(models.RoleOperator, models.ScopeSector, 0)
	dionicar, ovlDion := osobaUSektoruB(models.RoleSectionLeader, models.ScopeArea, 15)
	ovl, ovlOvl := ovlastenik(15, models.RoleContractOfficerA2)
	rukovoditelj, ovlRuk := osobaUSektoruB(models.RoleAreaLeader, models.ScopeArea, 15)
	sektor, ovlSek := osobaUSektoruB(models.RoleSectorLeader, models.ScopeSector, 0)

	for tko, o := range map[string]struct {
		u *models.User
		p *models.UserPermissions
	}{"izvođač": {izvodjac, ovlIzv}, "dežurni": {dezurni, ovlDez}, "rukovoditelj dionice": {dionicar, ovlDion}} {
		if err := naslovnica(o.u, o.p); err == nil || !strings.Contains(err.Error(), "ovlaštenik za praćenje ugovora") {
			t.Errorf("%s mijenja naslovnicu: %v", tko, err)
		}
	}
	for tko, o := range map[string]struct {
		u *models.User
		p *models.UserPermissions
	}{"ovlaštenik": {ovl, ovlOvl}, "rukovoditelj područja": {rukovoditelj, ovlRuk}, "uprava sektora": {sektor, ovlSek}} {
		if err := naslovnica(o.u, o.p); err != nil {
			t.Errorf("%s ne mijenja naslovnicu: %v", tko, err)
		}
	}

	for tko, o := range map[string]struct {
		u *models.User
		p *models.UserPermissions
	}{"izvođač": {izvodjac, ovlIzv}, "dežurni": {dezurni, ovlDez}, "ovlaštenik": {ovl, ovlOvl}, "rukovoditelj područja": {rukovoditelj, ovlRuk}} {
		if _, err := p.s.ObrisiDnevnik(ctx, o.u, o.p, p.oUB, p.uslugaB); err == nil || !strings.Contains(err.Error(), "uprava sektora ili globalni administrator") {
			t.Errorf("%s briše dnevnik usluge: %v", tko, err)
		}
	}
	if j, err := p.s.GetJournal(ctx, p.uslugaB.ID); err != nil || j == nil {
		t.Fatal("dnevnik usluge je obrisan")
	}
	// kontrola: uprava sektora briše svoj, globalni administrator i tuđi
	if _, err := p.s.ObrisiDnevnik(ctx, sektor, ovlSek, p.oUB, p.uslugaB); err != nil {
		t.Errorf("uprava sektora ne briše dnevnik usluge: %v", err)
	}
	if _, err := p.s.ObrisiDnevnik(ctx, sektor, ovlSek, p.oUD, p.uslugaD); err == nil {
		t.Error("uprava sektora B briše dnevnik usluge sektora D")
	}
	if _, err := p.s.ObrisiDnevnik(ctx, &models.User{ID: uuid.New()}, &models.UserPermissions{IsGlobalAdmin: true}, p.oUD, p.uslugaD); err != nil {
		t.Errorf("globalni administrator ne briše dnevnik usluge: %v", err)
	}
}

// Na listu koji su potvrdili izvođač i nadzor nema storna: upis ostaje kakav
// je bio, a poruka kaže da ispravak ide kao novi upis s napomenom (Odluke
// (30. 10. 2026.), 6). Na listu s jednom potvrdom storno radi kao i dosad.
func TestNemaStornaNaPotvrdenomListu(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	ou, op := ovlastenik(15, models.RoleContractOfficerA2)
	izvodjac, ovlIzv := osobaUSektoruB(models.RoleServiceLeaderForeman, models.ScopeArea, 15)
	list, nalog := p.nalog(t, p.uslugaB, p.oUB)
	rad := &models.JournalEntry{Kind: models.EntryKindWork, Text: "košnja nasipa"}
	if _, err := p.s.AddEntry(ctx, izvodjac, ovlIzv, p.oUB, p.uslugaB, list, rad); err != nil {
		t.Fatal(err)
	}

	// jedna potvrda: storno radi
	if err := p.s.ConfirmSheet(ctx, ou, op, p.oUB, p.uslugaB, list.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.s.VoidEntry(ctx, ou, op, p.oUB, p.uslugaB, nalog.ID, "nalog dan dvaput"); err != nil || !p.upis(t, nalog.ID).Voided {
		t.Errorf("storno na listu s jednom potvrdom: %v", err)
	}

	// obje potvrde: ni autor ni nadzor ne storniraju
	if err := p.s.ConfirmSheet(ctx, izvodjac, ovlIzv, p.oUB, p.uslugaB, list.ID); err != nil {
		t.Fatal(err)
	}
	for tko, o := range map[string]struct {
		u *models.User
		p *models.UserPermissions
	}{"izvođač": {izvodjac, ovlIzv}, "nadzor": {ou, op}} {
		err := p.s.VoidEntry(ctx, o.u, o.p, p.oUB, p.uslugaB, rad.ID, "krivi sat")
		if err == nil || !strings.Contains(err.Error(), "novi upis s napomenom") {
			t.Errorf("%s stornira upis na potvrđenom listu: %v", tko, err)
		}
	}
	if p.upis(t, rad.ID).Voided {
		t.Error("upis na potvrđenom listu je storniran")
	}
}
