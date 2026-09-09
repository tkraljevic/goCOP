package service_test

import (
	"context"

	"github.com/google/uuid"
	"path/filepath"
	"testing"

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
	if !db.UseRepoData() {
		t.Skip("data/ s registrima nije dostupan — registri stoje izvan repozitorija")
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
	if !db.UseRepoData() {
		t.Skip("data/ s registrima nije dostupan — registri stoje izvan repozitorija")
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
}

// letvaZaIzmjenu daje servis i jednu upisanu postaju, bez oslanjanja na
// registre iz data/ — inače bi se test preskočio ondje gdje ih nema, a
// preskočen test ne dokazuje ništa.
func letvaZaIzmjenu(t *testing.T) (*service.StationService, *repository.StationRepository, *models.Station) {
	t.Helper()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "letva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(database, "test-node")
	repo := repository.NewStationRepository(database, rec)
	svc := service.NewStationService(repo,
		service.NewSectionService(repository.NewSectionRepository(database, rec), service.NewSSEBroker()),
		service.NewSSEBroker())

	cm := func(v int) *int { return &v }
	st := &models.Station{ID: uuid.New(), Code: "batina", Name: "Batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)}}
	if err := repo.CreateStation(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return svc, repo, st
}

func ucitaj(t *testing.T, repo *repository.StationRepository, id uuid.UUID) *models.Station {
	t.Helper()
	st, err := repo.GetStationByID(context.Background(), id)
	if err != nil || st == nil {
		t.Fatalf("postaja se ne čita: %v", err)
	}
	return st
}

// Izvor kote, način, datumi i naziv iz dokumentacije dugo se nisu dali
// promijeniti: servis ih je pri svakom spremanju vraćao na staro, uz
// obrazloženje da ih obrazac ne šalje. Obrazac ih šalje, pa je posljedica bila
// da se upisano tiho baca — tko bi obrisao napomenu o koti, dobio bi je natrag
// pri sljedećem otvaranju, i tako koliko god puta pokušao.
func TestIzvorKoteSeDaPromijenitiIObrisati(t *testing.T) {
	svc, repo, st := letvaZaIzmjenu(t)
	ctx := context.Background()

	st.ZeroDatumSource = "Geodetski elaborat 250 BATINA"
	st.ZeroDatumMethod = "izmjereno na terenu"
	st.ZeroDatumSurveyDate = "2024-09-10"
	st.ZeroDatumDocumentDate = "2025-01"
	st.SourceName = "Batina (Dunav)"
	if err := svc.UpdateStation(ctx, globalAdmin(), st); err != nil {
		t.Fatal(err)
	}
	nakon := ucitaj(t, repo, st.ID)
	if nakon.ZeroDatumMethod != "izmjereno na terenu" || nakon.ZeroDatumSource != "Geodetski elaborat 250 BATINA" {
		t.Fatalf("upis nije prošao: izvor %q, način %q", nakon.ZeroDatumSource, nakon.ZeroDatumMethod)
	}

	// brisanje — ovo je bilo nemoguće koliko god puta se pokušalo
	nakon.ZeroDatumMethod = ""
	nakon.ZeroDatumSource = ""
	nakon.ZeroDatumSurveyDate = ""
	nakon.ZeroDatumDocumentDate = ""
	nakon.SourceName = ""
	if err := svc.UpdateStation(ctx, globalAdmin(), nakon); err != nil {
		t.Fatal(err)
	}
	prazno := ucitaj(t, repo, st.ID)
	for ime, v := range map[string]string{
		"način":           prazno.ZeroDatumMethod,
		"izvor":           prazno.ZeroDatumSource,
		"datum izmjere":   prazno.ZeroDatumSurveyDate,
		"datum dokumenta": prazno.ZeroDatumDocumentDate,
		"naziv iz dok.":   prazno.SourceName,
	} {
		if v != "" {
			t.Errorf("%s se vratio nakon brisanja: %q", ime, v)
		}
	}
}

// Kvačica „traži provjeru" bila je bez učinka: servis ju je računao iz pragova
// i prepisivao ono što je operater označio. Letva bez pragova i dalje traži
// pregled — to je stanje podatka, ne mišljenje — ali s pragovima odlučuje čovjek.
func TestKvacicaZaProvjeruImaUcinak(t *testing.T) {
	svc, repo, st := letvaZaIzmjenu(t)
	ctx := context.Background()

	st.NeedsReview = true
	st.ReviewNote = "kota nije potvrđena elaboratom"
	if err := svc.UpdateStation(ctx, globalAdmin(), st); err != nil {
		t.Fatal(err)
	}
	if s := ucitaj(t, repo, st.ID); !s.NeedsReview || s.ReviewNote != "kota nije potvrđena elaboratom" {
		t.Errorf("označeno za provjeru se izgubilo: %v %q", s.NeedsReview, s.ReviewNote)
	}

	s2 := ucitaj(t, repo, st.ID)
	s2.NeedsReview = false
	if err := svc.UpdateStation(ctx, globalAdmin(), s2); err != nil {
		t.Fatal(err)
	}
	if s := ucitaj(t, repo, st.ID); s.NeedsReview || s.ReviewNote != "" {
		t.Errorf("oznaka se ne da skinuti: %v %q", s.NeedsReview, s.ReviewNote)
	}

	// bez ijednog praga u centimetrima oznaka se vraća sama
	s3 := ucitaj(t, repo, st.ID)
	s3.Prep, s3.Regular = models.Threshold{}, models.Threshold{}
	s3.NeedsReview = false
	if err := svc.UpdateStation(ctx, globalAdmin(), s3); err != nil {
		t.Fatal(err)
	}
	if s := ucitaj(t, repo, st.ID); !s.NeedsReview || s.ReviewNote == "" {
		t.Error("letva bez pragova mora tražiti pregled bez obzira na kvačicu")
	}
}
