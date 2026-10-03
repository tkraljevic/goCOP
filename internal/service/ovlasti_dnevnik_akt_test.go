package service_test

import (
	"errors"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Rukovoditelj dionice „upisuje očitanja i dnevnike” za svoje dionice
// (katalog uloga), a dnevnik se vodi po području, u COP-u po sektoru. Pisanje
// po dosegu ne smije mu to uzeti: piše u dnevnik područja u kojem su mu
// dionice i u dnevnik COP-a dok dežura, ali ne u dnevnik tuđeg područja.
func TestDionicarPiseDnevnikSvogPodrucja(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	dionicar := o.osoba(t, "dionicar", service.AddDutyRequest{Role: models.RoleSectionLeader, SectionCodes: "B.16.1"})
	permD := o.ovlasti(t, dionicar.ID)
	zamjenik := o.osoba(t, "zamjenik", service.AddDutyRequest{Role: models.RoleSectionDeputy, SectionCodes: "B.16.1"})
	permZ := o.ovlasti(t, zamjenik.ID)
	js := &service.JournalService{}
	p5 := models.Area{ID: 5, SectorID: "A"}
	p16 := models.Area{ID: 16, SectorID: "B"}
	p17 := models.Area{ID: 17, SectorID: "B"}
	copB := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B"}
	opsegCopB := models.OpsegDnevnika(*copB, nil, []models.Area{p16, p17})
	copA := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "A"}
	opsegCopA := models.OpsegDnevnika(*copA, nil, []models.Area{p5})

	for _, tc := range []struct {
		tko string
		p   *models.UserPermissions
	}{{"rukovoditelj dionice", permD}, {"zamjenik rukovoditelja dionice", permZ}} {
		u := &tc.p.User
		if !js.CanWrite(tc.p, models.OpsegPodrucja(p16)) || !js.CanSupervise(u, tc.p, models.OpsegPodrucja(p16)) {
			t.Errorf("%s B.16.1 ne piše (ili ne potvrđuje za nadzor) u dnevnik područja 16", tc.tko)
		}
		if js.CanWrite(tc.p, models.OpsegPodrucja(p17)) {
			t.Errorf("%s B.16.1 piše u dnevnik područja 17", tc.tko)
		}
		if !js.MozeSebeUPlan(tc.p, opsegCopB, copB) || !js.CanWrite(tc.p, opsegCopB) || len(js.AllowedKinds(u, tc.p, opsegCopB, copB)) == 0 {
			t.Errorf("%s B.16.1 smije dežurati u COP-u B, a ne piše u njegov dnevnik", tc.tko)
		}
		if js.CanWrite(tc.p, opsegCopA) {
			t.Errorf("%s B.16.1 piše u dnevnik COP-a A", tc.tko)
		}
	}
}

// Akt vodomjera obuhvaća dionice za koje je vodomjer mjerodavan, i one
// drugog područja (Vukovar: B.15.x i B.34.5). Područje akta je ono s
// najviše dionica, ali akt piše i tko piše na bilo kojoj njegovoj dionici,
// po dionici, području ili sektoru: uprava područja 16 ne smije zaostati za
// svojim rukovoditeljem dionice B.16.1.
func TestAktPiseUpravaPodrucjaSvakeDionice(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ruk16 := o.osoba(t, "ruk16", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	perm16 := o.ovlasti(t, ruk16.ID)
	ruk17 := o.osoba(t, "ruk17", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(17)})
	perm17 := o.ovlasti(t, ruk17.ID)
	ruk5 := o.osoba(t, "ruk5", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(5)})
	perm5 := o.ovlasti(t, ruk5.ID)
	dionicar := o.osoba(t, "dionicar", service.AddDutyRequest{Role: models.RoleSectionLeader, SectionCodes: "B.16.1"})
	permD := o.ovlasti(t, dionicar.ID)

	// većina dionica u području 17, jedna u 16
	a := &models.Akt{Sektor: "B", AreaID: 17, Stupanj: models.PhaseRegular,
		Dionice: []models.AktDionica{{Code: "B.16.1"}, {Code: "B.17.1"}}}
	for _, tc := range []struct {
		tko   string
		p     *models.UserPermissions
		smije bool
	}{
		{"uprava područja 16", perm16, true},
		{"uprava područja 17", perm17, true},
		{"rukovoditelj dionice B.16.1", permD, true},
		{"uprava područja 5 (sektor A)", perm5, false},
	} {
		if got := o.akti.SmijePripremiti(tc.p, &tc.p.User, a); got != tc.smije {
			t.Errorf("%s: akt s dionicama B.16.1 i B.17.1 = %v, želi %v", tc.tko, got, tc.smije)
		}
	}
	// akt bez dionice područja 16 uprava 16 ne piše
	samo17 := &models.Akt{Sektor: "B", AreaID: 17, Stupanj: models.PhaseRegular, Dionice: []models.AktDionica{{Code: "B.17.1"}}}
	if o.akti.SmijePripremiti(perm16, &perm16.User, samo17) || o.akti.SmijePripremiti(permD, &permD.User, samo17) {
		t.Error("uprava područja 16 ili rukovoditelj dionice B.16.1 piše akt samo s dionicom B.17.1")
	}
}

// Terenska uloga (vodočuvar, strojar, rukovatelj, rukovoditelj dionice)
// piše na dionicama ili, bez njih, u cijelom području. Upisana samo uz
// sektor ne bi davala nikakvo pravo, a dužnost bi stajala kao valjana; zato
// se ne prima.
func TestTerenskaDuznostTraziPodrucjeIliDionice(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	u := o.osoba(t, "strojar", service.AddDutyRequest{})
	for _, d := range []service.AddDutyRequest{
		{UserID: u.ID, Role: models.RoleMachinist, SectorID: sektorP("B")},
		{UserID: u.ID, Role: models.RoleSectionLeader, SectorID: sektorP("B")},
		{UserID: u.ID, Role: models.RoleWaterGuard},
	} {
		if err := o.users.AddDuty(o.admin, d); !errors.Is(err, service.ErrInvalidUserData) {
			t.Errorf("%s bez područja i dionica: %v", d.Role, err)
		}
	}
	if err := o.users.AddDuty(o.admin, service.AddDutyRequest{UserID: u.ID, Role: models.RoleMachinist, SectorID: sektorP("B"), AreaID: podrucjeP(16)}); err != nil {
		t.Errorf("strojar područja 16: %v", err)
	}
	p := o.ovlasti(t, u.ID)
	if !p.AllowedAreas[16] {
		t.Errorf("strojar područja 16 ne piše u području: %v", p.AllowedAreas)
	}
	// ni izmjenom dužnost ne ostaje samo uz sektor
	svjez, _ := o.repo.GetUserByID(u.ID)
	if err := o.users.UpdateDuty(o.admin, svjez.Duties[0].ID, service.AddDutyRequest{UserID: u.ID, Role: models.RoleMachinist, SectorID: sektorP("B")}); !errors.Is(err, service.ErrInvalidUserData) {
		t.Errorf("izmjena strojara na samo sektor: %v", err)
	}
}
