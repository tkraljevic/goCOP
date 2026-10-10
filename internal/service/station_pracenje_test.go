package service_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// letveSektoraP otvara servis letvi nad izmišljenim sektorom P (uz sektor E)
// i vraća i bazu
func letveSektoraP(t *testing.T) (*service.StationService, *repository.StationRepository, *sql.DB) {
	t.Helper()
	db.KoristiIzmisljenePodatke(t)
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "pracenje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	sse := service.NewSSEBroker()
	repo := repository.NewStationRepository(baza, rec)
	return service.NewStationService(repo, service.NewSectionService(repository.NewSectionRepository(baza, rec), sse), sse), repo, baza
}

// Sektori i područja koji letvu prate (Odluke (8. 10. 2026.), pitanje 8)
// upisuju se s istim pravom kao i ostali podaci letve; upisuje se samo ono
// što je u registru, a ono što je letva već imala ne provjerava se ponovo.
func TestPracenjeLetveUObrascu(t *testing.T) {
	svc, repo, baza := letveSektoraP(t)
	ctx := context.Background()

	nova := &models.Station{Name: "Mohács probni", PrateSektori: []string{"P", " P", ""}, PratePodrucja: []int{2, 2}}
	if err := svc.CreateStation(ctx, globalAdmin(), nova, ""); err != nil {
		t.Fatalf("nova letva s praćenjem: %v", err)
	}
	got := findStation(t, repo, "Mohács probni")
	if !reflect.DeepEqual(got.PrateSektori, []string{"P"}) || !reflect.DeepEqual(got.PratePodrucja, []int{2}) {
		t.Errorf("uređeno praćenje nove letve: %v %v", got.PrateSektori, got.PratePodrucja)
	}
	odbijeno := func(ime string, err error, poruka string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), poruka) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", ime, poruka, err)
		}
	}
	odbijeno("nova letva s nepoznatim sektorom",
		svc.CreateStation(ctx, globalAdmin(), &models.Station{Name: "Barcs probni", PrateSektori: []string{"Z"}}, ""),
		"sektor Z nije u registru")

	izmjena := *got
	izmjena.PratePodrucja = append(izmjena.PratePodrucja, 99)
	odbijeno("izmjena s nepoznatim područjem", svc.UpdateStation(ctx, globalAdmin(), &izmjena), "branjeno područje 99 nije u registru")

	// sektor koji je u međuvremenu nestao iz registra ne zaustavlja izmjenu
	// letve: provjerava se samo novo
	if _, err := baza.Exec(`UPDATE stations SET prate_sektori = 'P,X' WHERE id = ?`, got.ID.String()); err != nil {
		t.Fatal(err)
	}
	sada := findStation(t, repo, "Mohács probni")
	sada.Notes = "Izmijenila Ana Anić"
	sada.PrateSektori = append(sada.PrateSektori, "E")
	if err := svc.UpdateStation(ctx, globalAdmin(), sada); err != nil {
		t.Fatalf("izmjena letve sa zatečenim nepoznatim sektorom: %v", err)
	}
	if got := findStation(t, repo, "Mohács probni"); !reflect.DeepEqual(got.PrateSektori, []string{"E", "P", "X"}) || got.Notes != "Izmijenila Ana Anić" {
		t.Errorf("nakon izmjene: %v %q", got.PrateSektori, got.Notes)
	}

	// isto pravo kao za izmjenu letve: bez ovlasti nad letvom praćenje se ne mijenja
	sada = findStation(t, repo, "Mohács probni")
	sada.PrateSektori = nil
	odbijeno("bez ovlasti", svc.UpdateStation(ctx, &models.UserPermissions{AllowedSectors: map[string]bool{"P": true}}, sada), "nemate ovlasti")

	// podaci letve provjeravaju se prije praćenja, a registar koji se ne
	// može pročitati zaustavlja upis
	odbijeno("nova letva bez naziva", svc.CreateStation(ctx, globalAdmin(), &models.Station{PrateSektori: []string{"P"}}, ""), "naziv")
	baza.Close()
	odbijeno("registar se ne čita", svc.CreateStation(ctx, globalAdmin(), &models.Station{Name: "Barcs probni", PrateSektori: []string{"P"}}, ""),
		"greška pri provjeri registra")
}
