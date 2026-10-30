package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/javnivodostaji"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Kartica letve: gumb „Unos u arhivu” dobije onaj tko smije uređivati letvu
// Primjerovo, dok čvor drži njezine izvorne datoteke: Pero, globalni
// administrator, i Ana, koja nije administrator, ali radi na dionici P.1.1
// uz koju letva stoji. Marko, koji radi na drugoj dionici, gumb ne dobije, a
// ni Pero kad čvor stabla nema. Gumb nosi šifru letve, a kartica letve vodi
// s unosa natrag (karticaLetve).
func TestKarticaLetveUnosUArhivuPoPravuIStablu(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "kartica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	stationRepo := repository.NewStationRepository(baza, rec)
	stations := service.NewStationService(stationRepo,
		service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker()), service.NewSSEBroker())
	letva := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stationRepo.CreateStation(context.Background(), letva); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'prva', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('veza-1', 'P.1.1', '` + letva.ID.String() + `', '2026-01-01 00:00:00')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	koren := t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "primjerica", "primjerovo"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := NewStationsHandler(stations, nil)
	h.SetPageTemplates(predlozakStranice(t, "station_detail.html"), nil, nil, nil, nil, nil)
	sStablom := &Server{podaciDir: koren}

	pero := &models.UserPermissions{IsGlobalAdmin: true}
	ana := &models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}}
	marko := &models.UserPermissions{AllowedSections: map[string]bool{"P.2.1": true}}
	for _, x := range []struct {
		opis    string
		perms   *models.UserPermissions
		uStablu func(string) bool
		gumb    bool
	}{
		{"Pero, čvor sa stablom", pero, sStablom.letvaUStablu, true},
		{"Ana s dionice letve, čvor sa stablom", ana, sStablom.letvaUStablu, true},
		{"Marko, čvor sa stablom", marko, sStablom.letvaUStablu, false},
		{"Pero, čvor bez stabla", pero, (&Server{}).letvaUStablu, false},
		{"Pero, provjera nije predana", pero, nil, false},
	} {
		h.SetUnosUArhivu(x.uStablu)
		r := httptest.NewRequest(http.MethodGet, "/stations/"+letva.ID.String(), nil)
		r.SetPathValue("id", letva.ID.String())
		r = zahtjevSKorisnikom(r, &models.User{FullName: "Pero Perić"}, x.perms)
		w := httptest.NewRecorder()
		h.ShowStation(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", x.opis, w.Code, w.Body.String())
		}
		if ima := strings.Contains(w.Body.String(), `href="/administracija/uvoz-niza?letva=primjerovo"`); ima != x.gumb {
			t.Errorf("%s: gumb %v", x.opis, ima)
		}
	}

	s := &Server{stationService: stations}
	if kartica, naziv := s.karticaLetve(context.Background(), "primjerovo"); kartica != "/stations/"+letva.ID.String() || naziv != "Primjerovo" {
		t.Errorf("kartica letve: %q %q", kartica, naziv)
	}
	if kartica, naziv := s.karticaLetve(context.Background(), "nema-je"); kartica != "" || naziv != "" {
		t.Errorf("letva koje nema: %q %q", kartica, naziv)
	}
	if kartica, _ := (&Server{}).karticaLetve(context.Background(), "primjerovo"); kartica != "" {
		t.Errorf("bez registra letava: %q", kartica)
	}
}

// Postavke prognoze: na čvoru s krugom preuzimanja gumb „Generiraj” dobiju
// dežurni i administrator, a vodočuvar ne.
func TestZaglavljePrognozeSmijeGenerirati(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	uvoznik := javnivodostaji.NoviUvoznik(repository.NewJavniSpremiste(baza, repository.NewReadingRepository(baza, ledger.New(baza, "test"))), nil)
	h := &PrognozeHandler{javni: func() *javnivodostaji.Uvoznik { return uvoznik }}
	dezurni := &models.UserPermissions{User: models.User{Duties: []models.Duty{{Role: models.RoleOperator, IsActive: true}}}}
	vodocuvar := &models.UserPermissions{User: models.User{Duties: []models.Duty{{Role: models.RoleWaterGuard, IsActive: true}}}}
	for opis, x := range map[string]struct {
		perms *models.UserPermissions
		smije bool
	}{
		"dežurni":                {dezurni, true},
		"globalni administrator": {&models.UserPermissions{IsGlobalAdmin: true}, true},
		"vodočuvar":              {vodocuvar, false},
	} {
		r := zahtjevSKorisnikom(httptest.NewRequest(http.MethodGet, "/prognoze/postavke", nil), &models.User{FullName: "Ivo Ivić"}, x.perms)
		if d := h.zaglavlje(r); !d.MozeGenerirati || d.SmijeGenerirati != x.smije {
			t.Errorf("%s: može %v, smije %v", opis, d.MozeGenerirati, d.SmijeGenerirati)
		}
	}
}

// Novi račun ili nova lozinka s prekratkom lozinkom: osoba čita jednu
// rečenicu bez tehničkog uvoda, a ostale greške računa ostaju kakve jesu.
func TestPorukaRacunaZaKratkuLozinku(t *testing.T) {
	kratka := fmt.Errorf("%w: %w", service.ErrInvalidUserData, service.ErrKratkaLozinka)
	if got := porukaRacuna(kratka); got != "Lozinka je prekratka: upišite najmanje 10 znakova." {
		t.Errorf("prekratka lozinka: %q", got)
	}
	if got := porukaRacuna(service.ErrUsernameExists); got != service.ErrUsernameExists.Error() {
		t.Errorf("druga greška: %q", got)
	}
}
