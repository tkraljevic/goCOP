package web

import (
	"context"
	"errors"
	"net/http"
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

func nizNaslovne(s string) *string { return &s }
func brojNaslovne(n int) *int      { return &n }

// Traka vodočuvara govori o vlastitom području i najvišem stupnju u ostatku
// sektora (Odluke (31. 10. 2026.))
func TestTrakaPodrucja(t *testing.T) {
	jutros := time.Now().In(models.Zagreb).Truncate(time.Hour)
	dionice := []models.Section{{Code: "B.16.1", AreaID: 16}, {Code: "B.16.2", AreaID: 16}, {Code: "B.34.1", AreaID: 34}}
	stanje := func(f models.DefensePhase, od time.Time) models.StanjeObrane {
		return models.StanjeObrane{Aktivni: []models.AktivniStadij{{Stupanj: f, Od: od}}}
	}
	if tr := trakaPodrucja(16, "B", nil, dionice); tr.Tekst != "BP 16: preventivna obrana" || tr.Razred != "" {
		t.Errorf("mirno: %+v", tr)
	}
	tr := trakaPodrucja(16, "B", map[string]models.StanjeObrane{"B.34.1": stanje(models.PhasePrep, jutros)}, dionice)
	if tr.Tekst != "BP 16: preventivna obrana · Sektor B: pripremno stanje na BP 34 od "+jutros.Format("15:04") || tr.Razred != "" {
		t.Errorf("obrana u susjednom području: %+v", tr)
	}
	davno := time.Date(2026, 10, 30, 6, 15, 0, 0, models.Zagreb)
	tr = trakaPodrucja(16, "B", map[string]models.StanjeObrane{"B.16.2": stanje(models.PhaseRegular, davno), "B.34.1": stanje(models.PhasePrep, jutros)}, dionice)
	if tr.Tekst != "BP 16: redovna obrana od 30. 10." || tr.Razred != "regular" {
		t.Errorf("obrana u vlastitom području: %+v", tr)
	}
}

// Dežura se od pripremnog stanja: pripremno radnim danom 7–15, redovna
// 7–19, izvanredna i izvanredno stanje cijeli dan (Odluke (1. 11. 2026.))
func TestDezurstvoTraje(t *testing.T) {
	pon := func(sat int) time.Time { return time.Date(2026, 11, 2, sat, 0, 0, 0, models.Zagreb) }
	sub := time.Date(2026, 11, 7, 10, 0, 0, 0, models.Zagreb)
	for _, s := range []struct {
		f    models.DefensePhase
		t    time.Time
		zeli bool
	}{
		{models.PhaseNormal, pon(10), false}, {models.PhasePrep, pon(10), true}, {models.PhasePrep, pon(16), false},
		{models.PhasePrep, sub, false}, {models.PhaseRegular, pon(18), true}, {models.PhaseRegular, pon(19), false},
		{models.PhaseEmergency, pon(3), true}, {models.PhaseState, sub, true},
	} {
		if got := DezurstvoTraje(s.f, s.t); got != s.zeli {
			t.Errorf("%s u %v: %v", s.f, s.t, got)
		}
	}
}

// Dnevni list, zadaci i moje letve vodočuvara
func TestKarticeVodocuvara(t *testing.T) {
	sad := time.Date(2026, 11, 2, 10, 0, 0, 0, models.Zagreb)
	if k := karticaDnevnogLista(nil, sad); k.Naslov != "Dnevni list · pon 2. 11." || k.Tekst != "Danas još nije otvoren." || len(k.Gumbi) != 1 {
		t.Errorf("bez lista: %+v", k)
	}
	otvoren := &models.VodocuvarskiList{ID: "l1", CreatedAt: sad.Add(-3 * time.Hour)}
	if k := karticaDnevnogLista(otvoren, sad); k.Tekst != "Otvoren u 07:00 · još nije predan" || len(k.Gumbi) != 2 || !k.Gumbi[1].SamoSiroko || k.Gumbi[1].URL != "/vodocuvar/l1/list" {
		t.Errorf("otvoren list: %+v", k)
	}
	predano := sad.Add(-time.Hour)
	otvoren.PredanoAt, otvoren.Broj = &predano, 3
	if k := karticaDnevnogLista(otvoren, sad); k.Tekst != "Predan u 09:00." {
		t.Errorf("predan list: %+v", k)
	}

	zadaci := []models.Zadatak{
		{Tekst: "Obiđi nasip", Zadao: "Pero Proba", ZadanoAt: sad.Add(-time.Hour), Status: models.ZadatakOtvoren},
		{Tekst: "Gotovo", Status: models.ZadatakObavljen},
		{Tekst: "Provjeri rešetke", Zadao: "Pero Proba", ZadanoAt: sad.Add(-2 * time.Hour), Status: models.ZadatakOtvoren},
	}
	if k := karticaZadataka(zadaci, sad); k.Broj != 2 || k.Redovi[0].Glavno != "Provjeri rešetke" || k.Redovi[0].Sporedno != "Pero Proba · danas 08:00" || k.Redovi[1].URL != "/vodocuvar/dan" {
		t.Errorf("zadaci: %+v", k)
	}
	if k := karticaZadataka(zadaci[1:2], sad); k.Prazno == nil || k.Prazno.Vrsta != PraznoGotovo {
		t.Errorf("bez otvorenih: %+v", k)
	}

	letve := []models.GaugeSummary{
		{Name: "Batina", URL: "/l/b", DoneToday: true, Latest: &models.Reading{LevelCm: brojNaslovne(312), MeasuredAt: sad.Add(-3 * time.Hour)}},
		{Name: "Branjin Vrh", URL: "/l/v", NewURL: "/readings/new?station=v"},
	}
	k := karticaMojihLetava(letve, sad)
	if k.Redovi[0].Vrijednost != "312 cm" || k.Redovi[0].Sporedno != "upisano danas 07:00" || k.Redovi[0].Gumb != nil ||
		k.Redovi[1].Gumb == nil || k.Redovi[1].Gumb.URL != "/readings/new?station=v&back=/" || k.Redovi[1].Sporedno != "danas još nije očitana" {
		t.Errorf("moje letve: %+v", k.Redovi)
	}
	if p := karticaMojihLetava(nil, sad); p.Prazno == nil || p.Prazno.Vrsta != PraznoCeka {
		t.Errorf("bez letava: %+v", p)
	}
}

// Osobe za poziv: rukovoditelj dionice samo za dionicu vodočuvara, broj
// mobitela ili ureda, poziv bez razmaka i crtica
func TestOsobeZaPoziv(t *testing.T) {
	sad := time.Now()
	vod := models.Duty{Role: models.RoleWaterGuard, IsActive: true, SectorID: nizNaslovne("B"), AreaID: brojNaslovne(16), SectionCodes: "B.16.1"}
	duz := func(r models.Role, dionice string) []models.Duty {
		return []models.Duty{{Role: r, IsActive: true, SectorID: nizNaslovne("B"), AreaID: brojNaslovne(16), SectionCodes: dionice}}
	}
	ruk := models.User{FullName: "Ana Proba", MobilePhone: "099 123-4567", Duties: duz(models.RoleAreaLeader, "")}
	if p, ima := osobaZaPoziv(ruk, models.RoleAreaLeader, "rukovoditelj BP %d", vod, sad); !ima || p.Funkcija != "rukovoditelj BP 16" || p.URL() != "tel:0991234567" {
		t.Errorf("rukovoditelj područja: %+v %v", p, ima)
	}
	dio := models.User{FullName: "Ivo Proba", Phone: "N/A", Duties: duz(models.RoleSectionLeader, "B.16.2,B.16.1")}
	if p, ima := osobaZaPoziv(dio, models.RoleSectionLeader, "rukovoditelj dionice %s", vod, sad); !ima || p.Funkcija != "rukovoditelj dionice B.16.1" || p.URL() != "" {
		t.Errorf("rukovoditelj dionice: %+v %v", p, ima)
	}
	tudja := models.User{FullName: "Eva Proba", Duties: duz(models.RoleSectionLeader, "B.16.3")}
	if _, ima := osobaZaPoziv(tudja, models.RoleSectionLeader, "rukovoditelj dionice %s", vod, sad); ima {
		t.Error("rukovoditelj tuđe dionice je u izboru")
	}
	cop := models.User{FullName: "Rea Proba", Phone: "031 200 300", Duties: duz(models.RoleCopLeader, "")}
	if p, ima := osobaZaPoziv(cop, models.RoleCopLeader, "voditelj COP-a, Sektor %s", vod, sad); !ima || p.Funkcija != "voditelj COP-a, Sektor B" || p.Broj != "031 200 300" {
		t.Errorf("voditelj COP-a: %+v %v", p, ima)
	}
	if _, ima := osobaZaPoziv(cop, models.RoleAreaLeader, "x", vod, sad); ima {
		t.Error("osoba bez te uloge je u izboru")
	}
	cijelo := vod
	cijelo.SectionCodes = ""
	if zajednickaDionica(models.Duty{SectionCodes: "B.16.3"}, cijelo) != "B.16.3" {
		t.Error("vodočuvar cijelog područja nema zajedničku dionicu")
	}
	if terenskaDuznost(&models.User{Duties: []models.Duty{vod}}, sad) == nil || terenskaDuznost(&models.User{}, sad) != nil {
		t.Error("terenska dužnost")
	}
}

// Naslovna vodočuvara kroz rukovatelja: traka, gumbi, osobe za poziv,
// dnevni list i zadaci iz dnevnika, moje letve naknadno
func TestNaslovnaVodocuvara(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "vod.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	vs := service.NewVodocuvarService(repository.NewVodocuvarRepository(baza, rec), users, "test")
	vod := dozvoleNaslovne(false, models.RoleWaterGuard)
	vod.User.Duties[0].SectorID, vod.User.Duties[0].AreaID = nizNaslovne("B"), brojNaslovne(16)
	nadzor := uuid.New()
	o.h.SetZid(func() *service.ZidService { return nil }, nil, nil)
	o.h.SetIzvori(IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, []models.Section{{Code: "B.16.1", AreaID: 16}}, nil
		},
		Osobe: func(sektor string, area int, uloga models.Role) ([]models.User, error) {
			if uloga != models.RoleAreaLeader {
				return nil, nil
			}
			u := models.User{ID: nadzor, FullName: "Ana Proba", MobilePhone: "099 1", Duties: []models.Duty{{Role: uloga, IsActive: true, SectorID: nizNaslovne("B"), AreaID: brojNaslovne(16)}}}
			return []models.User{u, u}, nil
		},
		Teren: func(context.Context, *models.UserPermissions, *models.User) (*service.FieldOverview, error) {
			return &service.FieldOverview{Mine: []models.GaugeSummary{{Name: "Batina", NewURL: "/readings/new?station=b"}}}, nil
		},
	})
	bez := o.zovi("/", vod).Body.String()
	if !strings.Contains(bez, "Dnevnik trenutačno nije dostupan.") {
		t.Error("bez dnevnika kartice ne kažu da nije dostupan")
	}
	o.h.SetVodocuvar(func() *service.VodocuvarService { return vs })
	s := o.zovi("/", vod).Body.String()
	for _, zeli := range []string{"BP 16: preventivna obrana", `href="/prijave/nova"`, "Nazovi: Ana Proba", "tel:0991", "Dnevni list ·",
		"Danas još nije otvoren.", "Nema otvorenih zadataka.", `data-lijeno="/naslovna/kartica/moje-letve"`, "naslovna teren"} {
		if !strings.Contains(s, zeli) {
			t.Errorf("Naslovna vodočuvara nema %q", zeli)
		}
	}
	if w := o.zovi("/naslovna/kartica/moje-letve", vod); !strings.Contains(w.Body.String(), "Upiši") || !strings.Contains(w.Body.String(), "&amp;back=/") {
		t.Errorf("moje letve: %s", w.Body.String())
	}
	o.h.SetIzvori(IzvoriNaslovne{Teren: func(context.Context, *models.UserPermissions, *models.User) (*service.FieldOverview, error) {
		return nil, errors.New("baza zauzeta")
	}})
	if w := o.zovi("/naslovna/kartica/moje-letve", vod); !strings.Contains(w.Body.String(), "Letve trenutačno nisu dostupne.") {
		t.Errorf("moje letve s greškom: %s", w.Body.String())
	}
	o.h.SetIzvori(IzvoriNaslovne{})
	if w := o.zovi("/naslovna/kartica/moje-letve", vod); !strings.Contains(w.Body.String(), "Letve trenutačno nisu dostupne.") || w.Code != http.StatusOK {
		t.Errorf("moje letve bez izvora: %s", w.Body.String())
	}
	if s := o.zovi("/", vod).Body.String(); strings.Contains(s, "naslovna-traka") || strings.Contains(s, "Nazovi") {
		t.Error("bez izvora ima traku ili izbor za poziv")
	}
	baza.Close()
	if s := o.zovi("/", vod).Body.String(); !strings.Contains(s, "Dnevnik trenutačno nije dostupan.") {
		t.Error("dnevnik bez baze ne kaže da nije dostupan")
	}
}

// Dežurni je u izboru za poziv samo dok dežurstvo traje
func TestDezurniUIzboruZaPoziv(t *testing.T) {
	h := &DashboardHandler{}
	dezurni := uuid.New()
	vod := models.Duty{Role: models.RoleWaterGuard, SectorID: nizNaslovne("B"), AreaID: brojNaslovne(16)}
	od := time.Now()
	stanja := []service.StanjeObrane{{Sektor: "B", Stadij: models.PhaseEmergency, Dnevnik: &models.Journal{DezurniID: dezurni.String(), DezurniOd: &od}}}
	if h.osobeZaPoziv(vod, stanja, time.Now()) != nil {
		t.Error("izbor bez imenika nije prazan")
	}
	h.izvori.Osobe = func(sektor string, area int, uloga models.Role) ([]models.User, error) {
		if uloga == models.RoleOperator {
			return []models.User{{ID: uuid.New(), FullName: "Drugi"}, {ID: dezurni, FullName: "Dora Proba", MobilePhone: "091 2"}}, nil
		}
		return nil, nil
	}
	o := h.osobeZaPoziv(vod, stanja, time.Now())
	if len(o) != 1 || o[0].Ime != "Dora Proba" || o[0].Funkcija != "dežurni u COP-u, Sektor B" {
		t.Errorf("dežurni u izvanrednoj obrani: %+v", o)
	}
	stanja[0].Stadij = models.PhaseNormal
	if o := h.osobeZaPoziv(vod, stanja, time.Now()); len(o) != 0 {
		t.Errorf("dežurni u preventivnoj obrani: %+v", o)
	}
	if h.dezurni("B", "nepoznat") != nil {
		t.Error("nepoznat dežurni")
	}
}

// Stanja sektora za traku: bez akata greška, s aktima stanja i dionice
func TestStanjaSektoraNaslovne(t *testing.T) {
	ctx := context.Background()
	if _, _, err := (&Server{}).stanjaSektoraNaslovne(ctx, "B"); err == nil {
		t.Error("bez akata nema greške")
	}
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "akti.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	s := &Server{
		akti: service.NewAktService(repository.NewAktiRepository(baza, rec), repository.NewStationRepository(baza, rec), sectionRepo,
			repository.NewTerritoryRepository(baza, rec), repository.NewReadingRepository(baza, rec), users, nil, "test"),
		sectionService: service.NewSectionService(sectionRepo, service.NewSSEBroker()),
	}
	if _, dionice, err := s.stanjaSektoraNaslovne(ctx, "B"); err != nil || len(dionice) != 0 {
		t.Errorf("prazan sektor: %v %v", dionice, err)
	}
	baza.Close()
	if _, _, err := s.stanjaSektoraNaslovne(ctx, "B"); err == nil {
		t.Error("bez baze nema greške")
	}
}
