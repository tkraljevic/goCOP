package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Prestanak obrane ne upisuje se unaprijed, kao ni proglašenje: obrana s
// upisanim krajem više nije otvorena, pa bi prestanak „za sutra” obranu
// zatvorio odmah. Podaci su izmišljeni: sektor P, dionica P.1.1, letva
// Primjerovo, a odluke donosi Pero Perić (pperic).
func TestPrestanakObraneNeUnaprijed(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "obrana.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	epizode := repository.NewEpisodeRepository(baza, rec)
	svc := NewEpisodeService(epizode, repository.NewReadingRepository(baza, rec), stanice)
	letva := models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo"}
	if err := stanice.CreateStation(ctx, &letva); err != nil {
		t.Fatal(err)
	}
	pero := &models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}}
	pocetak := time.Now().Add(-2 * time.Hour)
	if _, err := svc.Declare(ctx, pero, "pperic", "P.1.1", letva, pocetak, models.PhaseRegular, models.BasisOrder, ""); err != nil {
		t.Fatal(err)
	}

	sutra := time.Now().Add(24 * time.Hour)
	if err := svc.End(ctx, pero, "pperic", "P.1.1", sutra, "voda pada"); err == nil || !strings.Contains(err.Error(), "unaprijed") {
		t.Errorf("prestanak za sutra: %v", err)
	}
	if e, err := epizode.OpenEpisode(ctx, "P.1.1"); err != nil || e == nil {
		t.Fatalf("obrana nije otvorena nakon odbijenog prestanka: %v %v", e, err)
	}
	// do sat unaprijed vrijedi, kao kod proglašenja (razlika satova)
	if err := svc.End(ctx, pero, "pperic", "P.1.1", time.Now().Add(30*time.Minute), "voda pada"); err != nil {
		t.Errorf("prestanak pola sata unaprijed: %v", err)
	}
	if e, _ := epizode.OpenEpisode(ctx, "P.1.1"); e != nil {
		t.Errorf("obrana je i dalje otvorena: %+v", e)
	}
}

// Ovjera akta bez letve (ili s letvom koja nije UUID, npr. pristigao
// razmjenom) ne pada: akt je već spremljen kao ovjeren, pa bi pad ostavio
// ovjeren akt bez promjene stanja obrane.
func TestOvjeraAktaBezLetve(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01'),
			('P.1.2', 1, 'P', 'kanal Probni', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	epizode := repository.NewEpisodeRepository(baza, rec)
	obrana := NewEpisodeService(epizode, repository.NewReadingRepository(baza, rec), stanice)
	svc := NewAktService(repository.NewAktiRepository(baza, rec), stanice, nil, nil, nil, nil, obrana, "cvor-probni")
	pero := &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić"}
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *pero}

	for kod, letva := range map[string]string{"P.1.1": "", "P.1.2": "nije-uuid"} {
		a := &models.Akt{ID: uuid.NewString(), Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhaseRegular,
			Vrijedi: time.Now().Add(-time.Minute), StationID: letva, StationName: "Primjerovo", Dionice: []models.AktDionica{{Code: kod}}}
		ovjeren, _, err := svc.zakljuciOvjeru(ctx, uprava, pero, a, time.Now())
		if err != nil || ovjeren == nil || ovjeren.Status != models.AktOvjeren {
			t.Fatalf("ovjera akta s letvom %q: %+v %v", letva, ovjeren, err)
		}
		if e, err := epizode.OpenEpisode(ctx, kod); err != nil || e == nil || e.Phase != models.PhaseRegular {
			t.Errorf("obrana na %s nakon ovjere akta s letvom %q: %+v %v", kod, letva, e, err)
		}
	}
}
