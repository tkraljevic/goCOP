package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

func TestIznimkeRacunaKrozServis(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "moduli.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	ctx := context.Background()
	s := service.NewModuleService(repository.NewModuleRepository(baza, ledger.New(baza, "ured")))
	if err := s.SetUserOverride(ctx, &models.UserPermissions{IsGlobalAdmin: true}, "racun-1", []string{models.ModuleAdmin}, nil); err != nil {
		t.Fatal(err)
	}
	if l, err := s.IznimkeRacuna(ctx); err != nil || len(l) != 1 || l[0].UserID != "racun-1" {
		t.Errorf("%+v %v", l, err)
	}
}
