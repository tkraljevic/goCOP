package service

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Očitanje iznad praga na zidu događanja (Odluke (8. 10. 2026.), pitanje
// 8): letva s dionicama daje sektor preko dionice, a letva bez dionica
// sektore koji je prate, iz upisanog praćenja. Dosad je očitanje letve bez
// dionica bilo bez sektora, pa ga je vidio svatko. Izmišljeni sektori P
// (područje 1) i Q (područje 3).
func TestOcitanjeIznadPragaNaZiduSektoraKojiPratiLetvu(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zid-ocitanja.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	stations := repository.NewStationRepository(baza, rec)
	ctx := context.Background()
	letva := func(naziv string, sektori []string, podrucja []int) *models.Station {
		st := &models.Station{ID: uuid.New(), Code: strings.ToLower(naziv), Name: naziv, PrateSektori: sektori, PratePodrucja: podrucja}
		st.Prep, st.Regular, st.Emergency, st.State = pragCm(300), pragCm(500), pragCm(650), pragCm(800)
		if err := stations.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	primjerovo := letva("Primjerovo", []string{"Q"}, nil) // na dionici P.1.1; praćenje ovdje ne mijenja sektor
	mohacs := letva("Mohacs", []string{"Q"}, nil)
	barcs := letva("Barcs", []string{"Q"}, []int{1}) // prate ga Q i područje 1, dakle i P
	uzvodna := letva("Uzvodna", nil, nil)
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO', 'COP'), ('Q', 'Sektor Q', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI'), (3, 'Q', 'Mali sliv Drugdje', 'VGI')`,
		`INSERT INTO sections (code, area_id, sector_id, description, parts, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica',
			'[{"seq":1,"station_ids":["` + primjerovo.ID.String() + `"]}]', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	readings := repository.NewReadingRepository(baza, rec)
	kad := time.Now().Add(-time.Hour)
	for _, st := range []*models.Station{primjerovo, mohacs, barcs, uzvodna} {
		v := 520 // redovna obrana
		if err := readings.Create(ctx, &models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: kad, LevelCm: &v,
			Source: models.ReadingSourceManual}); err != nil {
			t.Fatal(err)
		}
	}
	nisko := 100
	if err := readings.Create(ctx, &models.Reading{ID: uuid.New(), StationID: mohacs.ID.String(), MeasuredAt: kad.Add(-time.Hour),
		LevelCm: &nisko, Source: models.ReadingSourceManual}); err != nil {
		t.Fatal(err)
	}

	zid := NewZidService(rec, nil, repository.NewSectionRepository(baza, rec), nil, repository.NewUserRepository(baza, rec), stations, nil)
	vidi := func(perms *models.UserPermissions, f FiltarZida) string {
		t.Helper()
		f.Modul, f.Limit = ModulOcitanja, 50
		dog, _, err := zid.Zadnji(ctx, perms, f)
		if err != nil {
			t.Fatal(err)
		}
		var letve []string
		for _, d := range dog {
			letve = append(letve, strings.SplitN(d.Tekst, ":", 2)[0]+"/"+d.Sektor)
		}
		sort.Strings(letve)
		return strings.Join(letve, ",")
	}
	sektorP := &models.UserPermissions{AllowedSectors: map[string]bool{"P": true}}
	sektorQ := &models.UserPermissions{AllowedSectors: map[string]bool{"Q": true}}
	for _, s := range []struct {
		ime   string
		perms *models.UserPermissions
		f     FiltarZida
		vidi  string
	}{
		// očitanje ispod praga ne ide na zid; Uzvodnu nitko ne prati, pa je
		// vide svi. Barcs prate P i Q: redak nosi prvi (P), a vide ga oba.
		{"razina 1", &models.UserPermissions{IsGlobalAdmin: true}, FiltarZida{}, "Barcs/P,Mohacs/Q,Primjerovo/P,Uzvodna/"},
		{"sektor P", sektorP, FiltarZida{}, "Barcs/P,Primjerovo/P,Uzvodna/"},
		{"sektor Q", sektorQ, FiltarZida{}, "Barcs/P,Mohacs/Q,Uzvodna/"},
		// filtar po sektoru propušta i letvu koju taj sektor prati uz drugi
		{"razina 1, filtar sektora P", &models.UserPermissions{IsGlobalAdmin: true}, FiltarZida{Sektor: "P"}, "Barcs/P,Primjerovo/P,Uzvodna/"},
		{"razina 1, filtar sektora Q", &models.UserPermissions{IsGlobalAdmin: true}, FiltarZida{Sektor: "Q"}, "Barcs/P,Mohacs/Q,Uzvodna/"},
	} {
		if got := vidi(s.perms, s.f); got != s.vidi {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.vidi)
		}
	}
}

func TestDogadjajPripadaSektoru(t *testing.T) {
	d := Dogadjaj{Sektor: "Q", josSektora: "E2,P"}
	if !d.pripadaSektoru("") || !d.pripadaSektoru("Q") || !d.pripadaSektoru("P") || d.pripadaSektoru("E") || d.pripadaSektoru("E2,P") {
		t.Error("događaj sektora Q koji pripada i sektoru P")
	}
	if !(Dogadjaj{}).pripadaSektoru("E") {
		t.Error("događaj bez sektora pripada svakom")
	}
	// bez registra dionica letva ide po praćenju
	c := &opisivac{s: &ZidService{}, podrucja: map[int]string{1: "P"}}
	var dog Dogadjaj
	c.sektorLetve(&dog, &models.Station{PratePodrucja: []int{1}})
	if dog.Sektor != "P" || dog.josSektora != "" || dog.ostaliSektori() != nil {
		t.Errorf("letva bez dionica: %+v", dog)
	}
}
