package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Dodjela dužnosti rukovoditelja obrane izvođača i zamjenika (Odluke
// (22. 11. 2026.), pitanja 4 i 5). Firme i osobe su izmišljene: Meandar
// Proba radi na području 5, Nasip Primjer samo na području 6, a Stara
// Gradnja više ne radi.

type okolinaDodjeleIzvodjaca struct {
	*okolinaOporavka
	users    *service.UserService
	org      *repository.OrgRepository
	firme    map[string]*models.Contractor
	sektorA  *models.UserPermissions // rukovoditelj sektora A
	upravaBP *models.UserPermissions // rukovoditelj područja 5
}

func novaOkolinaDodjeleIzvodjaca(t *testing.T) *okolinaDodjeleIzvodjaca {
	t.Helper()
	o, users := okolinaSektora(t)
	org := repository.NewOrgRepository(o.db, o.rec)
	users.SetFirme(org.ListContractors)
	ok := &okolinaDodjeleIzvodjaca{okolinaOporavka: o, users: users, org: org, firme: map[string]*models.Contractor{}}
	firma := func(naziv, kratko string, aktivna bool, podrucja ...int) {
		c := &models.Contractor{Name: naziv, ShortName: kratko, Active: aktivna}
		var veze []models.ContractorAssignment
		for _, p := range podrucja {
			veze = append(veze, models.ContractorAssignment{SectorID: "A", AreaID: p})
		}
		if err := org.SaveContractor(context.Background(), c, veze); err != nil {
			t.Fatal(err)
		}
		ok.firme[kratko] = c
	}
	firma("Meandar Proba d.o.o.", "Meandar Proba", true, 5)
	firma("Nasip Primjer d.o.o.", "Nasip Primjer", true, 6)
	firma("Stara Gradnja d.d.", "Stara Gradnja", false, 5)
	ok.sektorA = ok.uprava(t, "ruksa", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektor("A")})
	ok.upravaBP = ok.uprava(t, "rukbp", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeBr(5)})
	return ok
}

// uprava otvara račun s dužnošću i vraća ovlasti
func (o *okolinaDodjeleIzvodjaca) uprava(t *testing.T, ime string, req service.AddDutyRequest) *models.UserPermissions {
	t.Helper()
	u := o.racun(t, ime, "lozinka-"+ime, ime+"@voda.hr", false, true)
	req.UserID = u.ID
	if err := o.users.AddDuty(o.admin, req); err != nil {
		t.Fatal(err)
	}
	p, err := o.auth.PermissionsFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// izFirme otvara račun s vrstom organizacije i firmom
func (o *okolinaDodjeleIzvodjaca) izFirme(t *testing.T, ime string, vrsta models.OrgType, firma string) *models.User {
	t.Helper()
	u := o.racun(t, ime, "lozinka-"+ime, ime+"@primjer.hr", false, true)
	if _, err := o.db.Exec(`UPDATE users SET org_type = ?, org_name = ? WHERE id = ?`, string(vrsta), firma, u.ID.String()); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestDodjelaObraneIzvodjacaTraziFirmuPodrucja(t *testing.T) {
	o := novaOkolinaDodjeleIzvodjaca(t)
	for _, uloga := range []models.Role{models.RoleContractorAreaLeader, models.RoleContractorAreaDeputy} {
		t.Run(string(uloga), func(t *testing.T) {
			p := "rl"
			if uloga == models.RoleContractorAreaDeputy {
				p = "rz"
			}
			odbijena := []struct {
				ime    string
				racun  *models.User
				poruka string
			}{
				{"račun Hrvatskih voda", o.izFirme(t, p+"hv", models.OrgHrvatskeVode, "Meandar Proba d.o.o."), "Ugovorni izvođač (pravna osoba)"},
				{"pravna osoba bez firme", o.izFirme(t, p+"bez", models.OrgPravnaOsoba, " "), "nije upisana firma"},
				{"firma koje nema u registru", o.izFirme(t, p+"nema", models.OrgPravnaOsoba, "Nepostojeća d.o.o."), "firma Nepostojeća d.o.o. s računa nije u registru"},
				{"neaktivna firma", o.izFirme(t, p+"stara", models.OrgPravnaOsoba, "STARA GRADNJA DD, Primjerska 1"), "firma Stara Gradnja nije aktivna"},
				{"firma nije vezana na područje", o.izFirme(t, p+"nasip", models.OrgPravnaOsoba, "Nasip Primjer"), "firma Nasip Primjer nije vezana na BP 5"},
			}
			for _, s := range odbijena {
				err := o.users.AddDuty(o.admin, service.AddDutyRequest{UserID: s.racun.ID, Role: uloga, AreaID: podrucjeBr(5)})
				if !errors.Is(err, service.ErrInvalidUserData) || !strings.Contains(err.Error(), s.poruka) {
					t.Errorf("%s: %v", s.ime, err)
				}
			}

			// firma vezana na područje 5: daje globalni administrator i uprava sektora
			prvi := o.izFirme(t, p+"mea1", models.OrgPravnaOsoba, "Meandar Proba d.o.o., Primjerica")
			if err := o.users.AddDuty(o.admin, service.AddDutyRequest{UserID: prvi.ID, Role: uloga, AreaID: podrucjeBr(5)}); err != nil {
				t.Errorf("globalni administrator: %v", err)
			}
			drugi := o.izFirme(t, p+"mea2", models.OrgPravnaOsoba, "Meandar Proba")
			if err := o.users.AddDuty(o.admin, service.AddDutyRequest{UserID: drugi.ID, Role: models.RoleServiceLeaderForeman, AreaID: podrucjeBr(5)}); err != nil {
				t.Fatal(err)
			}
			if err := o.users.AddDuty(o.sektorA, service.AddDutyRequest{UserID: drugi.ID, Role: uloga, AreaID: podrucjeBr(5)}); err != nil {
				t.Errorf("uprava sektora: %v", err)
			}
			// uprava područja je ne daje, ni u svom području
			if err := o.users.AddDuty(o.upravaBP, service.AddDutyRequest{UserID: drugi.ID, Role: uloga, AreaID: podrucjeBr(5)}); !errors.Is(err, service.ErrUnauthorized) {
				t.Errorf("uprava područja dala dužnost: %v", err)
			}
			if service.SmijeDatiUlogu(o.upravaBP, uloga) || !service.SmijeDatiUlogu(o.sektorA, uloga) {
				t.Error("obrazac nudi ulogu upravi područja ili je ne nudi upravi sektora")
			}

			// novi račun: ista pravila
			novi := func(ime string, vrsta models.OrgType, firma string) service.CreateUserRequest {
				return service.CreateUserRequest{Username: ime, Password: "lozinka-" + ime, FullName: ime, OrgType: vrsta, OrgName: firma,
					Role: uloga, AreaID: podrucjeBr(5)}
			}
			if _, err := o.users.CreateUser(o.sektorA, novi(p+"novihv", models.OrgHrvatskeVode, "")); !errors.Is(err, service.ErrInvalidUserData) {
				t.Errorf("novi račun Hrvatskih voda: %v", err)
			}
			if _, err := o.users.CreateUser(o.sektorA, novi(p+"novimea", models.OrgPravnaOsoba, "Meandar Proba d.o.o.")); err != nil {
				t.Errorf("novi račun iz firme područja: %v", err)
			}
		})
	}
}

// Izmjena dužnosti u ulogu obrane izvođača ili na drugo područje je nova
// dodjela i provjerava firmu; izmjena iste dužnosti (razlog, rok) ne
func TestIzmjenaObraneIzvodjacaProvjeravaFirmu(t *testing.T) {
	o := novaOkolinaDodjeleIzvodjaca(t)
	ana := o.izFirme(t, "aproba", models.OrgPravnaOsoba, "Meandar Proba d.o.o.")
	if err := o.users.AddDuty(o.admin, service.AddDutyRequest{UserID: ana.ID, Role: models.RoleServiceLeaderForeman, AreaID: podrucjeBr(6)}); err != nil {
		t.Fatal(err)
	}
	duznost := func() models.Duty {
		t.Helper()
		p, err := o.auth.PermissionsFor(ana.ID)
		if err != nil || len(p.User.Duties) != 1 {
			t.Fatalf("dužnosti: %v", err)
		}
		return p.User.Duties[0]
	}
	d := duznost()
	// voditelj usluga na području 6 u rukovoditelja obrane na području 6: firma nije ondje
	err := o.users.UpdateDuty(o.admin, d.ID, service.AddDutyRequest{Role: models.RoleContractorAreaLeader, AreaID: podrucjeBr(6)})
	if !errors.Is(err, service.ErrInvalidUserData) || !strings.Contains(err.Error(), "nije vezana na BP 6") {
		t.Errorf("izmjena na području 6: %v", err)
	}
	if err := o.users.UpdateDuty(o.admin, d.ID, service.AddDutyRequest{Role: models.RoleContractorAreaLeader, AreaID: podrucjeBr(5)}); err != nil {
		t.Fatalf("izmjena na području 5: %v", err)
	}
	// firma poslije prestane raditi: ista dužnost se i dalje uređuje
	c := o.firme["Meandar Proba"]
	c.Active = false
	if err := o.org.SaveContractor(context.Background(), c, c.Assignments); err != nil {
		t.Fatal(err)
	}
	d = duznost()
	if err := o.users.UpdateDuty(o.admin, d.ID, service.AddDutyRequest{Role: d.Role, AreaID: d.AreaID, Reason: "Prilog 2"}); err != nil {
		t.Errorf("izmjena iste dužnosti: %v", err)
	}
	if err := o.users.UpdateDuty(o.admin, d.ID, service.AddDutyRequest{Role: models.RoleContractorAreaDeputy, AreaID: d.AreaID}); !errors.Is(err, service.ErrInvalidUserData) {
		t.Errorf("nova uloga uz neaktivnu firmu: %v", err)
	}
}

// Bez registra firmi ili kad se ne da pročitati dužnost se ne daje, jer
// se ne vidi je li firma vezana na područje
func TestDodjelaObraneIzvodjacaBezRegistra(t *testing.T) {
	o, users := okolinaSektora(t)
	u := o.racun(t, "mproba", "lozinka-mproba", "mproba@primjer.hr", false, true)
	if _, err := o.db.Exec(`UPDATE users SET org_type = ?, org_name = 'Meandar Proba' WHERE id = ?`, string(models.OrgPravnaOsoba), u.ID.String()); err != nil {
		t.Fatal(err)
	}
	req := service.AddDutyRequest{UserID: u.ID, Role: models.RoleContractorAreaLeader, AreaID: podrucjeBr(5)}
	if err := users.AddDuty(o.admin, req); !errors.Is(err, service.ErrInvalidUserData) || !strings.Contains(err.Error(), "registar licenciranih firmi") {
		t.Errorf("bez registra: %v", err)
	}
	users.SetFirme(func(context.Context) ([]models.Contractor, error) { return nil, errors.New("baza zaključana") })
	if err := users.AddDuty(o.admin, req); err == nil || !strings.Contains(err.Error(), "nije pročitan") {
		t.Errorf("registar se ne da pročitati: %v", err)
	}
	// ostale uloge registar ne trebaju
	if err := users.AddDuty(o.admin, service.AddDutyRequest{UserID: u.ID, Role: models.RoleServiceLeaderForeman, AreaID: podrucjeBr(5)}); err != nil {
		t.Errorf("voditelj usluga: %v", err)
	}
}
