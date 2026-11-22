package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Obrazac kišomjera nudi letve iz registra; čvor bez registra letvi daje
// prazan izbor.
func TestSveLetveZaObrazacKisomjera(t *testing.T) {
	r := httptest.NewRequest("GET", "/slivovi/kisomjer/novi", nil)
	if l := (&SlivoviHandler{}).sveLetve(r); l != nil {
		t.Errorf("bez registra letvi = %v", l)
	}
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "letve.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	stationRepo := repository.NewStationRepository(baza, rec)
	if err := stationRepo.CreateStation(context.Background(), &models.Station{ID: uuid.New(), Code: "lipa", Name: "Lipovac", Watercourse: "Primjerica"}); err != nil {
		t.Fatal(err)
	}
	stations := service.NewStationService(stationRepo,
		service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker()), service.NewSSEBroker())
	l := (&SlivoviHandler{stations: stations}).sveLetve(r)
	if len(l) != 1 || l[0].Code != "lipa" {
		t.Errorf("sveLetve = %+v", l)
	}
}
