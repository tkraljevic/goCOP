package service

import (
	"context"
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
)

// Zaključava današnje proglašenje, podizanje i prekid obrane na dionici
// (EpisodeService) i izračun epizoda iz niza očitanja, uz rubne i neispravne
// unose. Gdje je ponašanje upitno, test ga bilježi onakvo kakvo jest.

type okolinaObrane struct {
	svc      *EpisodeService
	epizode  *repository.EpisodeRepository
	ocitanja *repository.ReadingRepository
	rec      *ledger.Recorder
	letva    models.Station
	dionica  *models.UserPermissions
}

const pperic = "pperic"

func novaOkolinaObrane(t *testing.T) *okolinaObrane {
	t.Helper()
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
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.2', 1, 'P', 'kanal Probni', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	ocitanja := repository.NewReadingRepository(baza, rec)
	epizode := repository.NewEpisodeRepository(baza, rec)
	st := letvaBatina()
	st.ID, st.Code, st.Name = uuid.New(), "primjerovo", "Primjerovo"
	if err := stanice.CreateStation(context.Background(), &st); err != nil {
		t.Fatal(err)
	}
	return &okolinaObrane{
		svc: NewEpisodeService(epizode, ocitanja, stanice), epizode: epizode, ocitanja: ocitanja, rec: rec, letva: st,
		dionica: &models.UserPermissions{User: models.User{Username: pperic, FullName: "Pero Perić"},
			AllowedSections: map[string]bool{"P.1.1": true, "P.1.2": true}},
	}
}

func (o *okolinaObrane) ocitanje(t *testing.T, kad time.Time, cm int) {
	t.Helper()
	v := cm
	if err := o.ocitanja.Create(context.Background(), &models.Reading{ID: uuid.New(), StationID: o.letva.ID.String(),
		MeasuredAt: kad, LevelCm: &v, Source: models.ReadingSourceManual}); err != nil {
		t.Fatal(err)
	}
}

func TestObranuNaDioniciProglasavaSamoOnajTkoPiseNaDionici(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	podrucje := models.NewUserPermissions(models.User{Duties: []models.Duty{{Role: models.RoleAreaLeader, SectorID: sektorPtr("P"),
		AreaID: intp(1), IsActive: true}}})
	sektor := models.NewUserPermissions(models.User{Duties: []models.Duty{{Role: models.RoleSectorLeader, SectorID: sektorPtr("P"), IsActive: true}}})
	// Pravo se traži na samoj dionici: rukovoditelj područja i sektora, koji
	// po dionicama inače pišu, ovdje su odbijeni.
	for ime, p := range map[string]*models.UserPermissions{"bez ovlasti": nil, "rukovoditelj područja": podrucje, "rukovoditelj sektora": sektor} {
		if _, err := o.svc.Declare(ctx, p, pperic, "P.1.1", o.letva, time.Time{}, models.PhasePrep, models.BasisOrder, ""); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: proglašenje %v, očekivano ErrUnauthorized", ime, err)
		}
		if err := o.svc.Raise(ctx, p, "P.1.1", models.PhaseRegular, ""); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: podizanje %v", ime, err)
		}
		if err := o.svc.End(ctx, p, pperic, "P.1.1", time.Time{}, ""); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: prekid %v", ime, err)
		}
	}
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	if _, err := o.svc.Declare(ctx, admin, "uprava", "P.1.2", o.letva, time.Time{}, models.PhasePrep, models.BasisOrder, ""); err != nil {
		t.Errorf("globalni administrator proglašava na svakoj dionici: %v", err)
	}
}

func TestProglasenjeObrane(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()

	for _, faza := range []models.DefensePhase{models.PhaseNormal, models.PhaseUnknown, "", "BILO_STO"} {
		if _, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.1", o.letva, time.Time{}, faza, models.BasisOrder, ""); err == nil ||
			!strings.Contains(err.Error(), "odaberi stupanj obrane") {
			t.Errorf("faza %q: %v", faza, err)
		}
	}
	if _, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.1", o.letva, time.Now().Add(61*time.Minute), models.PhasePrep, models.BasisOrder, ""); err == nil ||
		!strings.Contains(err.Error(), "unaprijed") {
		t.Errorf("proglašenje više od sat unaprijed: %v", err)
	}

	// do sat vremena unaprijed se prima; temelj se ne provjerava, napomena se ne reže
	za50 := time.Now().Add(50 * time.Minute)
	e, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.1", o.letva, za50, models.PhaseRegular, "BILO_STO", "  po nalogu  ")
	if err != nil {
		t.Fatal(err)
	}
	if !e.StartedAt.Equal(za50) || e.Origin != models.EpisodeFromOperator || e.DeclaredBy != pperic || e.Basis != "BILO_STO" ||
		e.Note != "  po nalogu  " || e.StationID != o.letva.ID.String() || e.PeakCm != nil || !e.IsOpen() {
		t.Errorf("proglašena epizoda: %+v", e)
	}
	verzije, err := o.rec.History(ctx, "defense_episodes", e.ID.String())
	if err != nil || len(verzije) != 1 {
		t.Errorf("proglašenje mora zapisati jednu verziju u knjigu: %d (%v)", len(verzije), err)
	}

	// druga obrana na istoj dionici dok prva traje ne može
	if _, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.1", o.letva, time.Time{}, models.PhaseState, models.BasisOrder, ""); err == nil ||
		!strings.Contains(err.Error(), "već traje") {
		t.Errorf("dvostruko proglašenje: %v", err)
	}

	// prazno vrijeme je sada
	prije := time.Now()
	e2, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.2", o.letva, time.Time{}, models.PhasePrep, models.BasisOrder, "")
	if err != nil || e2.StartedAt.Before(prije) || e2.StartedAt.After(time.Now()) {
		t.Errorf("prazno vrijeme proglašenja: %v (%v)", e2, err)
	}

	// dionica koje nema u registru: zapis pada na stranom ključu
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	if _, err := o.svc.Declare(ctx, admin, "uprava", "X.9.9", o.letva, time.Time{}, models.PhasePrep, models.BasisOrder, ""); err == nil {
		t.Error("proglašenje na nepostojećoj dionici mora pasti")
	}

	// Letva bez identiteta: epizoda dobije nulti UUID, a ne prazan zapis.
	if err := o.svc.End(ctx, admin, "uprava", "P.1.2", time.Time{}, ""); err != nil {
		t.Fatal(err)
	}
	bez, err := o.svc.Declare(ctx, admin, "uprava", "P.1.2", models.Station{}, time.Time{}, models.PhasePrep, models.BasisOrder, "")
	if err != nil {
		t.Fatal(err)
	}
	if bez.StationID != uuid.Nil.String() {
		t.Errorf("letva bez identiteta: %q, očekivano nulti UUID", bez.StationID)
	}
}

func TestPragPrijedenPoOcitanjima(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	sad := time.Now().UTC().Truncate(time.Minute)
	o.ocitanje(t, sad.Add(-20*24*time.Hour), 900) // izvan prozora od 14 dana
	o.ocitanje(t, sad.Add(-3*24*time.Hour), 250)
	prijelaz := sad.Add(-2 * 24 * time.Hour)
	o.ocitanje(t, prijelaz, 320)
	o.ocitanje(t, sad.Add(-24*time.Hour), 510)

	e, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.1", o.letva, sad, models.PhaseRegular, models.BasisThreshold, "")
	if err != nil {
		t.Fatal(err)
	}
	if e.ThresholdAt == nil || !e.ThresholdAt.Equal(prijelaz) {
		t.Fatalf("prag prijeđen: %v, očekivano %v", e.ThresholdAt, prijelaz)
	}
	if e.DeclaredBeforeThreshold() {
		t.Error("obrana proglašena nakon prelaska praga nije proglašena unaprijed")
	}

	// letva bez pripremnog praga: prelazak se ne traži
	bez := o.letva
	bez.Prep = models.Threshold{}
	e2, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.2", bez, sad, models.PhaseRegular, models.BasisThreshold, "")
	if err != nil || e2.ThresholdAt != nil {
		t.Errorf("bez pripremnog praga: %v (%v)", e2.ThresholdAt, err)
	}
}

func TestPodizanjeStupnjaObrane(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	if err := o.svc.Raise(ctx, o.dionica, "P.1.1", models.PhaseRegular, ""); err == nil || !strings.Contains(err.Error(), "ne traje nijedna obrana") {
		t.Errorf("podizanje bez obrane: %v", err)
	}
	if _, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.1", o.letva, time.Time{}, models.PhaseRegular, models.BasisOrder, "početak"); err != nil {
		t.Fatal(err)
	}
	// stupanj se ne spušta i ne ponavlja; nepoznat stupanj je „niži”
	for _, faza := range []models.DefensePhase{models.PhaseRegular, models.PhasePrep, models.PhaseNormal, "BILO_STO"} {
		if err := o.svc.Raise(ctx, o.dionica, "P.1.1", faza, ""); err == nil || !strings.Contains(err.Error(), "obrana je već na stupnju") {
			t.Errorf("podizanje na %q: %v", faza, err)
		}
	}
	if err := o.svc.Raise(ctx, o.dionica, "P.1.1", models.PhaseState, "  voda raste "); err != nil {
		t.Fatal(err)
	}
	e, err := o.svc.Open(ctx, "P.1.1")
	if err != nil || e == nil {
		t.Fatal(err)
	}
	if e.Phase != models.PhaseState || e.Note != "početak\n  voda raste" {
		t.Errorf("nakon podizanja: faza %s, napomena %q", e.Phase, e.Note)
	}
}

func TestPrekidObrane(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	if err := o.svc.End(ctx, o.dionica, pperic, "P.1.1", time.Time{}, ""); err == nil || !strings.Contains(err.Error(), "ne traje nijedna obrana") {
		t.Errorf("prekid bez obrane: %v", err)
	}
	pocetak := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	if _, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.1", o.letva, pocetak, models.PhaseEmergency, models.BasisOrder, ""); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.End(ctx, o.dionica, pperic, "P.1.1", pocetak.Add(-time.Minute), ""); err == nil || !strings.Contains(err.Error(), "prije nego što je proglašena") {
		t.Errorf("prekid prije proglašenja: %v", err)
	}
	// Prekid se smije upisati unaprijed: rok u budućnosti se ne provjerava,
	// a obrana odmah prestaje biti otvorena.
	sutra := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	if err := o.svc.End(ctx, o.dionica, "pperic-zamjena", "P.1.1", sutra, "voda pada"); err != nil {
		t.Fatalf("prekid unaprijed: %v", err)
	}
	if otvorena, err := o.svc.Open(ctx, "P.1.1"); err != nil || otvorena != nil {
		t.Fatalf("nakon prekida obrana i dalje traje: %+v (%v)", otvorena, err)
	}
	sve, err := o.svc.List(ctx, "P.1.1")
	if err != nil || len(sve) != 1 {
		t.Fatalf("epizode dionice: %d (%v)", len(sve), err)
	}
	e := sve[0]
	if e.EndedAt == nil || !e.EndedAt.Equal(sutra) || e.EndedBy != "pperic-zamjena" || e.Phase != models.PhaseEmergency || e.Note != "voda pada" {
		t.Errorf("prekinuta epizoda: %+v", e)
	}

	// prekid u istom trenutku kad je proglašena je dopušten
	if _, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.2", o.letva, pocetak, models.PhasePrep, models.BasisOrder, ""); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.End(ctx, o.dionica, pperic, "P.1.2", pocetak, ""); err != nil {
		t.Errorf("prekid u trenutku proglašenja: %v", err)
	}
}

func TestOtvoreneObraneSektoraBezIdentiteta(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	e, err := o.svc.Declare(ctx, o.dionica, pperic, "P.1.1", o.letva, time.Time{}, models.PhasePrep, models.BasisOrder, "")
	if err != nil {
		t.Fatal(err)
	}
	otvorene, err := o.epizode.OpenEpisodesInSector(ctx, "P")
	if err != nil || len(otvorene) != 1 {
		t.Fatalf("otvorene obrane sektora P: %d (%v)", len(otvorene), err)
	}
	// Identitet se čita, ali ne upisuje u rezultat.
	if otvorene[0].ID != uuid.Nil || otvorene[0].SectionCode != "P.1.1" || e.ID == uuid.Nil {
		t.Errorf("otvorena obrana sektora: ID %s, dionica %s", otvorene[0].ID, otvorene[0].SectionCode)
	}
}

func TestRacunataEpizodaNaRubovima(t *testing.T) {
	st := letvaBatina()
	niz := []ocitanje{
		{dan(2026, 3, 1), 299},
		{dan(2026, 3, 2), 300}, // točno pripremni prag otvara epizodu
		{dan(2026, 3, 3), 510},
		{dan(2026, 3, 4), 510}, // jednaki vrh ne pomiče trenutak vrha
		{dan(2026, 3, 5), 299},
		{dan(2026, 3, 6), 820},
	}
	ep := izracunaj(niz, st)
	if len(ep) != 2 {
		t.Fatalf("epizoda: %d, očekivano 2", len(ep))
	}
	prva := ep[0]
	if !prva.StartedAt.Equal(dan(2026, 3, 2)) || prva.Phase != models.PhaseRegular || *prva.PeakCm != 510 || !prva.PeakAt.Equal(dan(2026, 3, 3)) {
		t.Errorf("prva epizoda: %+v", prva)
	}
	if prva.EndedAt == nil || !prva.EndedAt.Equal(dan(2026, 3, 4)) {
		t.Errorf("prva epizoda završava zadnjim očitanjem iznad praga: %v", prva.EndedAt)
	}
	// Epizoda na kraju niza dobije kraj zadnjim očitanjem, pa nije otvorena.
	druga := ep[1]
	if druga.EndedAt == nil || druga.IsOpen() || druga.Phase != models.PhaseState {
		t.Errorf("epizoda na kraju niza: kraj %v, otvorena %v, faza %s", druga.EndedAt, druga.IsOpen(), druga.Phase)
	}
}

func sektorPtr(s string) *string { return &s }
