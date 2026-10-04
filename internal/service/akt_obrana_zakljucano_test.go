package service_test

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
	"gocop/internal/service"
)

// Zaključava kako ovjera akta o obrani mijenja stanje obrane na dionicama:
// akt ovlašćuje promjenu na svojim dionicama, bez obzira na prava onoga tko
// ovjerava; uspostava otvara ili podiže obranu, prekid pripremnog stanja je
// prekida. Upitna mjesta test bilježi onakva kakva jesu.

type okolinaAkta struct {
	akti    *service.AktService
	epizode *service.EpisodeService
	letva   *models.Station
	rukovod *models.User
	ovlasti *models.UserPermissions
	vodocuv *models.User
	vodOvl  *models.UserPermissions
}

func novaOkolinaAkta(t *testing.T) *okolinaAkta {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akti.db"))
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
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	epizode := service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, repository.NewSectionRepository(baza, rec),
		repository.NewTerritoryRepository(baza, rec), readingRepo, users, epizode, "test")

	ctx := context.Background()
	p, r, e, s := 300, 500, 650, 800
	st := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica",
		Prep: models.Threshold{Cm: &p}, Regular: models.Threshold{Cm: &r}, Emergency: models.Threshold{Cm: &e}, State: models.Threshold{Cm: &s}}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"P.1.1", "P.1.2"} {
		if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), code, st.ID.String(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for i, cm := range []int{310, 320} {
		v := cm
		if err := readingRepo.Create(ctx, &models.Reading{ID: uuid.New(), StationID: st.ID.String(),
			MeasuredAt: time.Now().Add(time.Duration(i-3) * time.Hour), LevelCm: &v, Source: models.ReadingSourceManual}); err != nil {
			t.Fatal(err)
		}
	}
	sektor, podrucje := "P", 1
	osoba := func(ime, puno string, d *models.Duty) (*models.User, *models.UserPermissions) {
		u := &models.User{ID: uuid.New(), Username: ime, FullName: puno, IsActive: true}
		if err := userRepo.CreateUser(u, d); err != nil {
			t.Fatal(err)
		}
		u, err := userRepo.GetUserByID(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return u, models.NewUserPermissions(*u)
	}
	ruk, rukOvl := osoba("pperic", "Pero Perić", &models.Duty{Title: "Rukovoditelj BP 1", Role: models.RoleAreaLeader,
		ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsPrimary: true})
	vod, vodOvl := osoba("pperic-vodocuvar", "Pero Perić", &models.Duty{Title: "Vodočuvar P.1.1", Role: models.RoleWaterGuard,
		ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: "P.1.1", IsPrimary: true})
	return &okolinaAkta{akti: akti, epizode: epizode, letva: st, rukovod: ruk, ovlasti: rukOvl, vodocuv: vod, vodOvl: vodOvl}
}

// ovjeri priprema, sprema i ovjerava akt rukovoditelja područja
func (o *okolinaAkta) ovjeri(t *testing.T, radnja string, stupanj models.DefensePhase, vrijedi time.Time) (*models.Akt, []string) {
	t.Helper()
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(), Radnja: radnja, Stupanj: stupanj, Vrijedi: vrijedi})
	if err != nil {
		t.Fatalf("priprema %s %s: %v", radnja, stupanj, err)
	}
	if err := o.akti.Spremi(ctx, o.ovlasti, a); err != nil {
		t.Fatalf("spremanje: %v", err)
	}
	ovjeren, upozorenja, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID)
	if err != nil {
		t.Fatalf("ovjera %s %s: %v", radnja, stupanj, err)
	}
	return ovjeren, upozorenja
}

func (o *okolinaAkta) stanje(t *testing.T) map[string]models.DefensePhase {
	t.Helper()
	out := map[string]models.DefensePhase{}
	for _, d := range []string{"P.1.1", "P.1.2"} {
		e, err := o.epizode.Open(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		if e != nil {
			out[d] = e.Phase
		}
	}
	return out
}

func TestOvjeraAktaMijenjaStanjeObrane(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	sat := time.Now().Add(-time.Hour).Truncate(time.Minute)

	// Rukovoditelj područja ne piše izravno na dionicama, pa obranu sam ne
	// proglašava; ovjereni akt to ipak učini na svim dionicama akta.
	if _, err := o.epizode.Declare(ctx, o.ovlasti, o.rukovod.ID.String(), "P.1.1", *o.letva, sat, models.PhasePrep, models.BasisOrder, ""); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("izravno proglašenje rukovoditelja područja: %v", err)
	}
	a, upozorenja := o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat)
	if len(upozorenja) != 0 || a.Status != models.AktOvjeren || a.Broj != 1 {
		t.Fatalf("uspostava pripremnog: upozorenja %v, status %s, broj %d", upozorenja, a.Status, a.Broj)
	}
	if got := o.stanje(t); got["P.1.1"] != models.PhasePrep || got["P.1.2"] != models.PhasePrep {
		t.Fatalf("nakon uspostave pripremnog: %v", got)
	}
	e, _ := o.epizode.Open(ctx, "P.1.1")
	if e.DeclaredBy != o.rukovod.ID.String() || e.Basis != models.BasisThreshold || !strings.Contains(e.Note, a.Oznaka()) {
		t.Errorf("epizoda iz akta: proglasio %s, temelj %s, napomena %q", e.DeclaredBy, e.Basis, e.Note)
	}

	// viši stupanj podiže obranu, niži ili isti ne mijenja ništa i ne upozorava
	a2, upozorenja := o.ovjeri(t, models.AktUspostava, models.PhaseRegular, sat.Add(10*time.Minute))
	if len(upozorenja) != 0 || a2.Broj != 2 {
		t.Errorf("uspostava redovne: upozorenja %v, broj %d", upozorenja, a2.Broj)
	}
	if _, upozorenja := o.ovjeri(t, models.AktUspostava, models.PhasePrep, sat.Add(20*time.Minute)); len(upozorenja) != 0 {
		t.Errorf("uspostava nižeg stupnja ne smije upozoravati: %v", upozorenja)
	}
	if got := o.stanje(t); got["P.1.1"] != models.PhaseRegular || got["P.1.2"] != models.PhaseRegular {
		t.Fatalf("nakon podizanja i niže uspostave: %v", got)
	}

	// Prekid stupnja koji nije pripremni ne mijenja stanje, a za svaku
	// dionicu vrati upozorenje „obrana je već na stupnju …”.
	_, upozorenja = o.ovjeri(t, models.AktPrekid, models.PhaseRegular, sat.Add(30*time.Minute))
	if len(upozorenja) != 2 || !strings.Contains(upozorenja[0], "obrana je već na stupnju") {
		t.Errorf("prekid redovne: upozorenja %v", upozorenja)
	}
	if got := o.stanje(t); got["P.1.1"] != models.PhaseRegular {
		t.Errorf("prekid redovne ne smije mijenjati stanje: %v", got)
	}

	// prekid pripremnog stanja prekida obranu bez obzira na njezin stupanj
	if _, upozorenja := o.ovjeri(t, models.AktPrekid, models.PhasePrep, sat.Add(40*time.Minute)); len(upozorenja) != 0 {
		t.Errorf("prekid pripremnog: %v", upozorenja)
	}
	if got := o.stanje(t); len(got) != 0 {
		t.Fatalf("nakon prekida pripremnog obrana i dalje traje: %v", got)
	}

	// Akt koji vrijedi tek za dva sata se ovjeri, ali obrana se ne otvori:
	// ostaje samo upozorenje, i ništa je poslije ne otvara.
	a3, upozorenja := o.ovjeri(t, models.AktUspostava, models.PhasePrep, time.Now().Add(2*time.Hour))
	if a3.Status != models.AktOvjeren || len(upozorenja) != 2 || !strings.Contains(upozorenja[0], "unaprijed") {
		t.Errorf("uspostava za dva sata: status %s, upozorenja %v", a3.Status, upozorenja)
	}
	if got := o.stanje(t); len(got) != 0 {
		t.Errorf("akt za dva sata otvorio je obranu: %v", got)
	}
}

func TestOvjeraBezOvlasti(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, service.ZahtjevAkta{StationID: o.letva.ID.String(),
		Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.akti.Spremi(ctx, o.ovlasti, a); err != nil {
		t.Fatal(err)
	}
	// vodočuvar piše na dionici akta, ali akt ne ovjerava
	if o.akti.SmijeOvjeriti(o.vodOvl, a) {
		t.Fatal("vodočuvar ne ovjerava akt o obrani")
	}
	if _, _, err := o.akti.Ovjeri(ctx, o.vodOvl, o.vodocuv, a.ID); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("ovjera bez ovlasti: %v", err)
	}
	if e, _ := o.epizode.Open(ctx, "P.1.1"); e != nil {
		t.Error("neovjereni akt ne smije otvoriti obranu")
	}
	// ovjeren akt se ne ovjerava ponovno
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.akti.Ovjeri(ctx, o.ovlasti, o.rukovod, a.ID); err == nil {
		t.Error("druga ovjera istog akta mora biti odbijena")
	}
	// ni nepoznatih ulaza
	for ime, z := range map[string]service.ZahtjevAkta{
		"bez radnje":     {StationID: o.letva.ID.String(), Stupanj: models.PhasePrep},
		"stupanj normal": {StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhaseNormal},
		"kriva letva":    {StationID: "nije-uuid", Radnja: models.AktUspostava, Stupanj: models.PhasePrep},
		"tuđe dionice":   {StationID: o.letva.ID.String(), Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Dionice: []string{"X.9.9"}},
	} {
		if _, err := o.akti.Pripremi(ctx, o.ovlasti, o.rukovod, z); err == nil {
			t.Errorf("%s: priprema mora biti odbijena", ime)
		}
	}
}
