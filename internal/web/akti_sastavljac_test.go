package web

import (
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	webassets "gocop/web"
)

// Akt o stupnju obrane izdaje se isključivo u Centru obrane od poplava:
// „Novi akt” pod Rješenjima i obavijestima i „Proglasi / prekini” na
// stranici očitanja letve vidi samo tko smije sastaviti nacrt (voditelj
// centra, zamjenik, dežurni operater sektora), a nacrt ispravlja samo njegov
// autor; ovjera ostaje kakva je (Odluke (8. 10. 2026.), pitanje 18).

// okolinaSastavljaca je sektor P s letvom Primjerovo na dionici P.1.1 i
// djelatnicima po ulogama
type okolinaSastavljaca struct {
	akti       *service.AktService
	readings   *service.ReadingService
	stations   *service.StationService
	structures *service.StructureService
	users      *service.UserService
	letva      *models.Station
	tmpl       func(stranica string) *template.Template
	osobe      map[string]*models.User
	// za cijeli poslužitelj: prijava sesijom i vidljivost modula
	auth     *service.AuthService
	sesije   *repository.SessionRepository
	moduli   *service.ModuleService
	userRepo *repository.UserRepository // za osobe koje pojedini test doda
	baza     *sql.DB                    // za test koji kvari bazu
}

// djelatnik je osoba s jednom aktivnom dužnošću u sektoru P
type djelatnik struct {
	korisnik, ime string
	duznost       models.Duty
}

func novaOkolinaSastavljaca(t *testing.T) *okolinaSastavljaca {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "sastavljac.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	sesije := repository.NewSessionRepository(baza)
	auth := service.NewAuthService(userRepo, sesije)
	users := service.NewUserService(userRepo, auth, service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	sections := service.NewSectionService(sectionRepo, service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	structureRepo := repository.NewStructureRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	o := &okolinaSastavljaca{
		auth: auth, sesije: sesije, userRepo: userRepo, moduli: service.NewModuleService(repository.NewModuleRepository(baza, rec)),
		akti: service.NewAktService(repository.NewAktiRepository(baza, rec), stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users,
			service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo), "test"),
		readings:   service.NewReadingService(readingRepo, stationRepo, structureRepo, sections, users),
		stations:   service.NewStationService(stationRepo, sections, service.NewSSEBroker()),
		structures: service.NewStructureService(structureRepo),
		users:      users,
		osobe:      map[string]*models.User{},
		baza:       baza,
	}
	ctx := context.Background()
	o.letva = &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stationRepo.CreateStation(ctx, o.letva); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, 'P.1.1', ?, ?)`,
		uuid.NewString(), o.letva.ID.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
	sektor, podrucje := "P", 1
	for uloga, d := range map[string]djelatnik{
		"dežurni":      {"iivic-cop", "Iva Ivić", models.Duty{Title: "Dežurni operater u COP-u", Role: models.RoleOperator, ScopeType: models.ScopeSector, SectorID: &sektor}},
		"zamjenik":     {"iivic", "Ivo Ivić", models.Duty{Title: "Zamjenik voditelja COP-a", Role: models.RoleCopDeputy, ScopeType: models.ScopeSector, SectorID: &sektor}},
		"rukovoditelj": {"aanic", "Ana Anić", models.Duty{Title: "Rukovoditelj BP 1", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje}},
		"vodočuvar":    {"mmarkic", "Marko Markić", models.Duty{Title: "Vodočuvar P.1.1", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, SectorID: &sektor, AreaID: &podrucje, SectionCodes: "P.1.1"}},
	} {
		u := &models.User{ID: uuid.New(), Username: d.korisnik, FullName: d.ime, IsActive: true}
		d.duznost.IsPrimary = true
		if err := userRepo.CreateUser(u, &d.duznost); err != nil {
			t.Fatal(err)
		}
		if u, err = userRepo.GetUserByID(u.ID); err != nil {
			t.Fatal(err)
		}
		o.osobe[uloga] = u
	}
	// uprava organizacije bez dužnosti u centru ovjerava, ali ne sastavlja
	o.osobe["uprava"] = &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", IsGlobalAdmin: true, IsActive: true}

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	o.tmpl = func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	return o
}

// ovlasti su ovlasti osobe kakve ih daje prijava
func ovlastiOsobe(u *models.User) *models.UserPermissions {
	p := models.NewUserPermissions(*u)
	p.IsGlobalAdmin = u.IsGlobalAdmin
	return p
}

// zovi šalje zahtjev u ime osobe
func (o *okolinaSastavljaca) zovi(mux *http.ServeMux, tko, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
	u := o.osobe[tko]
	r := httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
	if forma != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, u), contextKeyPerms, ovlastiOsobe(u))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(c))
	return w
}

func TestNoviAktIIspravakNacrtaSamoSastavljacu(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	ctx := context.Background()
	h := NewAktiHandler(func() *service.AktService { return o.akti }, o.users, o.stations,
		o.tmpl("akti.html"), o.tmpl("akt_form.html"), o.tmpl("akt.html"), o.tmpl("primatelji.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /akti", h.ShowPopis)
	mux.HandleFunc("GET /akti/novi", h.ShowForm)
	mux.HandleFunc("POST /akti/novi", h.HandleCreate)
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	mux.HandleFunc("POST /akti/{id}/tekst", h.HandleTekst)
	mux.HandleFunc("POST /akti/{id}/slozi-primatelje", h.HandleSloziPrimatelje)

	// „Novi akt” i obrazac: samo Centar obrane od poplava
	for tko, sastavlja := range map[string]bool{"dežurni": true, "zamjenik": true, "rukovoditelj": false, "vodočuvar": false, "uprava": false} {
		if popis := o.zovi(mux, tko, http.MethodGet, "/akti", nil).Body.String(); strings.Contains(popis, `href="/akti/novi"`) != sastavlja {
			t.Errorf("%s: gumb „Novi akt” %v, očekivano %v", tko, !sastavlja, sastavlja)
		}
		w := o.zovi(mux, tko, http.MethodGet, "/akti/novi", nil)
		if sastavlja && w.Code != http.StatusOK {
			t.Errorf("%s: obrazac novog akta %d", tko, w.Code)
		}
		if !sastavlja && (w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/akti?error=")) {
			t.Errorf("%s: obrazac bi trebao vratiti na popis, a dobiveno %d %s", tko, w.Code, w.Header().Get("Location"))
		}
	}
	// ni obrazac poslan mimo gumba ne sastavlja nacrt
	forma := url.Values{"station_id": {o.letva.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"PRIPREMNO"},
		"vrijedi": {time.Now().Add(-time.Hour).In(models.Zagreb).Format("2006-01-02T15:04")}}
	for _, tko := range []string{"rukovoditelj", "vodočuvar", "uprava"} {
		if l := mustUnescape(o.zovi(mux, tko, http.MethodPost, "/akti/novi", forma).Header().Get("Location")); !strings.Contains(l, "Centar obrane od poplava sektora P") {
			t.Errorf("%s: sastavljanje nacrta mora biti odbijeno s objašnjenjem: %s", tko, l)
		}
	}
	if svi, _ := o.akti.List(ctx, nil, repository.FiltarAkata{}); len(svi) != 0 {
		t.Fatalf("nacrt je nastao mimo Centra obrane od poplava: %+v", svi)
	}

	// dežurni sastavi nacrt; ispravlja ga samo on, ovjerava rukovoditelj
	l := mustUnescape(o.zovi(mux, "dežurni", http.MethodPost, "/akti/novi", forma).Header().Get("Location"))
	if !strings.Contains(l, "success=") {
		t.Fatalf("nacrt dežurnog operatera: %s", l)
	}
	id := strings.TrimPrefix(strings.SplitN(l, "?", 2)[0], "/akti/")
	for tko, z := range map[string]struct{ ispravlja, ovjerava bool }{
		"dežurni": {true, false}, "zamjenik": {false, false}, "rukovoditelj": {false, true}, "vodočuvar": {false, false}, "uprava": {false, true},
	} {
		s := o.zovi(mux, tko, http.MethodGet, "/akti/"+id, nil).Body.String()
		if strings.Contains(s, "Ispravi tekst nacrta") != z.ispravlja || strings.Contains(s, "Ponovno složi primatelje") != z.ispravlja {
			t.Errorf("%s: ispravak nacrta na stranici, očekivano %v", tko, z.ispravlja)
		}
		if strings.Contains(s, "Ovjeri u goCOP-u") != z.ovjerava {
			t.Errorf("%s: ovjera na stranici, očekivano %v", tko, z.ovjerava)
		}
		tekst := url.Values{"uvod": {"Uvod osobe " + tko + "."}, "zavrsno": {"Završna rečenica."}}
		if l := mustUnescape(o.zovi(mux, tko, http.MethodPost, "/akti/"+id+"/tekst", tekst).Header().Get("Location")); strings.Contains(l, "success=") != z.ispravlja {
			t.Errorf("%s: ispravak teksta: %s", tko, l)
		}
		if l := mustUnescape(o.zovi(mux, tko, http.MethodPost, "/akti/"+id+"/slozi-primatelje", url.Values{}).Header().Get("Location")); strings.Contains(l, "success=") != z.ispravlja {
			t.Errorf("%s: ponovno složeni primatelji: %s", tko, l)
		}
	}
	if a, _ := o.akti.Get(ctx, id); a == nil || a.Uvod != "Uvod osobe dežurni." || a.Izradio != "Iva Ivić" {
		t.Errorf("nacrt mora nositi samo ispravak svog autora: %+v", a)
	}
}

func TestProglasiPrekiniSamoSastavljacu(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	h := NewReadingsHandler(o.readings, o.stations, o.structures, o.users, o.tmpl("readings.html"), o.tmpl("reading_history.html"), o.tmpl("reading_form.html"))
	h.SetAkti(func() *service.AktService { return o.akti })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readings/station/{id}", h.ShowHistory)
	for tko, vidi := range map[string]bool{"dežurni": true, "zamjenik": true, "rukovoditelj": false, "vodočuvar": false, "uprava": false} {
		w := o.zovi(mux, tko, http.MethodGet, "/readings/station/"+o.letva.ID.String(), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: stranica očitanja %d", tko, w.Code)
		}
		if strings.Contains(w.Body.String(), "Proglasi / prekini") != vidi {
			t.Errorf("%s: gumb „Proglasi / prekini”, očekivano %v", tko, vidi)
		}
	}

	// bez modula Dnevnici gumba nema, jer bi /akti/novi dao 403 (Odluke
	// (30. 10. 2026.), 11)
	r := httptest.NewRequest(http.MethodGet, "/readings/station/"+o.letva.ID.String(), nil)
	dezurni := o.osobe["dežurni"]
	c := context.WithValue(context.WithValue(context.WithValue(r.Context(), contextKeyUser, dezurni), contextKeyPerms, ovlastiOsobe(dezurni)),
		contextKeyModules, models.Visibility{models.ModuleReadings: true})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(c))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Proglasi / prekini") {
		t.Errorf("bez Dnevnika: %d, gumb „Proglasi / prekini” %v", w.Code, strings.Contains(w.Body.String(), "Proglasi / prekini"))
	}

	// bez servisa akata gumba nema ni za koga
	perms := ovlastiOsobe(o.osobe["dežurni"])
	ctx := context.Background()
	if (&ReadingsHandler{}).smijeProglasiti(ctx, perms, o.letva) {
		t.Error("rukovatelj bez servisa akata nudi gumb")
	}
	bez := &ReadingsHandler{}
	bez.SetAkti(func() *service.AktService { return nil })
	if bez.smijeProglasiti(ctx, perms, o.letva) {
		t.Error("rukovatelj bez pokrenutog servisa akata nudi gumb")
	}
}

// Gumb dobiva servis akata pri postavljanju poslužitelja (setupRoutes). Bez
// toga ga ne bi vidio ni Centar obrane od poplava, a test samog rukovatelja
// to ne bi primijetio; zato ovdje kroz pravi poslužitelj i prijavu sesijom.
func TestProglasiPrekiniKrozPosluzitelj(t *testing.T) {
	o := novaOkolinaSastavljaca(t)
	s, err := NewServer("", o.auth, o.users, nil, nil, o.stations, nil, o.structures, o.readings, nil, o.moduli, nil, nil, nil,
		SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAkti(o.akti)
	for tko, vidi := range map[string]bool{"dežurni": true, "zamjenik": true, "rukovoditelj": false, "vodočuvar": false} {
		sesija := &models.Session{UserID: o.osobe[tko].ID, IPAddress: "192.168.1.50", UserAgent: "test", ExpiresAt: time.Now().Add(time.Hour)}
		if err := o.sesije.CreateSession(sesija); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/readings/station/"+o.letva.ID.String(), nil)
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija.ID.String()})
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: stranica očitanja kroz poslužitelj %d: %s", tko, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "Proglasi / prekini") != vidi {
			t.Errorf("%s: gumb „Proglasi / prekini” kroz poslužitelj, očekivano %v", tko, vidi)
		}
	}
}
