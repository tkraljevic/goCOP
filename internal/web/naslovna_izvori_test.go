package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/dhmz"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Izvori Naslovne nad pravom bazom: vodostaj dan ranije, promjene prava,
// a izvori kojih poslužitelj još nema daju „nije dostupno”
func TestIzvoriNaslovne(t *testing.T) {
	ctx := context.Background()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "izvori.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	readings := service.NewReadingService(readingRepo, stationRepo, repository.NewStructureRepository(baza, rec),
		service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker()), users)
	st := &models.Station{ID: uuid.New(), Code: "proba", Name: "Proba"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	sad := time.Now().UTC().Truncate(time.Hour)
	for _, o := range []struct {
		prije time.Duration
		cm    int
	}{{0, 312}, {25 * time.Hour, 300}, {60 * time.Hour, 250}} {
		cm := o.cm
		if err := readingRepo.Create(ctx, &models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: sad.Add(-o.prije), LevelCm: &cm, Source: models.ReadingSourceManual}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{readingService: readings, userService: users, db: baza, recorder: rec}
	g := models.GaugeSummary{StationID: st.ID.String(), Latest: &models.Reading{MeasuredAt: sad}}
	if p := s.vodostajPrijeDan(ctx, g); p == nil || *p != 300 {
		t.Errorf("vodostaj dan ranije: %v", p)
	}
	g.Latest.MeasuredAt = sad.Add(-24 * time.Hour)
	if p := s.vodostajPrijeDan(ctx, g); p != nil {
		t.Errorf("prestaro očitanje za promjenu u 24 h: %v", *p)
	}
	if s.vodostajPrijeDan(ctx, models.GaugeSummary{}) != nil {
		t.Error("letva bez očitanja ima vodostaj dan ranije")
	}
	if n, nacin, err := s.pravaNaslovne(ctx); err != nil || n != 0 || nacin == "" {
		t.Errorf("promjene prava: %d %q %v", n, nacin, err)
	}

	if _, ima := s.razmjenaNaslovne(ctx); ima {
		t.Error("razmjena bez čvora")
	}
	s.razmjena = func(context.Context) RazmjenaStanje { return RazmjenaStanje{UMrezi: true} }
	if r, ima := s.razmjenaNaslovne(ctx); !ima || !r.UMrezi {
		t.Errorf("razmjena: %+v %v", r, ima)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if p := s.vrijemeNaslovne(r); p.Mjesto != "" {
		t.Errorf("vrijeme bez ploče: %+v", p)
	}
	nema := httptest.NewServer(http.NotFoundHandler())
	defer nema.Close()
	s.vrijemeH = &VrijemeHandler{dhmz: &dhmz.Klijent{Adrese: map[string]string{dhmz.AdresaVrijeme: nema.URL, dhmz.AdresaUpozorenjaDanas: nema.URL,
		dhmz.AdresaUpozorenjaSutra: nema.URL, dhmz.AdresaRegije: nema.URL, dhmz.AdresaBilten: nema.URL}}}
	if p := s.vrijemeNaslovne(r); p.Mjesto == "" || len(p.Greske) == 0 {
		t.Errorf("vrijeme bez DHMZ-a: %+v", p)
	}
	if l, err := s.nepredaniUSektoru(ctx, nil, "B"); l != nil || err != nil {
		t.Errorf("listovi bez dnevnika: %v %v", l, err)
	}
	if _, err := s.listoviNaslovne(ctx, nil, repository.FiltarListova{}); err == nil {
		t.Error("listovi bez dnevnika bez greške")
	}
	if _, _, err := s.zadaciPodrucjaNaslovne(ctx, nil, 16, sad); err == nil {
		t.Error("zadaci bez dnevnika bez greške")
	}
	if _, err := s.prijaveNaslovne(ctx, nil, repository.FiltarPrijava{}); err == nil {
		t.Error("prijave bez dnevnika bez greške")
	}
	if _, err := s.cekajuParafuNaslovne(ctx, nil, 16); err == nil {
		t.Error("parafe bez dnevnika bez greške")
	}
	if _, err := s.dnevniciUslugaNaslovne(ctx, 16); err == nil {
		t.Error("dnevnici usluga bez dnevnika bez greške")
	}
	if _, err := s.listoviDnevnikaNaslovne(ctx, "dn"); err == nil {
		t.Error("listovi dnevnika bez dnevnika bez greške")
	}
	if _, err := s.aktiNaPotpisNaslovne(ctx, nil, "B"); err == nil {
		t.Error("akti bez servisa akata bez greške")
	}
	s.akti = service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, repository.NewSectionRepository(baza, rec),
		repository.NewTerritoryRepository(baza, rec), readingRepo, users, service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo), "test")
	if a, err := s.aktiNaPotpisNaslovne(ctx, &models.UserPermissions{IsGlobalAdmin: true}, "B"); len(a) != 0 || err != nil {
		t.Errorf("akti na potpis praznog sektora: %v %v", a, err)
	}
	s.vodocuvar = service.NewVodocuvarService(repository.NewVodocuvarRepository(baza, rec), users, "test")
	s.prijave = service.NewPrijavaService(repository.NewPrijavaRepository(baza, rec), users, s.vodocuvar, "test")
	admin := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), IsGlobalAdmin: true}}
	if l, err := s.listoviNaslovne(ctx, admin, repository.FiltarListova{AreaID: 16}); len(l) != 0 || err != nil {
		t.Errorf("listovi praznog područja: %v %v", l, err)
	}
	if z, _, err := s.zadaciPodrucjaNaslovne(ctx, admin, 16, sad); len(z) != 0 || err != nil {
		t.Errorf("zadaci praznog područja: %v %v", z, err)
	}
	if p, err := s.prijaveNaslovne(ctx, admin, repository.FiltarPrijava{AreaID: 16}); len(p) != 0 || err != nil {
		t.Errorf("prijave praznog područja: %v %v", p, err)
	}
	if l, err := s.cekajuParafuNaslovne(ctx, admin, 16); len(l) != 0 || err != nil {
		t.Errorf("parafe praznog područja: %v %v", l, err)
	}
	if l, err := s.nepredaniUSektoru(ctx, admin, "B"); len(l) != 0 || err != nil {
		t.Errorf("listovi praznog sektora: %v %v", l, err)
	}
	i := s.izvoriNaslovne()
	if i.Letve == nil || i.Djelatnici == nil {
		t.Fatal("izvori bez letava ili djelatnika")
	}
	if _, err := i.Letve(ctx); err != nil {
		t.Errorf("pregled letava: %v", err)
	}
	if _, err := i.Djelatnici(); err != nil {
		t.Errorf("djelatnici: %v", err)
	}
	if _, err := i.Osobe("B", 16, models.RoleAreaLeader); err != nil {
		t.Errorf("osobe s ulogom: %v", err)
	}
	if _, _, err := i.StanjaSektora(ctx, "B"); err == nil {
		t.Error("stanja sektora bez akata bez greške")
	}
	if _, err := i.Teren(ctx, admin, &admin.User); err != nil {
		t.Errorf("terenski pogled: %v", err)
	}
	s.journalService = service.NewJournalService(repository.NewJournalRepository(baza, rec), nil, nil)
	if _, err := i.Podrucja("B"); err != nil {
		t.Errorf("područja: %v", err)
	}
	if _, err := i.Dezurstva(ctx, "nema"); err != nil {
		t.Errorf("plan dežurstava: %v", err)
	}
	if _, err := i.Zapisi(ctx, "nema"); err != nil {
		t.Errorf("upisi dnevnika: %v", err)
	}
	if d, err := s.dnevniciUslugaNaslovne(ctx, 16); len(d) != 0 || err != nil {
		t.Errorf("dnevnici usluga: %v %v", d, err)
	}
	if l, err := s.listoviDnevnikaNaslovne(ctx, "nema"); len(l) != 0 || err != nil {
		t.Errorf("listovi dnevnika: %v %v", l, err)
	}
	sredstva := func() []error {
		_, e1 := s.skladistaNaslovne(ctx, "B", 16)
		_, e2 := s.stanjeSkladistaNaslovne(ctx, "sk")
		_, e3 := s.sredstvaNaTerenuNaslovne(ctx, "B")
		_, e4 := s.prometSredstavaNaslovne(ctx, repository.FiltarPrometa{Limit: 5})
		_, e5 := s.popisiSredstavaNaslovne(ctx, "B", "sk", 2026)
		_, e6 := s.vrsteSredstavaNaslovne(ctx)
		return []error{e1, e2, e3, e4, e5, e6}
	}
	for i, err := range sredstva() {
		if err == nil {
			t.Errorf("sredstva %d bez servisa bez greške", i)
		}
	}
	s.mtsService = service.NewMtsService(repository.NewMtsRepository(baza, rec), repository.NewSectionRepository(baza, rec), userRepo)
	for i, err := range sredstva() {
		if err != nil {
			t.Errorf("sredstva %d: %v", i, err)
		}
	}
	baza.Close()
	if _, _, err := s.pravaNaslovne(ctx); err == nil {
		t.Error("promjene prava bez baze bez greške")
	}
}
