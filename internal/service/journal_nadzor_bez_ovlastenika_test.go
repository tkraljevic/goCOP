package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Dok za program dnevnika usluge na području nema važećeg ovlaštenika za
// praćenje ugovora, nadzor su uprava područja i uprava sektora sa
// zamjenicima: pišu nalog, otkazuju ga i potvrđuju list za nadzor. Dežurni,
// rukovoditelj dionice, ovlaštenik drugog programa, uprava drugog područja
// ili sektora i uprava s isteklom dužnošću nisu nadzor. Čim ovlaštenik
// postoji, nadzor je samo on; uz grešku čitanja ili bez izvora uprava nije
// nadzor (Odluke (30. 10. 2026.)). Podaci su izmišljeni.
func TestNadzorUslugeBezOvlastenika(t *testing.T) {
	p := novaUpisiProba(t)
	ctx := context.Background()
	ovlastenici := map[string]bool{}
	var greska error
	pitano := ""
	p.s.SetImaOvlastenika(func(podrucje int, program string) (bool, error) {
		pitano = program
		return ovlastenici[program], greska
	})
	osoba := func(uloga models.Role, doseg models.ScopeType, sektor string, podrucje int) (*models.User, *models.UserPermissions) {
		d := models.Duty{Role: uloga, ScopeType: doseg, SectorID: &sektor, IsActive: true}
		if podrucje > 0 {
			d.AreaID = &podrucje
		}
		perms := models.NewUserPermissions(models.User{ID: uuid.New(), FullName: string(uloga), Duties: []models.Duty{d}})
		return &perms.User, perms
	}
	type tko struct {
		u     *models.User
		perms *models.UserPermissions
	}
	uprava := map[string]tko{}
	for ime, x := range map[string]struct {
		uloga    models.Role
		doseg    models.ScopeType
		podrucje int
	}{
		"rukovoditelj područja":                {models.RoleAreaLeader, models.ScopeArea, 15},
		"zamjenik rukovoditelja područja":      {models.RoleAreaDeputy, models.ScopeArea, 15},
		"zamjenik rukovoditelja sektora za BP": {models.RoleSectorAreaDeputy, models.ScopeArea, 15},
		"rukovoditelj sektora":                 {models.RoleSectorLeader, models.ScopeSector, 0},
		"zamjenik rukovoditelja sektora":       {models.RoleSectorDeputy, models.ScopeSector, 0},
		"voditelj centra":                      {models.RoleCopLeader, models.ScopeSector, 0},
	} {
		u, perms := osoba(x.uloga, x.doseg, "B", x.podrucje)
		uprava[ime] = tko{u, perms}
	}
	nije := map[string]tko{}
	for ime, x := range map[string]struct {
		uloga    models.Role
		doseg    models.ScopeType
		sektor   string
		podrucje int
	}{
		"dežurni operater":            {models.RoleOperator, models.ScopeSector, "B", 0},
		"rukovoditelj dionice":        {models.RoleSectionLeader, models.ScopeArea, "B", 15},
		"ovlaštenik A3":               {models.RoleContractOfficerA3, models.ScopeArea, "B", 15},
		"rukovoditelj drugog BP":      {models.RoleAreaLeader, models.ScopeArea, "D", 30},
		"rukovoditelj drugog sektora": {models.RoleSectorLeader, models.ScopeSector, "D", 0},
	} {
		u, perms := osoba(x.uloga, x.doseg, x.sektor, x.podrucje)
		nije[ime] = tko{u, perms}
	}
	istekla := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	bivsi, ovlBivsi := osoba(models.RoleAreaLeader, models.ScopeArea, "B", 15)
	ovlBivsi.User.Duties[0].ExpiresAt = &istekla
	nije["rukovoditelj područja s isteklom dužnošću"] = tko{bivsi, ovlBivsi}

	// nema ovlaštenika A.02: uprava je nadzor, ostali nisu
	for ime, x := range uprava {
		if !p.s.CanSupervise(x.u, x.perms, p.oUB, p.uslugaB) {
			t.Errorf("%s nije nadzor dok ovlaštenika nema", ime)
		}
	}
	if pitano != "A.02" {
		t.Errorf("pitan je program %q", pitano)
	}
	for ime, x := range nije {
		if p.s.CanSupervise(x.u, x.perms, p.oUB, p.uslugaB) {
			t.Errorf("%s je nadzor u dnevniku usluge A.02 područja 15", ime)
		}
	}

	// rukovoditelj područja piše nalog, otkazuje ga i potvrđuje list
	ruk := uprava["rukovoditelj područja"]
	list, err := p.s.NewSheet(ctx, ruk.u, ruk.perms, p.oUB, p.uslugaB, p.pocetak, "")
	if err != nil {
		t.Fatal(err)
	}
	nalog := &models.JournalEntry{Kind: models.EntryKindTask, Text: "očistiti propust"}
	if _, err := p.s.AddEntry(ctx, ruk.u, ruk.perms, p.oUB, p.uslugaB, list, nalog); err != nil {
		t.Fatalf("rukovoditelj područja ne piše nalog: %v", err)
	}
	if err := p.s.SetTaskStatus(ctx, ruk.u, ruk.perms, p.oUB, p.uslugaB, nalog.ID, models.TaskCancelled); err != nil {
		t.Errorf("rukovoditelj područja ne otkazuje nalog: %v", err)
	}
	dez := nije["dežurni operater"]
	if err := p.s.ConfirmSheet(ctx, dez.u, dez.perms, p.oUB, p.uslugaB, list.ID); err == nil || !strings.Contains(err.Error(), "uprava područja ili sektora") {
		t.Errorf("dežurni potvrđuje list za nadzor: %v", err)
	}
	if err := p.s.ConfirmSheet(ctx, ruk.u, ruk.perms, p.oUB, p.uslugaB, list.ID); err != nil {
		t.Errorf("rukovoditelj područja ne potvrđuje list: %v", err)
	}

	// ovlaštenik A.02 postoji: uprava više nije nadzor, ovlaštenik jest
	ovlastenici["A.02"] = true
	for ime, x := range uprava {
		if p.s.CanSupervise(x.u, x.perms, p.oUB, p.uslugaB) {
			t.Errorf("%s je nadzor iako ovlaštenik postoji", ime)
		}
	}
	if err := p.s.SetTaskStatus(ctx, ruk.u, ruk.perms, p.oUB, p.uslugaB, nalog.ID, models.TaskOpen); err == nil {
		t.Error("rukovoditelj područja otvara nalog iako ovlaštenik postoji")
	}
	if ou, op := ovlastenik(15, models.RoleContractOfficerA2); !p.s.CanSupervise(ou, op, p.oUB, p.uslugaB) {
		t.Error("ovlaštenik A.02 nije nadzor")
	}
	// ovlaštenik A.03 ne zatvara upravi dnevnik A.02
	ovlastenici = map[string]bool{"A.03": true}
	if !p.s.CanSupervise(ruk.u, ruk.perms, p.oUB, p.uslugaB) {
		t.Error("ovlaštenik drugog programa oduzima upravi nadzor")
	}

	// greška čitanja, i servis bez izvora: uprava nije nadzor
	greska = errors.New("baza zaključana")
	if p.s.CanSupervise(ruk.u, ruk.perms, p.oUB, p.uslugaB) {
		t.Error("uz grešku čitanja uprava je nadzor")
	}
	greska = nil
	p.s.SetImaOvlastenika(nil)
	if p.s.CanSupervise(ruk.u, ruk.perms, p.oUB, p.uslugaB) {
		t.Error("bez izvora ovlaštenika uprava je nadzor")
	}
	if p.s.imaOvlastenikaZa(15, nil) != true {
		t.Error("bez izvora ovlaštenik se ne pretpostavlja")
	}
}

// Novi dnevnik bez vrste pita ima li ovlaštenika bilo kojeg programa, a
// osoba bez ovlasti nije uprava ni nadzor.
func TestNadzorUslugeNoviDnevnikIBezOvlasti(t *testing.T) {
	s := &JournalService{}
	pitano := "-"
	s.SetImaOvlastenika(func(podrucje int, program string) (bool, error) {
		pitano = program
		return false, nil
	})
	if s.imaOvlastenikaZa(15, nil) || pitano != "" {
		t.Errorf("novi dnevnik: ima=%v, pitan program %q", s.imaOvlastenikaZa(15, nil), pitano)
	}
	o := models.Opseg{Sektor: "B", Podrucje: 15, Podrucja: []int{15}}
	if upravaZaNadzor(nil, o) || s.nadzorUsluge(nil, o, nil) {
		t.Error("osoba bez ovlasti je nadzor")
	}
	// kontrola: rukovoditelj područja jest
	podrucje := 15
	perms := models.NewUserPermissions(models.User{ID: uuid.New(), Duties: []models.Duty{{Role: models.RoleAreaLeader, AreaID: &podrucje, IsActive: true}}})
	if !s.nadzorUsluge(perms, o, nil) {
		t.Error("rukovoditelj područja nije nadzor u novom dnevniku bez ovlaštenika")
	}
}
