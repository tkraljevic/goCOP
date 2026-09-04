package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func setupStationTestServices(t *testing.T) (*service.StationService, *repository.StationRepository) {
	t.Helper()

	database, err := db.OpenDB(filepath.Join(t.TempDir(), "test_stations_gocop.db"))
	if err != nil {
		t.Fatalf("baza: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := db.InitSchema(database); err != nil {
		t.Fatalf("shema: %v", err)
	}
	if err := db.SeedInitialData(database); err != nil {
		t.Fatalf("seed: %v", err)
	}

	sse := service.NewSSEBroker()
	sectionService := service.NewSectionService(repository.NewSectionRepository(database, ledger.New(database, "test-node")), sse)
	stationRepo := repository.NewStationRepository(database, ledger.New(database, "test-node"))
	return service.NewStationService(stationRepo, sectionService, sse), stationRepo
}

func globalAdmin() *models.UserPermissions {
	return &models.UserPermissions{IsGlobalAdmin: true}
}

func findStation(t *testing.T, repo *repository.StationRepository, name string) *models.Station {
	t.Helper()
	stations, err := repo.ListStations(context.Background(), name, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range stations {
		if stations[i].Name == name {
			return &stations[i]
		}
	}
	t.Fatalf("postaja %q nije u registru", name)
	return nil
}

// Obrazac za izmjenu ne šalje sva polja. Kvar u kojem je UPDATE brisao šifru
// postaje pronađen je slučajno — odavde ga čuva test.
func TestIzmjenaPostajeCuvaOnoStoObrazacNeSalje(t *testing.T) {
	svc, repo := setupStationTestServices(t)
	ctx := context.Background()

	before := findStation(t, repo, "Županja")
	if before.Code == "" || before.Watercourse == "" || len(before.SectionCodes) == 0 {
		t.Fatalf("Županja iz seeda nema šifru/vodu/dionice: %+v", before)
	}

	// Obrazac: samo ono što korisnik vidi — bez šifre, bez podrijetla vode
	prep := 610
	edited := models.Station{
		ID:          before.ID,
		Name:        before.Name,
		Stationing:  before.Stationing,
		Watercourse: before.Watercourse,
		ZeroDatum:   before.ZeroDatum,
		Prep:        models.Threshold{Cm: &prep, Raw: "+610"},
		Regular:     before.Regular,
		Emergency:   before.Emergency,
		State:       before.State,
		Record:      before.Record,
		Notes:       "provjera izmjene",
	}
	if err := svc.UpdateStation(ctx, globalAdmin(), &edited); err != nil {
		t.Fatalf("izmjena: %v", err)
	}

	after := findStation(t, repo, "Županja")
	if after.Code != before.Code {
		t.Errorf("šifra izgubljena: %q → %q", before.Code, after.Code)
	}
	if after.Watercourse != before.Watercourse || after.WatercourseSource != before.WatercourseSource {
		t.Errorf("voda ili podrijetlo promijenjeni: (%q, %q) → (%q, %q)",
			before.Watercourse, before.WatercourseSource, after.Watercourse, after.WatercourseSource)
	}
	if after.WatercourseCode != before.WatercourseCode {
		t.Errorf("veza na registar voda izgubljena: %q → %q", before.WatercourseCode, after.WatercourseCode)
	}
	if len(after.SectionCodes) != len(before.SectionCodes) {
		t.Errorf("veze s dionicama: %d → %d", len(before.SectionCodes), len(after.SectionCodes))
	}
	if after.Prep.Cm == nil || *after.Prep.Cm != 610 {
		t.Errorf("prag P nije promijenjen na 610: %v", after.Prep.Cm)
	}
	if after.Notes != "provjera izmjene" {
		t.Errorf("napomena nije spremljena: %q", after.Notes)
	}
}

// Ručna promjena naziva vode raskida vezu na registar i bilježi se kao
// potvrda operatera — inače bi postaja pisala jednu vodu, a pokazivala na drugu
func TestRucnaPromjenaVodeRaskidaVezuNaRegistar(t *testing.T) {
	svc, repo := setupStationTestServices(t)
	ctx := context.Background()

	before := findStation(t, repo, "Županja")
	edited := *before
	edited.Watercourse = "Bosut"

	if err := svc.UpdateStation(ctx, globalAdmin(), &edited); err != nil {
		t.Fatalf("izmjena: %v", err)
	}

	after := findStation(t, repo, "Županja")
	if after.Watercourse != "Bosut" {
		t.Errorf("voda nije promijenjena: %q", after.Watercourse)
	}
	if after.WatercourseSource != models.WatercourseFromOperator {
		t.Errorf("podrijetlo mora biti OPERATER, dobiveno %q", after.WatercourseSource)
	}
	if after.WatercourseCode != "" {
		t.Errorf("veza na registar mora pasti kad se naziv ručno promijeni, ostala %q", after.WatercourseCode)
	}
}

// Pragovi moraju rasti od pripremnog stanja prema izvanrednom
func TestIzmjenaOdbijaKriviRedoslijedPragova(t *testing.T) {
	svc, repo := setupStationTestServices(t)

	before := findStation(t, repo, "Županja")
	p, r := 900, 500
	edited := *before
	edited.Prep = models.Threshold{Cm: &p}
	edited.Regular = models.Threshold{Cm: &r}

	if err := svc.UpdateStation(context.Background(), globalAdmin(), &edited); err == nil {
		t.Error("izmjena s R < P morala je biti odbijena")
	}
}

// Uklanjanje s dionice ne briše postaju iz registra
func TestUklanjanjeSDioniceNeBriseIzRegistra(t *testing.T) {
	svc, repo := setupStationTestServices(t)
	ctx := context.Background()

	before := findStation(t, repo, "Županja")
	if len(before.SectionCodes) < 2 {
		t.Fatalf("Županja treba više dionica za ovaj test, ima %d", len(before.SectionCodes))
	}
	removed := before.SectionCodes[0]

	if err := svc.RemoveSectionStation(ctx, globalAdmin(), removed, before.ID); err != nil {
		t.Fatalf("uklanjanje: %v", err)
	}

	after := findStation(t, repo, "Županja")
	if len(after.SectionCodes) != len(before.SectionCodes)-1 {
		t.Errorf("veze: %d → %d, očekivano jedna manje", len(before.SectionCodes), len(after.SectionCodes))
	}
	for _, c := range after.SectionCodes {
		if c == removed {
			t.Errorf("dionica %s nije uklonjena", removed)
		}
	}

	// ponovno dodavanje ne duplicira
	if err := svc.AddSectionStations(ctx, globalAdmin(), removed, []uuid.UUID{before.ID}); err != nil {
		t.Fatalf("dodavanje: %v", err)
	}
	if err := svc.AddSectionStations(ctx, globalAdmin(), removed, []uuid.UUID{before.ID}); err != nil {
		t.Fatalf("ponovno dodavanje: %v", err)
	}
	final := findStation(t, repo, "Županja")
	if len(final.SectionCodes) != len(before.SectionCodes) {
		t.Errorf("nakon vraćanja veze: %d, očekivano %d", len(final.SectionCodes), len(before.SectionCodes))
	}
}

// Svaki upis kroz servis mora ostaviti verziju u knjizi — to je temelj
// sinkronizacije, pa nije stvar dobre volje repozitorija
func TestSvakiUpisOstavljaVerziju(t *testing.T) {
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "verzije.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedInitialData(database); err != nil {
		t.Fatal(err)
	}

	rec := ledger.New(database, "test-node")
	sse := service.NewSSEBroker()
	sectionService := service.NewSectionService(repository.NewSectionRepository(database, rec), sse)
	stationRepo := repository.NewStationRepository(database, rec)
	svc := service.NewStationService(stationRepo, sectionService, sse)
	ctx := context.Background()

	st := findStation(t, stationRepo, "Županja")
	edited := *st
	edited.Notes = "prva izmjena"
	if err := svc.UpdateStation(ctx, globalAdmin(), &edited); err != nil {
		t.Fatal(err)
	}
	edited.Notes = "druga izmjena"
	if err := svc.UpdateStation(ctx, globalAdmin(), &edited); err != nil {
		t.Fatal(err)
	}

	history, err := rec.History(ctx, repository.EntityStations, st.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("dvije izmjene moraju dati dvije verzije, dobiveno %d", len(history))
	}
	if history[0].NodeID != "test-node" || history[0].Supersedes != history[1].VersionID {
		t.Errorf("verzije nisu ulančane kako treba: %+v", history[0])
	}

	// uklanjanje s dionice ostavlja arhiviranu verziju veze
	removed := st.SectionCodes[0]
	if err := svc.RemoveSectionStation(ctx, globalAdmin(), removed, st.ID); err != nil {
		t.Fatal(err)
	}
	link, err := rec.Latest(ctx, repository.EntitySectionStations, removed+"|"+st.ID.String())
	if err != nil {
		t.Fatalf("veza nema verziju nakon uklanjanja: %v", err)
	}
	if !link.Archived {
		t.Error("uklonjena veza mora biti arhivirana, ne obrisana")
	}
}
