package service

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zaključava kako stanje obrane na dionici nastaje i mijenja se: isključivo
// iz ovjerenih akata o obrani, uz prag iz očitanja i otvorene obrane
// sektora. Gdje je ponašanje upitno, test ga bilježi onakvo kakvo jest.
//
// Namjerno promijenjeno prema Odlukama (8. 10. 2026.), pitanje 14: ručno
// proglašenje, podizanje i prekid obrane (EpisodeService.Declare, Raise,
// End) i računanje epizoda iz vodostaja (Rebuild, izracunaj) obrisani su, pa
// provjere početno stanje stvaraju ovjerenim aktom, kao u stvarnom radu.

type okolinaObrane struct {
	svc      *EpisodeService
	akti     *AktService
	epizode  *repository.EpisodeRepository
	ocitanja *repository.ReadingRepository
	stanice  *repository.StationRepository
	rec      *ledger.Recorder
	letva    models.Station
	pero     *models.User
	uprava   *models.UserPermissions
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
	svc := NewEpisodeService(epizode, ocitanja, stanice)
	pero := &models.User{ID: uuid.New(), Username: pperic, FullName: "Pero Perić"}
	korisnici := repository.NewUserRepository(baza, rec)
	users := NewUserService(korisnici, NewAuthService(korisnici, repository.NewSessionRepository(baza)), NewSSEBroker())
	return &okolinaObrane{
		svc: svc, akti: NewAktService(repository.NewAktiRepository(baza, rec), stanice, repository.NewSectionRepository(baza, rec),
			repository.NewTerritoryRepository(baza, rec), nil, users, svc, "cvor-probni"),
		epizode: epizode, ocitanja: ocitanja, stanice: stanice, rec: rec, letva: st,
		pero: pero, uprava: &models.UserPermissions{IsGlobalAdmin: true, User: *pero},
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

// ovjeriAkt sprema i ovjerava akt o obrani po letvi (prazno: bez letve) na
// zadanim dionicama: jedini put kojim se mijenja stanje obrane
func (o *okolinaObrane) ovjeriAkt(t *testing.T, letva, radnja string, stupanj models.DefensePhase, vrijedi time.Time, dionice ...string) *models.Akt {
	t.Helper()
	ctx := context.Background()
	a := &models.Akt{Sektor: "P", AreaID: 1, Radnja: radnja, Stupanj: stupanj, Vrijedi: vrijedi, StationID: letva, StationName: "Primjerovo"}
	for _, d := range dionice {
		a.Dionice = append(a.Dionice, models.AktDionica{Code: d})
	}
	// Testovi obrane namjerno proglašavaju i unatrag: „Vrijedi od” izvan
	// uobičajenog raspona je potvrđen (Odluke (8. 10. 2026.), pitanje 17).
	a.Potvrde = potvrdeVrijedi(a, o.pero, true, time.Now())
	if err := o.akti.Spremi(ctx, o.uprava, a); err != nil {
		t.Fatalf("spremanje akta %s %s: %v", radnja, stupanj, err)
	}
	ovjeren, upozorenja, err := o.akti.Ovjeri(ctx, o.uprava, o.pero, a.ID, PotvrdaOvjere{Primatelji: true, Vrijedi: true})
	if err != nil || len(upozorenja) != 0 {
		t.Fatalf("ovjera akta %s %s: %v (upozorenja %v)", radnja, stupanj, err, upozorenja)
	}
	return ovjeren
}

// otvorena vraća obranu koja na dionici traje i pada ako je nema
func (o *okolinaObrane) otvorena(t *testing.T, dionica string) *models.DefenseEpisode {
	t.Helper()
	e, err := o.svc.Open(context.Background(), dionica)
	if err != nil || e == nil {
		t.Fatalf("na dionici %s ne traje obrana: %+v (%v)", dionica, e, err)
	}
	return e
}

// Hitnog ručnog proglašenja mimo akta nema, ni u noći izvanredne obrane:
// servis obrane izvana samo čita, a piše jedino povijest izvedenu iz
// ovjerenih akata. Novi put do stanja obrane bio bi nova odluka o pravilima
// obrane i mora svjesno promijeniti ovaj test.
func TestObranaSeNeMijenjaMimoAkta(t *testing.T) {
	tip := reflect.TypeOf(&EpisodeService{})
	var metode []string
	for i := 0; i < tip.NumMethod(); i++ {
		metode = append(metode, tip.Method(i).Name)
	}
	if want := []string{"ByStation", "List", "Open", "UskladiIzAkata"}; !reflect.DeepEqual(metode, want) {
		t.Errorf("javne metode servisa obrane: %v, očekivano %v", metode, want)
	}
}

// Ovjeren akt otvara epizodu na svojoj dionici: početak je vrijeme iz akta,
// proglasio je onaj tko je akt ovjerio, osnova je prijeđen prag, a u knjigu
// ide jedna verzija. Akt bez letve daje epizodu bez letve (prazno, a ne
// nulti UUID) i bez praga.
func TestObranaIzOvjerenogAkta(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	pocetak := time.Now().Add(-time.Hour).UTC().Truncate(time.Minute)
	a := o.ovjeriAkt(t, o.letva.ID.String(), models.AktUspostava, models.PhaseRegular, pocetak, "P.1.1")

	e := o.otvorena(t, "P.1.1")
	if e.ID != models.IDEpizodeIzAkta("P.1.1", a.ID) || !e.StartedAt.Equal(pocetak) || e.Phase != models.PhaseRegular ||
		e.Origin != models.EpisodeFromOperator || e.DeclaredBy != o.pero.ID.String() || e.Basis != models.BasisThreshold ||
		e.StationID != o.letva.ID.String() || e.PeakCm != nil || e.Note != a.Naslov()+" "+a.Oznaka() {
		t.Errorf("epizoda iz akta: %+v", e)
	}
	verzije, err := o.rec.History(ctx, "defense_episodes", e.ID.String())
	if err != nil || len(verzije) != 1 {
		t.Errorf("ovjera mora zapisati jednu verziju epizode u knjigu: %d (%v)", len(verzije), err)
	}

	o.ovjeriAkt(t, "", models.AktUspostava, models.PhasePrep, pocetak, "P.1.2")
	if bez := o.otvorena(t, "P.1.2"); bez.StationID != "" || bez.ThresholdAt != nil {
		t.Errorf("akt bez letve: letva %q, prag %v", bez.StationID, bez.ThresholdAt)
	}
}

func TestPragPrijedenPoOcitanjima(t *testing.T) {
	o := novaOkolinaObrane(t)
	sad := time.Now().UTC().Truncate(time.Minute)
	o.ocitanje(t, sad.Add(-20*24*time.Hour), 900) // izvan prozora od 14 dana
	o.ocitanje(t, sad.Add(-3*24*time.Hour), 250)
	prijelaz := sad.Add(-2 * 24 * time.Hour)
	o.ocitanje(t, prijelaz, 320)
	o.ocitanje(t, sad.Add(-24*time.Hour), 510)

	o.ovjeriAkt(t, o.letva.ID.String(), models.AktUspostava, models.PhaseRegular, sad, "P.1.1")
	e := o.otvorena(t, "P.1.1")
	if e.ThresholdAt == nil || !e.ThresholdAt.Equal(prijelaz) {
		t.Fatalf("prag prijeđen: %v, očekivano %v", e.ThresholdAt, prijelaz)
	}
	if e.DeclaredBeforeThreshold() {
		t.Error("obrana proglašena nakon prelaska praga nije proglašena unaprijed")
	}

	// letva bez pripremnog praga: prelazak se ne traži
	bez := models.Station{ID: uuid.New(), Code: "primjerovo-bez-praga", Name: "Primjerovo bez praga"}
	if err := o.stanice.CreateStation(context.Background(), &bez); err != nil {
		t.Fatal(err)
	}
	o.ovjeriAkt(t, bez.ID.String(), models.AktUspostava, models.PhaseRegular, sad, "P.1.2")
	if e2 := o.otvorena(t, "P.1.2"); e2.ThresholdAt != nil {
		t.Errorf("bez pripremnog praga: %v", e2.ThresholdAt)
	}
}

// Viši stupanj uspostavlja se novim aktom: epizoda ostaje ista i nosi najviši
// dosegnuti stupanj, a bilješka nabraja akte razdoblja. Prekid višega dok
// niži traje ne spušta zabilježeni stupanj i ne zatvara epizodu.
func TestPodizanjeStupnjaObrane(t *testing.T) {
	o := novaOkolinaObrane(t)
	sat := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Minute)
	letva := o.letva.ID.String()
	prvi := o.ovjeriAkt(t, letva, models.AktUspostava, models.PhaseRegular, sat, "P.1.1")
	drugi := o.ovjeriAkt(t, letva, models.AktUspostava, models.PhaseState, sat.Add(10*time.Minute), "P.1.1")

	e := o.otvorena(t, "P.1.1")
	if e.ID != models.IDEpizodeIzAkta("P.1.1", prvi.ID) || e.Phase != models.PhaseState ||
		e.Note != prvi.Naslov()+" "+prvi.Oznaka()+"\n"+drugi.Naslov()+" "+drugi.Oznaka() {
		t.Errorf("nakon podizanja: %s, faza %s, napomena %q", e.ID, e.Phase, e.Note)
	}

	o.ovjeriAkt(t, letva, models.AktPrekid, models.PhaseState, sat.Add(20*time.Minute), "P.1.1")
	if e := o.otvorena(t, "P.1.1"); e.Phase != models.PhaseState || e.EndedAt != nil {
		t.Errorf("prekid izvanrednog stanja dok redovna traje: faza %s, kraj %v", e.Phase, e.EndedAt)
	}
}

// Prekid najvišeg stadija, kad ništa niže ne traje, zatvara epizodu: kraj je
// vrijeme iz akta o prekidu, a prekinuo je onaj tko ga je ovjerio. Stupanj i
// akti razdoblja ostaju zabilježeni.
func TestPrekidObrane(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	pocetak := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Minute)
	kraj := time.Now().Add(-time.Hour).UTC().Truncate(time.Minute)
	letva := o.letva.ID.String()
	u := o.ovjeriAkt(t, letva, models.AktUspostava, models.PhaseEmergency, pocetak, "P.1.1")
	p := o.ovjeriAkt(t, letva, models.AktPrekid, models.PhaseEmergency, kraj, "P.1.1")

	if otvorena, err := o.svc.Open(ctx, "P.1.1"); err != nil || otvorena != nil {
		t.Fatalf("nakon prekida obrana i dalje traje: %+v (%v)", otvorena, err)
	}
	sve, err := o.svc.List(ctx, "P.1.1")
	if err != nil || len(sve) != 1 {
		t.Fatalf("epizode dionice: %d (%v)", len(sve), err)
	}
	e := sve[0]
	if e.EndedAt == nil || !e.EndedAt.Equal(kraj) || e.EndedBy != o.pero.ID.String() || e.Phase != models.PhaseEmergency ||
		!strings.Contains(e.Note, u.Oznaka()) || !strings.Contains(e.Note, p.Oznaka()) {
		t.Errorf("prekinuta epizoda: %+v", e)
	}
}

func TestOtvoreneObraneSektoraBezIdentiteta(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	a := o.ovjeriAkt(t, o.letva.ID.String(), models.AktUspostava, models.PhasePrep, time.Now().Add(-time.Hour), "P.1.1")
	otvorene, err := o.epizode.OpenEpisodesInSector(ctx, "P")
	if err != nil || len(otvorene) != 1 {
		t.Fatalf("otvorene obrane sektora P: %d (%v)", len(otvorene), err)
	}
	// Otvorena obrana sektora nosi svoj identitet, kao i Open.
	if otvorene[0].ID != models.IDEpizodeIzAkta("P.1.1", a.ID) || otvorene[0].SectionCode != "P.1.1" {
		t.Errorf("otvorena obrana sektora: ID %s, dionica %s", otvorene[0].ID, otvorene[0].SectionCode)
	}
}
