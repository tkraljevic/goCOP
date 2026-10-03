package service_test

import (
	"context"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Pravo pisanja ide po dosegu dužnosti. Dužnost područja ili dionice nosi i
// sektor (normalizeScope ga upiše iz područja), ali on joj ne daje pisanje
// po cijelom sektoru: rukovoditelj dionice B.16.1 piše na svojoj dionici,
// njezinim objektima i aktima, a ne u području 17; uprava područja 16 piše
// u području 16, ne u 17; rukovoditelj sektora B piše u cijelom sektoru.
func TestPisanjePoDosegu(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ctx := context.Background()
	ruk16 := o.osoba(t, "ruk16", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	perm16 := o.ovlasti(t, ruk16.ID)
	rukB := o.osoba(t, "rukb", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	permB := o.ovlasti(t, rukB.ID)
	// rukovoditelja dionice i vodočuvara dodjeljuje uprava područja 16
	dionicar := o.osoba(t, "dionicar", service.AddDutyRequest{Role: models.RoleMachinist, AreaID: podrucjeP(16)})
	if err := o.users.AddDuty(perm16, service.AddDutyRequest{UserID: dionicar.ID, Role: models.RoleSectionLeader, SectionCodes: "B.16.1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`UPDATE duties SET is_active = 0 WHERE user_id = ? AND role = ?`, dionicar.ID.String(), string(models.RoleMachinist)); err != nil {
		t.Fatal(err)
	}
	permD := o.ovlasti(t, dionicar.ID)
	if permD.AllowedSectors["B"] || permD.AllowedAreas[16] || !permD.AllowedSections["B.16.1"] {
		t.Fatalf("ovlasti rukovoditelja dionice: sektori %v područja %v dionice %v", permD.AllowedSectors, permD.AllowedAreas, permD.AllowedSections)
	}
	if perm16.AllowedSectors["B"] || !perm16.AllowedAreas[16] {
		t.Fatalf("ovlasti uprave područja 16: sektori %v područja %v", perm16.AllowedSectors, perm16.AllowedAreas)
	}
	// prikaz i izbor i dalje znaju u kojem sektoru i području osoba radi
	if !permD.RadiUSektoru("B") || !permD.RadiUPodrucju(16) || permD.RadiUPodrucju(17) || !perm16.RadiUSektoru("B") {
		t.Error("sektor i područje dužnosti nisu ostali za prikaz")
	}

	sekcije := service.NewSectionService(repository.NewSectionRepository(o.db, o.rec), service.NewSSEBroker())
	dionica := func(code string) *models.Section {
		t.Helper()
		sec, err := sekcije.GetSectionWithDetails(code)
		if err != nil || sec == nil {
			t.Fatalf("dionica %s: %v", code, err)
		}
		return sec
	}
	rs := service.NewReadingService(nil, nil, nil, sekcije, o.users)
	akt := func(area int, dionice ...string) *models.Akt {
		a := &models.Akt{Sektor: "B", AreaID: area, Stupanj: models.PhaseRegular}
		for _, c := range dionice {
			a.Dionice = append(a.Dionice, models.AktDionica{Code: c})
		}
		return a
	}
	objekt := func(area int, dionice ...string) *models.Structure {
		return &models.Structure{SectorID: "B", AreaID: area, SectionCodes: dionice}
	}
	journals := &service.JournalService{}
	objekti := service.NewStructureService(nil)
	podrucja := []models.Area{{ID: 16, SectorID: "B"}, {ID: 17, SectorID: "B"}}

	tests := []struct {
		tko   string
		perms *models.UserPermissions
		u     *models.User
		// 16: piše u svom; 17: piše u tuđem području istog sektora
		u16, u17 bool
	}{
		{"rukovoditelj dionice B.16.1", permD, &permD.User, true, false},
		{"uprava područja 16", perm16, &perm16.User, true, false},
		{"rukovoditelj sektora B", permB, &permB.User, true, true},
	}
	for _, tc := range tests {
		p := tc.perms
		if got := sekcije.CanEditSection(p, dionica("B.16.1")); got != tc.u16 {
			t.Errorf("%s: dionica B.16.1 = %v, želi %v", tc.tko, got, tc.u16)
		}
		if got := sekcije.CanEditSection(p, dionica("B.17.1")); got != tc.u17 {
			t.Errorf("%s: dionica B.17.1 = %v, želi %v", tc.tko, got, tc.u17)
		}
		if got := rs.CanRecordStructure(p, objekt(16, "B.16.1")); got != tc.u16 {
			t.Errorf("%s: objekt na dionici B.16.1 = %v, želi %v", tc.tko, got, tc.u16)
		}
		if got := rs.CanRecordStructure(p, objekt(17)); got != tc.u17 {
			t.Errorf("%s: objekt u području 17 = %v, želi %v", tc.tko, got, tc.u17)
		}
		if got := rs.CanRecordStructure(p, objekt(17, "B.17.1")); got != tc.u17 {
			t.Errorf("%s: objekt na dionici B.17.1 = %v, želi %v", tc.tko, got, tc.u17)
		}
		if got := objekti.CanEdit(p, objekt(16, "B.16.1")); got != tc.u16 {
			t.Errorf("%s: registar, objekt na dionici B.16.1 = %v, želi %v", tc.tko, got, tc.u16)
		}
		if got := objekti.CanEdit(p, objekt(17, "B.17.1")); got != tc.u17 {
			t.Errorf("%s: registar, objekt na dionici B.17.1 = %v, želi %v", tc.tko, got, tc.u17)
		}
		if got := o.akti.SmijePripremiti(p, tc.u, akt(16, "B.16.1")); got != tc.u16 {
			t.Errorf("%s: akt područja 16 = %v, želi %v", tc.tko, got, tc.u16)
		}
		if got := o.akti.SmijePripremiti(p, tc.u, akt(17, "B.17.1")); got != tc.u17 {
			t.Errorf("%s: akt područja 17 = %v, želi %v", tc.tko, got, tc.u17)
		}
		if got := p.HasWriteAccess("B", 17, ""); got != tc.u17 {
			t.Errorf("%s: HasWriteAccess(B, 17) = %v, želi %v", tc.tko, got, tc.u17)
		}
	}

	// nove dionice: uprava područja samo u svom području
	if !sekcije.CanCreateSectionInArea(perm16, "B", 16) || sekcije.CanCreateSectionInArea(perm16, "B", 17) {
		t.Error("uprava područja 16 otvara dionice izvan svog područja (ili ne u svom)")
	}
	if !sekcije.CanCreateSectionInArea(permB, "B", 17) {
		t.Error("rukovoditelj sektora B ne otvara dionice u području 17")
	}
	tudja := dionica("B.17.1")
	tudja.Description = "prepisao rukovoditelj dionice B.16.1"
	tudja.DescriptionCustom = true
	tudja.Parts = []models.SectionPart{{Seq: 1, Description: "poddionica koju je upisao tuđi rukovoditelj"}}
	if err := sekcije.SaveSection(ctx, permD, tudja, false); err == nil {
		t.Error("rukovoditelj dionice B.16.1 spremio je dionicu B.17.1")
	}
	svoja := dionica("B.16.1")
	svoja.Description = "opis rukovoditelja dionice"
	svoja.DescriptionCustom = true
	svoja.Parts = []models.SectionPart{{Seq: 1, Description: "poddionica"}}
	if err := sekcije.SaveSection(ctx, permD, svoja, false); err != nil {
		t.Errorf("rukovoditelj dionice B.16.1 ne sprema svoju dionicu: %v", err)
	}

	// dnevnici područja: uprava područja u svom, sektor u svima
	if !journals.CanWrite(perm16, models.OpsegPodrucja(podrucja[0])) || journals.CanWrite(perm16, models.OpsegPodrucja(podrucja[1])) {
		t.Error("uprava područja 16 piše u dnevnik područja 17 (ili ne u svoj)")
	}
	if !journals.CanWrite(permB, models.OpsegPodrucja(podrucja[1])) {
		t.Error("rukovoditelj sektora B ne piše u dnevnik područja 17")
	}

	// vodočuvar dionice B.16.1: objekt bez dionice u svom području da,
	// u tuđem ne
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, SectionCodes: "B.16.1"})
	permV := o.ovlasti(t, vod.ID)
	if !rs.CanRecordStructure(permV, objekt(16)) || rs.CanRecordStructure(permV, objekt(17)) {
		t.Error("vodočuvar dionice B.16.1: objekt bez dionice u području 16 da, u području 17 ne")
	}
}
