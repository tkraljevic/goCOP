package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// obrisiLetvu šalje brisanje letve kao obrazac s kartice letve ili kao JSON
func obrisiLetvu(t *testing.T, h *StationsHandler, perms *models.UserPermissions, id string, kaoJSON bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/stations/delete", strings.NewReader(url.Values{"station_id": {id}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if kaoJSON {
		r = httptest.NewRequest(http.MethodPost, "/api/stations/delete", strings.NewReader(`{"station_id":"`+id+`"}`))
		r.Header.Set("Content-Type", "application/json")
	}
	c := context.WithValue(r.Context(), contextKeyUser, &models.User{FullName: "Ana Probić"})
	w := httptest.NewRecorder()
	h.HandleDeleteStationAPI(w, r.WithContext(context.WithValue(c, contextKeyPerms, perms)))
	return w
}

// Brisanje letve koje više nema, npr. drugim klikom ili iz druge otvorene
// kartice, vraća na popis letvi s porukom zašto nije obrisana. Prije je
// javljalo da je obrisana, a u knjigu upisalo praznu obrisanu letvu.
func TestBrisanjeLetveKojeViseNema(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "brisanje.db"))
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
	h := NewStationsHandler(stations, nil)
	admin, operater := &models.UserPermissions{IsGlobalAdmin: true}, &models.UserPermissions{}
	id := letva.ID.String()

	// bez prava letva ostaje, a poruka se vidi na njezinoj kartici
	if w := obrisiLetvu(t, h, operater, id, false); w.Code != http.StatusSeeOther ||
		!strings.HasPrefix(w.Header().Get("Location"), "/stations/"+id+"?error=") {
		t.Errorf("brisanje bez prava: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := obrisiLetvu(t, h, operater, id, true); w.Code != http.StatusForbidden {
		t.Errorf("brisanje bez prava preko JSON-a: %d %s", w.Code, w.Body.String())
	}

	if w := obrisiLetvu(t, h, admin, id, false); w.Code != http.StatusSeeOther ||
		!strings.HasPrefix(w.Header().Get("Location"), "/stations?success=") {
		t.Fatalf("brisanje letve: %d %s", w.Code, w.Header().Get("Location"))
	}

	poruka := repository.ErrPostajaNePostoji.Error()
	naPopis := "/stations?" + url.Values{"error": {poruka}}.Encode()
	if w := obrisiLetvu(t, h, admin, id, false); w.Code != http.StatusSeeOther || w.Header().Get("Location") != naPopis {
		t.Errorf("drugo brisanje iste letve: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := obrisiLetvu(t, h, admin, id, true); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), poruka) {
		t.Errorf("drugo brisanje iste letve preko JSON-a: %d %s", w.Code, w.Body.String())
	}
	var spomenika int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE archived = 1 AND entity = ?`, repository.EntityStations).Scan(&spomenika); err != nil {
		t.Fatal(err)
	}
	if spomenika != 1 {
		t.Errorf("spomenika letve u knjizi: %d, a letva je obrisana jednom", spomenika)
	}
}
