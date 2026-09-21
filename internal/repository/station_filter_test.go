package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

func TestStationCountryFilterAndListCountries(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop_filter.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()

	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}

	repo := NewStationRepository(baza, ledger.New(baza, "test-node"))
	ctx := context.Background()

	testStations := []models.Station{
		{Code: "batina", Name: "Batina", Watercourse: "Dunav"},
		{Code: "zupanja", Name: "Županja", Watercourse: "Sava"},
		{Code: "bezdan", Name: "Bezdan (Srbija)", Watercourse: "Dunav"},
		{Code: "apatin", Name: "Apatin (Srbija)", Watercourse: "Dunav"},
		{Code: "budapest", Name: "Budapest (Mađarska)", Watercourse: "Dunav"},
		{Code: "baja", Name: "Baja (Mađarska)", Watercourse: "Dunav"},
		{Code: "bratislava", Name: "Bratislava (Slovačka)", Watercourse: "Dunav"},
		{Code: "gornja-radgona", Name: "Gornja Radgona (Slovenija)", Watercourse: "Mura"},
	}

	for i := range testStations {
		if err := repo.CreateStation(ctx, &testStations[i]); err != nil {
			t.Fatalf("CreateStation %s: %v", testStations[i].Code, err)
		}
	}

	// 1. ListCountries
	countries, err := repo.ListCountries(ctx)
	if err != nil {
		t.Fatalf("ListCountries: %v", err)
	}

	expectedCountries := []string{"Hrvatska", "Mađarska", "Slovačka", "Slovenija", "Srbija"}
	if len(countries) != len(expectedCountries) {
		t.Fatalf("ListCountries len = %d, want %d (%v)", len(countries), len(expectedCountries), countries)
	}
	for i, exp := range expectedCountries {
		if countries[i] != exp {
			t.Errorf("countries[%d] = %q, want %q", i, countries[i], exp)
		}
	}

	// 2. ListStations bez filtra
	all, err := repo.ListStations(ctx, "", "", "", false)
	if err != nil {
		t.Fatalf("ListStations all: %v", err)
	}
	if len(all) != len(testStations) {
		t.Fatalf("ListStations all len = %d, want %d", len(all), len(testStations))
	}

	// 3. ListStations za Hrvatsku
	hr, err := repo.ListStations(ctx, "", "", "Hrvatska", false)
	if err != nil {
		t.Fatalf("ListStations Hrvatska: %v", err)
	}
	if len(hr) != 2 {
		t.Fatalf("ListStations Hrvatska len = %d, want 2 (Batina, Županja)", len(hr))
	}
	for _, s := range hr {
		if s.Zemlja() != "Hrvatska" {
			t.Errorf("postaja %s ima zemlju %s, a očekivana je Hrvatska", s.Name, s.Zemlja())
		}
	}

	// 4. ListStations za Srbiju
	rs, err := repo.ListStations(ctx, "", "", "Srbija", false)
	if err != nil {
		t.Fatalf("ListStations Srbija: %v", err)
	}
	if len(rs) != 2 {
		t.Fatalf("ListStations Srbija len = %d, want 2 (Bezdan, Apatin)", len(rs))
	}

	// 5. Kombinirani filtar: vodotok Dunav + država Mađarska
	huDunav, err := repo.ListStations(ctx, "", "Dunav", "Mađarska", false)
	if err != nil {
		t.Fatalf("ListStations Dunav+Mađarska: %v", err)
	}
	if len(huDunav) != 2 {
		t.Fatalf("ListStations Dunav+Mađarska len = %d, want 2 (Budapest, Baja)", len(huDunav))
	}

	// 6. Case-insensitivity u filtru
	huLower, err := repo.ListStations(ctx, "", "", "mađarska", false)
	if err != nil {
		t.Fatalf("ListStations mađarska lowercase: %v", err)
	}
	if len(huLower) != 2 {
		t.Fatalf("ListStations mađarska len = %d, want 2", len(huLower))
	}
}
