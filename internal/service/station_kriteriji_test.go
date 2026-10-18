package service_test

import (
	"context"
	"database/sql"
	"errors"
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
)

// Vodomjeri i kriteriji obrane na dionici, upis postaje uz dionicu i
// brisanje postaje iz registra. Podaci su izmišljeni: sektor P s
// područjima 1 i 2, dionice P.1.1 i P.1.2.

type okolinaPostaja struct {
	baza *sql.DB
	svc  *service.StationService
	repo *repository.StationRepository
}

// dijeloviP11 su poddionice dionice P.1.1 s retcima vodomjera i kriterija
// kako stoje u dokumentaciji dionice
const dijeloviP11 = `[
	{"seq": 1, "description": "rijeka Primjerica, lijeva obala", "gauges": [
		{"station_name": "Primjerovo, rkm 12,5 (80,45)", "prep_cm": "+300"},
		{"station_name": "Pravilnik o radu akumulacije Probna"}
	]},
	{"seq": 2, "description": "kanal Probni", "gauges": [
		{"station_name": "Sava - Primjerovo"},
		{"station_name": "Vodokaz kod mosta km 3+200"},
		{"station_name": "rkm 14,0"},
		{"station_name": "Ustava Probna"}
	]}
]`

func novaOkolinaPostaja(t *testing.T) *okolinaPostaja {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "postaje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica'), (2, 'P', 'Mali sliv Probni', 'VGI Probni')`,
		`INSERT INTO sections (code, area_id, sector_id, description, parts, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '` + dijeloviP11 + `', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.2', 1, 'P', 'kanal Probni', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	sse := service.NewSSEBroker()
	repo := repository.NewStationRepository(baza, rec)
	o := &okolinaPostaja{baza: baza, repo: repo,
		svc: service.NewStationService(repo, service.NewSectionService(repository.NewSectionRepository(baza, rec), sse), sse)}
	o.letva(t, "Primjerovo", "", "P.1.1")
	o.letva(t, "Probni most", "  VODOKAZ kod mosta km 3+200 ", "P.1.1")
	o.letva(t, "Ustava Probna", "", "P.1.2")
	return o
}

// letva upisuje postaju u registar i veže je uz dionice
func (o *okolinaPostaja) letva(t *testing.T, ime, izvorno string, dionice ...string) *models.Station {
	t.Helper()
	st := &models.Station{ID: uuid.New(), Code: strings.ToLower(strings.ReplaceAll(ime, " ", "-")), Name: ime, SourceName: izvorno}
	if err := o.repo.CreateStation(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	for _, d := range dionice {
		if _, err := o.baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), d, st.ID.String(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func (o *okolinaPostaja) pokvari(t *testing.T, tablica string) {
	t.Helper()
	if _, err := o.baza.Exec(`ALTER TABLE ` + tablica + ` RENAME TO ` + tablica + `_nema`); err != nil {
		t.Fatal(err)
	}
}

// Kriteriji obrane dionice su retci dokumentacije koji nisu njezini
// mjerodavni vodomjeri. Redak je vodomjer kad se naziv slaže s postajom
// dionice bez obzira na stacionažu, kotu nule i ime rijeke ispred naziva,
// ili kad je izvorni zapis postaje isti redak. Postaja druge dionice ne
// skida redak, a redak koji je samo stacionaža ostaje kriterij. Nijedan
// redak se ne gubi; greške čitanja se vraćaju.
func TestKriterijiObraneDionice(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	kriteriji, err := o.svc.GetSectionGaugeCriteria(ctx, "P.1.1")
	if err != nil {
		t.Fatal(err)
	}
	var retci []string
	for _, k := range kriteriji {
		retci = append(retci, k.StationName)
	}
	if got := strings.Join(retci, " | "); got != "Pravilnik o radu akumulacije Probna | rkm 14,0 | Ustava Probna" {
		t.Errorf("kriteriji P.1.1: %s", got)
	}
	if k, err := o.svc.GetSectionGaugeCriteria(ctx, "P.1.2"); err != nil || len(k) != 0 {
		t.Errorf("dionica bez redaka: %+v %v", k, err)
	}
	if _, err := o.svc.GetSectionGaugeCriteria(ctx, "P.1.9"); !errors.Is(err, service.ErrSectionNotFound) {
		t.Errorf("nepostojeća dionica: %v", err)
	}

	o.pokvari(t, "section_stations")
	if _, err := o.svc.GetSectionGaugeCriteria(ctx, "P.1.1"); err == nil {
		t.Error("greška čitanja vodomjera dionice nije vraćena")
	}
	o.pokvari(t, "sections")
	if _, err := o.svc.GetSectionGaugeCriteria(ctx, "P.1.1"); err == nil || errors.Is(err, service.ErrSectionNotFound) {
		t.Errorf("greška čitanja dionice: %v", err)
	}
}

// Postaju uz dionicu upisuje onaj tko smije uređivati tu dionicu; tko ne
// smije, dobije poruku s dionicom i postaja se ne upisuje. Nepostojeća
// dionica se javlja kao takva. Postaju izvan dionice upisuje samo
// globalni administrator, a bez podataka o postaji nema upisa.
func TestUpisPostajeUzDionicu(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	upisana := func(ime string) bool {
		t.Helper()
		st, err := o.svc.ListStations(ctx, ime, "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		return len(st) > 0
	}
	podrucje1 := &models.UserPermissions{AllowedAreas: map[int]bool{1: true}}
	podrucje2 := &models.UserPermissions{AllowedAreas: map[int]bool{2: true}, AdminAreas: map[int]bool{2: true}}

	for _, x := range []struct {
		opis    string
		perms   *models.UserPermissions
		postaja *models.Station
		dionica string
		greska  string
	}{
		{"bez podataka", globalAdmin(), nil, "", "podaci o postaji nedostaju"},
		{"tuđe područje", podrucje2, &models.Station{Name: "Novi vodokaz"}, "P.1.1",
			"nemate ovlasti za izmjenu mjerodavnih vodomjera na dionici P.1.1"},
		{"bez ovlasti", nil, &models.Station{Name: "Novi vodokaz"}, "P.1.1",
			"nemate ovlasti za izmjenu mjerodavnih vodomjera na dionici P.1.1"},
		{"nepostojeća dionica", globalAdmin(), &models.Station{Name: "Novi vodokaz"}, "P.1.9", service.ErrSectionNotFound.Error()},
		{"izvan dionice bez globalne uprave", podrucje1, &models.Station{Name: "Novi vodokaz"}, "",
			"postaju izvan dionice može unijeti samo globalni administrator"},
	} {
		err := o.svc.CreateStation(ctx, x.perms, x.postaja, x.dionica)
		if err == nil || err.Error() != x.greska {
			t.Errorf("%s: %v, želi %q", x.opis, err, x.greska)
		}
	}
	if upisana("Novi vodokaz") {
		t.Fatal("odbijena postaja je upisana")
	}

	nova := &models.Station{Name: "Novi vodokaz"}
	if err := o.svc.CreateStation(ctx, podrucje1, nova, "P.1.1"); err != nil {
		t.Fatal(err)
	}
	if !upisana("Novi vodokaz") || nova.Code == "" {
		t.Fatalf("postaja nije upisana: %+v", nova)
	}
}

// Postaju iz registra briše samo globalni administrator, jer nestaje sa
// svih dionica; drugima se brisanje odbija i postaja ostaje.
func TestBrisanjePostajeSamoGlobalniAdministrator(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	st := o.letva(t, "Probna letva", "", "P.1.1", "P.1.2")
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
	}{
		{"bez ovlasti", nil},
		{"uprava sektora", &models.UserPermissions{AdminSectors: map[string]bool{"P": true}}},
	} {
		if err := o.svc.DeleteStation(ctx, x.perms, st.ID); err == nil ||
			err.Error() != "vodomjernu postaju iz registra može obrisati samo globalni administrator" {
			t.Errorf("%s: %v", x.opis, err)
		}
		if got, err := o.svc.GetStation(ctx, st.ID); err != nil || got == nil {
			t.Fatalf("%s: odbijeno brisanje obrisalo je postaju (%v)", x.opis, err)
		}
	}
	if err := o.svc.DeleteStation(ctx, globalAdmin(), st.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := o.svc.GetStation(ctx, st.ID); err != nil || got != nil {
		t.Fatalf("postaja nije obrisana: %+v %v", got, err)
	}
	if na, err := o.svc.GetSectionStations(ctx, "P.1.2"); err != nil || len(na) != 1 || na[0].Name != "Ustava Probna" {
		t.Errorf("obrisana postaja ostala na dionici: %+v %v", na, err)
	}
}
