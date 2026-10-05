package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Obrazac novog akta nudi samo letve po kojima bi priprema osobi prihvatila
// akt: rukovoditelju područja letvu svog područja, a ne i letvu drugog
// sektora samo zato što ima neko područje
func TestObrazacAktaNudiSamoLetveKojeSmije(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akti-letve.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo'), ('R', 'Sektor R', 'VGO Drugovo', 'COP Drugovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica'), (2, 'R', 'Mali sliv Drugovo', 'VGI Drugovo')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01'),
			('R.2.1', 2, 'R', 'potok Drugi', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users,
		service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo), "test")
	stations := service.NewStationService(stationRepo, service.NewSectionService(sectionRepo, service.NewSSEBroker()), service.NewSSEBroker())

	ctx := context.Background()
	for naziv, dionica := range map[string]string{"Primjerovo": "P.1.1", "Drugovo": "R.2.1"} {
		st := &models.Station{ID: uuid.New(), Code: strings.ToLower(naziv), Name: naziv, Watercourse: "Primjerica"}
		if err := stationRepo.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), dionica, st.ID.String(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	sektor, podrucje := "P", 1
	pero := &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", IsActive: true}
	if err := userRepo.CreateUser(pero, &models.Duty{Title: "Rukovoditelj BP 1", Role: models.RoleAreaLeader,
		ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	pero, err = userRepo.GetUserByID(pero.ID)
	if err != nil {
		t.Fatal(err)
	}
	perms := models.NewUserPermissions(*pero)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("akt_form.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAktiHandler(func() *service.AktService { return akti }, users, stations, nil, tp, nil, nil)
	r := httptest.NewRequest(http.MethodGet, "/akti/novi", nil)
	c := context.WithValue(r.Context(), contextKeyUser, pero)
	c = context.WithValue(c, contextKeyPerms, perms)
	w := httptest.NewRecorder()
	h.ShowForm(w, r.WithContext(c))
	if w.Code != http.StatusOK {
		t.Fatalf("obrazac: %d\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Primjerovo") {
		t.Error("obrazac ne nudi letvu područja rukovoditelja")
	}
	if strings.Contains(w.Body.String(), "Drugovo") {
		t.Error("obrazac nudi letvu drugog sektora, koju bi priprema odbila")
	}
}
