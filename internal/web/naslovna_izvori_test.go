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
	s.vodocuvar = service.NewVodocuvarService(repository.NewVodocuvarRepository(baza, rec), users, "test")
	admin := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), IsGlobalAdmin: true}}
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
	baza.Close()
	if _, _, err := s.pravaNaslovne(ctx); err == nil {
		t.Error("promjene prava bez baze bez greške")
	}
}
