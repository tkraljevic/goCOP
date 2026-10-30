package web

import (
	"context"
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
// Primjerovo (ovdje Pero, globalni administrator), dok čvor drži njezine
// izvorne datoteke. Marko, koji letvu ne smije uređivati, gumb ne dobije, a
// ni Pero kad čvor stabla nema. Tko smije uređivati letvu po dionici,
// provjerava canEditStation.
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
	koren := t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "primjerica", "primjerovo"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := NewStationsHandler(stations, nil)
	h.SetPageTemplates(predlozakStranice(t, "station_detail.html"), nil, nil, nil, nil, nil)
	sStablom := &Server{podaciDir: koren}

	pero := &models.UserPermissions{IsGlobalAdmin: true}
	marko := &models.UserPermissions{AllowedSections: map[string]bool{"P.2.1": true}}
	for _, x := range []struct {
		opis    string
		perms   *models.UserPermissions
		uStablu func(string) bool
		gumb    bool
	}{
		{"Pero, čvor sa stablom", pero, sStablom.letvaUStablu, true},
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
		if ima := strings.Contains(w.Body.String(), `href="/administracija/uvoz-niza"`); ima != x.gumb {
			t.Errorf("%s: gumb %v", x.opis, ima)
		}
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
