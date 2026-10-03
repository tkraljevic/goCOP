package service_test

import (
	"context"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Dužnost na dionici piše na objektima uz koje dionica stoji. Kad bi
// rukovoditelj dionice B.16.1 smio uz svoju dionicu vezati bilo koji objekt
// ili vodomjer, vezom bi sebi dao pisanje po tuđem području i sektoru:
// uređivanje i brisanje objekta iz registra, očitanja i akte vodomjera.
// Novu vezu na zapis drugog područja zato upisuje samo tko ondje piše.
func TestDionicaNeVezeTudjiObjektNiVodomjer(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	ctx := context.Background()
	strRepo := repository.NewStructureRepository(o.db, o.rec)
	objekti := service.NewStructureService(strRepo)
	sekcije := service.NewSectionService(repository.NewSectionRepository(o.db, o.rec), service.NewSSEBroker())
	rs := service.NewReadingService(nil, nil, nil, sekcije, o.users)
	postaje := repository.NewStationRepository(o.db, o.rec)

	novi := func(naziv, sektor string, podrucje int) *models.Structure {
		t.Helper()
		st := &models.Structure{Name: naziv, Kind: models.StructureKindPumpingStation, SectorID: sektor, AreaID: podrucje}
		if err := objekti.Create(ctx, o.admin, st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	cs16 := novi("CS Šesnaest", "B", 16)
	cs17 := novi("CS Sedamnaest", "B", 17)
	csA := novi("CS Sektora A", "A", 5)
	letva := func(naziv string) *models.Station {
		t.Helper()
		st := &models.Station{Code: naziv, Name: naziv}
		if err := postaje.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	letva17 := letva("Letva Sedamnaest")
	slobodna := letva("Letva Bez Dionica")

	// letva 17 mjerodavna je za dionicu B.17.1 (upisuje globalni administrator)
	d17, err := sekcije.GetSectionWithDetails("B.17.1")
	if err != nil {
		t.Fatal(err)
	}
	d17.Parts = []models.SectionPart{{Seq: 1, Description: "poddionica 17", StationIDs: []string{letva17.ID.String()}}}
	if err := sekcije.SaveSection(ctx, o.admin, d17, false); err != nil {
		t.Fatal(err)
	}

	ruk16 := o.osoba(t, "ruk16", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(16)})
	perm16 := o.ovlasti(t, ruk16.ID)
	rukB := o.osoba(t, "rukb", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	permB := o.ovlasti(t, rukB.ID)
	dionicar := o.osoba(t, "dionicar", service.AddDutyRequest{Role: models.RoleSectionLeader, SectionCodes: "B.16.1"})
	permD := o.ovlasti(t, dionicar.ID)
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, SectionCodes: "B.16.1"})
	permV := o.ovlasti(t, vod.ID)

	spremi := func(p *models.UserPermissions, dio models.SectionPart) error {
		t.Helper()
		sec, err := sekcije.GetSectionWithDetails("B.16.1")
		if err != nil {
			t.Fatal(err)
		}
		dio.Seq = 1
		if dio.Description == "" {
			dio.Description = "poddionica 16"
		}
		sec.Parts = []models.SectionPart{dio}
		return sekcije.SaveSection(ctx, p, sec, false)
	}
	objekt := func(st *models.Structure) models.SectionPart {
		return models.SectionPart{Objects: []models.PartObject{{StructureID: st.ID.String(), Name: st.Name}}}
	}
	vodomjer := func(st *models.Station) models.SectionPart {
		return models.SectionPart{StationIDs: []string{st.ID.String()}}
	}

	for _, tc := range []struct {
		tko string
		p   *models.UserPermissions
	}{{"rukovoditelj dionice B.16.1", permD}, {"vodočuvar dionice B.16.1", permV}, {"uprava područja 16", perm16}} {
		if err := spremi(tc.p, objekt(cs17)); err == nil {
			t.Errorf("%s vezao je uz B.16.1 objekt područja 17", tc.tko)
		}
		if err := spremi(tc.p, objekt(csA)); err == nil {
			t.Errorf("%s vezao je uz B.16.1 objekt sektora A", tc.tko)
		}
		if err := spremi(tc.p, vodomjer(letva17)); err == nil {
			t.Errorf("%s vezao je uz B.16.1 vodomjer dionica područja 17", tc.tko)
		}
		// vodomjer i iz zapisa vodomjera dionice, po nazivu
		if err := spremi(tc.p, models.SectionPart{Gauges: []models.GaugeItem{{StationName: letva17.Name}}}); err == nil {
			t.Errorf("%s vezao je uz B.16.1 vodomjer područja 17 kroz zapis vodomjera", tc.tko)
		}
	}
	for _, st := range []*models.Structure{cs17, csA} {
		svjez, _ := objekti.Get(ctx, st.ID)
		if len(svjez.SectionCodes) != 0 {
			t.Errorf("%s: odbijeno spremanje ipak je upisalo vezu %v", st.Name, svjez.SectionCodes)
		}
		for _, p := range []*models.UserPermissions{permD, permV} {
			if objekti.CanEdit(p, svjez) || rs.CanRecordStructure(p, svjez) {
				t.Errorf("%s: dužnost na B.16.1 piše po tuđem objektu", st.Name)
			}
		}
	}

	// svoje područje, slobodan vodomjer i ono što je već bilo uz dionicu: da
	if err := spremi(permD, objekt(cs16)); err != nil {
		t.Errorf("rukovoditelj dionice ne veže objekt svog područja: %v", err)
	}
	if err := spremi(permD, vodomjer(slobodna)); err != nil {
		t.Errorf("rukovoditelj dionice ne veže vodomjer bez dionica: %v", err)
	}
	// rukovoditelj sektora B piše i u području 17, globalni administrator svuda
	if err := spremi(permB, models.SectionPart{StationIDs: []string{letva17.ID.String()}, Objects: []models.PartObject{{StructureID: cs17.ID.String(), Name: cs17.Name}}}); err != nil {
		t.Errorf("rukovoditelj sektora B ne veže objekt i vodomjer područja 17: %v", err)
	}
	if err := spremi(o.admin, models.SectionPart{StationIDs: []string{letva17.ID.String()},
		Objects: []models.PartObject{{StructureID: cs17.ID.String(), Name: cs17.Name}, {StructureID: csA.ID.String(), Name: csA.Name}}}); err != nil {
		t.Fatalf("globalni administrator ne veže objekt sektora A: %v", err)
	}
	// postojeća veza ne priječi spremanje dionice
	if err := spremi(permD, models.SectionPart{Description: "opis rukovoditelja dionice", StationIDs: []string{letva17.ID.String()},
		Objects: []models.PartObject{{StructureID: cs16.ID.String(), Name: cs16.Name}, {StructureID: cs17.ID.String(), Name: cs17.Name},
			{StructureID: csA.ID.String(), Name: csA.Name}}}); err != nil {
		t.Errorf("rukovoditelj dionice ne sprema dionicu s vezama koje je upisao administrator: %v", err)
	}
	// ...ali objekt drugog područja uz dionicu ne daje pisanje po njemu:
	// vodi ga njegovo područje (CS Budžak područja 16 stoji i na B.34.1)
	svjezA, _ := objekti.Get(ctx, csA.ID)
	svjez17, _ := objekti.Get(ctx, cs17.ID)
	svjez16, _ := objekti.Get(ctx, cs16.ID)
	if len(svjezA.SectionCodes) == 0 {
		t.Fatal("veza koju je upisao globalni administrator nije upisana")
	}
	for _, st := range []*models.Structure{svjezA, svjez17} {
		if objekti.CanEdit(permD, st) || rs.CanRecordStructure(permD, st) {
			t.Errorf("%s uz dionicu B.16.1: rukovoditelj dionice piše po objektu drugog područja", st.Name)
		}
	}
	if err := objekti.Delete(ctx, permD, csA.ID); err == nil {
		t.Error("rukovoditelj dionice B.16.1 obrisao je objekt sektora A")
	}
	if !objekti.CanEdit(permD, svjez16) || !rs.CanRecordStructure(permD, svjez16) {
		t.Error("rukovoditelj dionice ne piše po objektu svog područja na svojoj dionici")
	}
}
